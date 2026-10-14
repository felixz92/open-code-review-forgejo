// Package publish orchestrates posting an OCR result to a Forgejo PR.
package publish

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/felixz92/open-code-review-forgejo/internal/diffinv"
	"github.com/felixz92/open-code-review-forgejo/internal/format"
	"github.com/felixz92/open-code-review-forgejo/internal/incremental"
	"github.com/felixz92/open-code-review-forgejo/internal/policy"
	"github.com/felixz92/open-code-review-forgejo/internal/poster"
	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

// Options configures a publish run.
type Options struct {
	RunTag        string
	Sticky        bool
	Incremental   bool
	OverlapThresh float64
	Route         policy.Policy
	// CommitID is the SHA OCR actually reviewed; "" falls back to the PR head.
	CommitID string
	// Suggestions controls whether suggestion blocks are rendered into
	// inline and summary-fallback comment bodies.
	Suggestions bool
	// ApproveBelow is the highest severity that does not block approval:
	// findings above it force REQUEST_CHANGES. Empty defaults to "low";
	// unknown values fail safe (all findings block).
	ApproveBelow string
	// GetDiff is used by tests via the Poster interface; no extra fields.
}

// Review states submitted to Forgejo, as sent in the review event.
const (
	StateApproved       = "APPROVED"
	StateRequestChanges = "REQUEST_CHANGES"
	StateComment        = "COMMENT"
)

// Stats holds mutually exclusive posting counts.
type Stats struct {
	Total      int
	Inline     int
	Summary    int
	Skipped    int
	Routed     int
	Failed     int
	SummaryURL string
	// State is the review state the run derived (APPROVED when no findings
	// block approval, REQUEST_CHANGES when findings posted successfully,
	// COMMENT when posting failed).
	State string
}

type inlineItem struct {
	comment *review.Comment
	body    string
	id      string
	span    *incremental.Span
}

type failedItem struct {
	comment *review.Comment
	reason  string
}

const defaultApproveBelow = "low"

// countFindings counts result-JSON comments at severity ranks above the
// approval threshold. Routing and incremental skipping never influence the
// count: presentation must not change the verdict.
func countFindings(result *review.Result, approveBelow string) int {
	n := 0
	for i := range result.Comments {
		if policy.SeverityAbove(&result.Comments[i], approveBelow) {
			n++
		}
	}
	return n
}

// decideState computes the Forgejo review state from the result JSON, the
// approval threshold, and the posting outcome, with no run-to-run state.
// Findings are JSON comments above the approval threshold; posted reports
// whether posting succeeded (inline review submitted or summary published).
func decideState(result *review.Result, approveBelow string, posted bool) string {
	if countFindings(result, approveBelow) == 0 {
		return StateApproved
	}
	if posted {
		return StateRequestChanges
	}
	return StateComment
}

// Publish posts the result to the PR via the poster and returns stats.
func Publish(result *review.Result, p poster.Poster, opts Options) (*Stats, error) {
	if opts.OverlapThresh <= 0 || opts.OverlapThresh > 1 {
		opts.OverlapThresh = incremental.DefaultOverlapThreshold
	}
	if opts.ApproveBelow == "" {
		opts.ApproveBelow = defaultApproveBelow
	}
	stats := &Stats{Total: len(result.Comments)}

	// No comments: empty approval review (best-effort) plus the LGTM
	// summary (sticky-aware), which remains the primary contract.
	if stats.Total == 0 {
		message := result.Message
		if message == "" {
			message = "No comments generated. Looks good to me."
		}
		commitID := bestCommitID(p, opts)
		reviewID, err := p.CreatePullReview(nil, commitID)
		if err != nil {
			stats.State = StateComment
			log.Printf("[approve] approval review creation failed: %v", err)
		} else if err := p.SubmitPullReview(reviewID, StateApproved, approveBody); err != nil {
			stats.State = StateComment
			log.Printf("[approve] approval review submit failed: %v", err)
		} else {
			stats.State = StateApproved
		}
		anchor := findSummaryComment(p, opts)
		stats.SummaryURL = upsertSummary(p, opts, anchor, format.WrapSummaryBody(format.BuildNoIssuesBody(message), opts.RunTag))
		return stats, nil
	}

	// Partition: inline / no-line / policy-routed.
	var inline []inlineItem
	var noLine []format.SummaryItem
	var routed []format.SummaryItem
	for i := range result.Comments {
		c := &result.Comments[i]
		hasLine := c.EndLine >= 1 && c.Path != ""
		if !hasLine {
			noLine = append(noLine, format.SummaryItem{Comment: c, Reason: format.NoLineReason})
			continue
		}
		if res := policy.Route(c, opts.Route); res.Routed {
			routed = append(routed, format.SummaryItem{Comment: c, Reason: res.Reason})
			continue
		}
		id := newCommentID(opts.RunTag)
		// Suggestions stay off here: blocks are appended after the diff
		// pre-check passes (the keep loop below appends SuggestionBlocks
		// once); passing opts.Suggestions here would double-render them.
		inline = append(inline, inlineItem{
			comment: c,
			body:    format.FormatComment(c, id, c.StartLine, c.EndLine, false),
			id:      id,
			span:    incremental.NewSpan(c.StartLine, c.EndLine),
		})
	}
	stats.Summary = len(noLine)
	stats.Routed = len(routed)

	// Deterministic order so reruns reproduce the same post sequence.
	sort.SliceStable(inline, func(i, j int) bool {
		a, b := inline[i].comment, inline[j].comment
		switch {
		case a.Path != b.Path:
			return a.Path < b.Path
		case a.StartLine != b.StartLine:
			return a.StartLine < b.StartLine
		default:
			return a.EndLine < b.EndLine
		}
	})

	// Incremental filtering: drop comments overlapping prior bot comments.
	if opts.Incremental && len(inline) > 0 {
		history, err := loadHistory(p)
		if err != nil {
			log.Printf("[incremental] could not load history, disabling filter: %v", err)
		} else {
			var kept []inlineItem
			for _, it := range inline {
				if incremental.Overlaps(it.comment.Path, *it.span, history, opts.OverlapThresh) {
					stats.Skipped++
					continue
				}
				kept = append(kept, it)
			}
			inline = kept
		}
	}

	// Diff pre-check: drop provably out-of-diff comments before submitting.
	var failed []failedItem
	if len(inline) > 0 {
		diff, err := p.GetDiff()
		if err != nil {
			log.Printf("[diff-precheck] diff unavailable, keeping all comments: %v", err)
			diff = nil
		}
		inv, invErr := diffinv.New(diff)
		if invErr != nil {
			log.Printf("[diff-precheck] diff not parseable, keeping all comments: %v (first 200 chars: %q)",
				invErr, preview(diff))
		}
		var keep []inlineItem
		for _, it := range inline {
			if invErr == nil && inv.Classify(it.comment.Path, it.comment.StartLine, it.comment.EndLine) == diffinv.Invalid {
				failed = append(failed, failedItem{comment: it.comment, reason: "out of diff (line could not be resolved)"})
				continue
			}
			if opts.Suggestions {
				// Fail-open: when the diff is unavailable or unparseable
				// (invErr != nil) suggestions are still included.
				it.body += format.SuggestionBlocks(it.comment)
			}
			keep = append(keep, it)
		}
		inline = keep
	}

	// Idempotent retry cleanup: delete stale pending reviews of this bot.
	// Unknown identity means ownership cannot be proven, so cleanup is
	// skipped to avoid deleting other users' pending reviews.
	botLogin, err := p.WhoAmI()
	if err != nil {
		log.Printf("[cleanup] skipping stale pending review cleanup, identity unknown: %v", err)
	} else {
		deleteStalePending(p, botLogin)
	}

	// Summary anchor (pre-review body) before posting.
	anchor := anchorSummary(p, opts, stats, noLine, routed, result.Warnings)

	// Post the review.
	// submitState assumes posting succeeds; failure paths demote the
	// recorded state to COMMENT.
	submitState := decideState(result, opts.ApproveBelow, true)
	if len(inline) > 0 {
		commitID := bestCommitID(p, opts)
		if commitID == "" {
			// Nothing can be created: fail the comments up front with a
			// clear reason; no review is created or submitted.
			for _, it := range inline {
				failed = append(failed, failedItem{comment: it.comment, reason: "could not resolve reviewed commit for posting"})
			}
			inline = nil
			stats.State = StateComment
		} else {
			inputs := make([]poster.ReviewCommentInput, 0, len(inline))
			for _, it := range inline {
				inputs = append(inputs, poster.ReviewCommentInput{
					Path:    it.comment.Path,
					Body:    it.body,
					NewLine: it.comment.EndLine,
				})
			}
			reviewID, err := p.CreatePullReview(inputs, commitID)
			if err != nil {
				// The whole batch failed (e.g. commit drift): report per comment.
				for _, it := range inline {
					failed = append(failed, failedItem{
						comment: it.comment,
						reason:  fmt.Sprintf("review could not be created for commit %s: %v", commitID, err),
					})
				}
				stats.State = StateComment
				log.Printf("[publish] review creation failed: %v", err)
			} else {
				if err := p.SubmitPullReview(reviewID, submitState, submitBody(stats, inline)); err != nil {
					_ = p.DeletePullReview(reviewID)
					for _, it := range inline {
						failed = append(failed, failedItem{
							comment: it.comment,
							reason:  fmt.Sprintf("review submit failed: %v", err),
						})
					}
					stats.State = StateComment
					log.Printf("[publish] review submit failed: %v", err)
				} else {
					stats.Inline = len(inline)
					stats.State = submitState
				}
			}
		}
	}
	stats.Failed = len(failed)

	// Finalize the summary.
	finalBody := finalizeBody(opts, stats, noLine, routed, failed, inline, result.Warnings)
	stats.SummaryURL = upsertSummary(p, opts, anchor, finalBody)
	if stats.State == "" {
		// No inline review was attempted: decide from the posting outcome.
		// A summary-only run still requests changes when the summary
		// published; posting failures had already recorded COMMENT.
		stats.State = decideState(result, opts.ApproveBelow, stats.SummaryURL != "")
	}
	return stats, nil
}

func preview(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// approveBody is the body of the empty approval review on clean runs.
const approveBody = "🔍 OpenCodeReview: no issues found."

// bestCommitID returns the SHA OCR actually reviewed, falling back to the
// PR's current head SHA. Empty when neither is available.
func bestCommitID(p poster.Poster, opts Options) string {
	if opts.CommitID != "" {
		return opts.CommitID
	}
	pr, err := p.GetPullRequest()
	if err != nil || pr == nil || pr.HeadSHA == "" {
		log.Printf("[publish] could not resolve commit for posting: %v", err)
		return ""
	}
	return pr.HeadSHA
}

func submitBody(stats *Stats, inline []inlineItem) string {
	return fmt.Sprintf("🔍 OpenCodeReview: %d inline review comment(s) posted from automated review.", len(inline))
}

func newCommentID(runTag string) string {
	return fmt.Sprintf("ocr-%s-%08x", runTag, randUint32())
}

func randUint32() uint32 {
	var b [4]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return uint32(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint32(b[:])
}

func deleteStalePending(p poster.Poster, botLogin string) {
	reviews, err := p.ListPullReviews()
	if err != nil {
		log.Printf("[cleanup] could not list reviews: %v", err)
		return
	}
	for _, r := range reviews {
		if r.State != "PENDING" || r.Reviewer != botLogin {
			continue
		}
		if err := p.DeletePullReview(r.ID); err != nil {
			log.Printf("[cleanup] deleting pending review %d: %v", r.ID, err)
		}
	}
}

// loadHistory collects spans of prior bot review comments.
func loadHistory(p poster.Poster) ([]incremental.HistoryEntry, error) {
	reviews, err := p.ListPullReviews()
	if err != nil {
		return nil, err
	}
	var history []incremental.HistoryEntry
	for _, r := range reviews {
		comments, err := p.ListPullReviewComments(r.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range comments {
			if !strings.Contains(c.Body, format.BotMarker) {
				continue
			}
			span := spanFromHistory(c)
			if span != nil {
				history = append(history, incremental.HistoryEntry{Path: c.Path, Span: *span})
			}
		}
	}
	return history, nil
}

// spanLineRange extracts the stated line span from our own multiline comment
// bodies. It is anchored to the "> File `path` lines S-E" note FormatComment
// appends, so wording inside the comment content cannot be mistaken for the
// recorded span.
var spanLineRange = regexp.MustCompile(`(?m)^> File [\x60][^\x60]*[\x60] lines (\d+)-(\d+)\s*$`)

func spanFromHistory(c poster.ReviewCommentInfo) *incremental.Span {
	if m := spanLineRange.FindStringSubmatch(c.Body); m != nil {
		s, _ := strconv.ParseInt(m[1], 10, 64)
		e, _ := strconv.ParseInt(m[2], 10, 64)
		return incremental.NewSpan(s, e)
	}
	return incremental.NewSpan(0, c.NewLine)
}

// findSummaryComment returns the ID of the newest comment carrying the marker
// (cross-run marker in sticky mode, per-run tag otherwise), or 0.
func findSummaryComment(p poster.Poster, opts Options) int64 {
	comments, err := p.ListIssueComments()
	if err != nil {
		log.Printf("[summary] could not list comments: %v", err)
		return 0
	}
	needle := format.SummaryMarker
	if !opts.Sticky {
		needle = fmt.Sprintf(format.PerRunMarkerFmt, opts.RunTag)
	}
	for i := len(comments) - 1; i >= 0; i-- {
		if strings.Contains(comments[i].Body, needle) {
			return comments[i].ID
		}
	}
	return 0
}

// anchorSummary creates or reuses the summary comment with the pre-review body.
// Returns the anchor comment ID (0 when unavailable).
func anchorSummary(p poster.Poster, opts Options, stats *Stats, noLine, routed []format.SummaryItem, warnings []review.Warning) int64 {
	if id := findSummaryComment(p, opts); id != 0 {
		return id
	}
	body := format.WrapSummaryBody(format.BuildPreReviewBody(stats.Total, noLine, routed, warnings, opts.Suggestions), opts.RunTag)
	id, err := p.CreateIssueComment(body)
	if err != nil {
		log.Printf("[summary] anchor creation failed: %v", err)
		return 0
	}
	return id
}

// upsertSummary writes the final body to the anchor or creates the summary.
func upsertSummary(p poster.Poster, opts Options, anchor int64, body string) string {
	if anchor != 0 {
		if err := p.EditIssueComment(anchor, body); err == nil {
			return fmt.Sprintf("comment:%d", anchor)
		} else {
			log.Printf("[summary] anchor update failed, falling back: %v", err)
		}
	}
	id, err := p.CreateIssueComment(body)
	if err != nil {
		log.Printf("[summary] summary creation failed: %v", err)
		return ""
	}
	return fmt.Sprintf("comment:%d", id)
}

func finalizeBody(opts Options, stats *Stats, noLine, routed []format.SummaryItem, failed []failedItem, posted []inlineItem, warnings []review.Warning) string {
	body := format.BuildSummaryBody(stats.Total, stats.Inline, stats.Summary, stats.Skipped, stats.Routed, stats.Failed, len(warnings))
	body += format.FormatSummaryComments(noLine, opts.Suggestions)
	body += format.FormatSummaryComments(routed, opts.Suggestions)
	for _, f := range failed {
		body += format.FormatSummaryComments([]format.SummaryItem{{Comment: f.comment, Reason: f.reason}}, opts.Suggestions)
	}
	if len(posted) == 0 && stats.Skipped > 0 {
		body += "\n\n---\n\nℹ️ All inline comments overlapped with existing reviews; nothing new was posted."
	}
	body += format.FormatWarnings(warnings)
	return format.WrapSummaryBody(body, opts.RunTag)
}
