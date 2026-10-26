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
	// findings above it force REQUEST_CHANGES. Empty defaults to "low".
	// Fail safe: unknown thresholds and unranked comment severities both
	// count as blocking findings.
	ApproveBelow string
	// ResolutionTracking maintains the cross-run findings ledger in the
	// sticky summary (opt-in via --resolution-tracking).
	ResolutionTracking bool
	// Coverage gates which open ledger entries may resolve (from
	// --coverage-diff). Nil keeps the full-range resolution semantics.
	Coverage *Coverage
	// GetDiff is used by tests via the Poster interface; no extra fields.
}

// Review states submitted to Forgejo, as sent in the review event.
const (
	StateApproved       = "APPROVED"
	StateRequestChanges = "REQUEST_CHANGES"
	StateComment        = "COMMENT"
)

// Run-status values recorded in Stats.RunStatus and surfaced to the
// workflow via OCR_RUN_STATUS in the dotenv stats file. CLI and publish
// code must compare against these constants rather than duplicated
// string literals.
const (
	RunStatusFailed    = "failed"
	RunStatusCompleted = "completed"
)

// Stats holds mutually exclusive posting counts.
type Stats struct {
	Total   int
	Inline  int
	Summary int
	Skipped int
	Routed  int
	Failed  int
	// Resolved counts open ledger entries this run marked resolved
	// (cross-run resolution tracking; deliberately outside the
	// OCR_COMMENTS_* total partition, design D7).
	Resolved   int
	SummaryURL string
	// State is the review state the run derived (APPROVED when no findings
	// block approval, REQUEST_CHANGES when findings posted successfully,
	// COMMENT when posting failed).
	State string
	// RunStatus classifies the OCR run itself: "failed" when the result
	// JSON reported status "failed", "completed" otherwise. Surfaced to
	// the workflow via OCR_RUN_STATUS in the dotenv stats file.
	RunStatus string
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
		// Fail safe: a comment whose severity the ladder does not
		// recognize (e.g. a missing or nonstandard field in the
		// external result JSON) blocks approval instead of silently
		// approving while it is still posted inline.
		if !policy.SeverityKnown(result.Comments[i].Severity) || policy.SeverityAbove(&result.Comments[i], approveBelow) {
			n++
		}
	}
	return n
}

// decideState computes the Forgejo review state from the result JSON, the
// approval threshold, and the posting outcome, with no run-to-run state.
// Findings are JSON comments above the approval threshold; posted reports
// whether posting succeeded (inline review submitted or summary published).
func decideState(result *review.Result, approveBelow string, posted bool, openSurvivors int) string {
	if countFindings(result, approveBelow) == 0 {
		if openSurvivors > 0 {
			// Clean delta run with surviving open entries: those
			// findings were not re-reviewed, so approving here would
			// vouch for code nobody looked at (design D4).
			return StateComment
		}
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

	// Failed run: never approve, never touch the resolution ledger (see
	// publishFailed).
	if result.Status == review.StatusFailed {
		return publishFailed(result, p, opts, stats), nil
	}

	// No comments: empty approval review (best-effort) plus the LGTM
	// summary (sticky-aware), which remains the primary contract.
	if stats.Total == 0 {
		message := result.Message
		if message == "" {
			message = "No comments generated. Looks good to me."
		}
		// Decide the state before submitting: surviving open entries
		// (clean delta run) must downgrade the approval (design D4).
		var prior *format.Ledger
		openSurvivors := 0
		if opts.ResolutionTracking {
			prior = loadLedger(p, opts)
			openSurvivors = survivingOpenCount(prior, opts.Coverage, nil, opts.OverlapThresh)
		}
		state := decideState(result, opts.ApproveBelow, true, openSurvivors)
		cleanupStalePending(p)
		commitID := bestCommitID(p, opts)
		reviewID, err := p.CreatePullReview(nil, commitID)
		if err != nil {
			stats.State = StateComment
			log.Printf("[approve] approval review creation failed: %v", err)
		} else if err := p.SubmitPullReview(reviewID, state, approveBody); err != nil {
			// Mirror the findings path: the created pending review is
			// removed on refusal so a persistent failure cannot
			// accumulate orphans (this branch returns before any later
			// deleteStalePending pass could reap them).
			_ = p.DeletePullReview(reviewID)
			stats.State = StateComment
			log.Printf("[approve] approval review submit failed: %v", err)
		} else {
			stats.State = state
		}
		anchor := findSummaryComment(p, opts)
		body := format.WrapSummaryBody(format.BuildNoIssuesBody(message), opts.RunTag)
		if opts.ResolutionTracking {
			// A non-empty open ledger resolves the entries the coverage
			// permits and reports the resolutions; an empty ledger keeps
			// the classic body.
			var resolved int
			body, resolved, openSurvivors = trackedNoIssuesBody(prior, opts, message)
			stats.Resolved = resolved
			if openSurvivors > 0 {
				body += fmt.Sprintf("\n\nℹ️ Delta clean, %d finding(s) remain open.", openSurvivors)
			}
		}
		// Success paths record the reviewed head for the query mode;
		// the helper returns "" when the SHA is unknown.
		body += format.FormatLastReviewedMarker(result.Manifest.Input.ResolvedHead)
		stats.SummaryURL = upsertSummary(p, opts, anchor, body)
		return stats, nil
	}

	// Partition: inline / no-line / policy-routed.
	var inline []inlineItem
	var noLine []format.SummaryItem
	var routed []format.SummaryItem
	var routedCands []inlineItem
	for i := range result.Comments {
		c := &result.Comments[i]
		hasLine := c.EndLine >= 1 && c.Path != ""
		if !hasLine {
			noLine = append(noLine, format.SummaryItem{Comment: c, Reason: format.NoLineReason})
			continue
		}
		if res := policy.Route(c, opts.Route); res.Routed {
			routed = append(routed, format.SummaryItem{Comment: c, Reason: res.Reason})
			routedCands = append(routedCands, inlineItem{comment: c, span: incremental.NewSpan(c.StartLine, c.EndLine)})
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

	// Resolution-tracking candidates: every finding with usable line
	// evidence the run reported for inline posting or routed to the
	// summary, captured before incremental and diff pre-filtering. A
	// finding still reported by OCR never resolves its ledger entry as
	// long as it is reported with usable line evidence. Findings without
	// lines land in the no-line bucket and never participate in matching
	// (per spec, findings without usable line information never match),
	// so a finding
	// re-reported without lines cannot keep its entry open — the entry
	// resolves here and re-enters fresh once the finding is reported with
	// lines again. Only successfully posted findings create entries
	// (matched-only items can only keep existing entries open).
	candidates := make([]inlineItem, 0, len(inline)+len(routedCands))
	candidates = append(candidates, inline...)
	candidates = append(candidates, routedCands...)

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

	// Prior ledger state must be read before the anchor flow can create
	// this run's own summary comment, which would shadow the genuine
	// prior-run summary comment. The survivor count must also be known
	// before the review state is decided (design D4).
	var priorLedger *format.Ledger
	openSurvivors := 0
	if opts.ResolutionTracking {
		priorLedger = loadLedger(p, opts)
		openSurvivors = survivingOpenCount(priorLedger, opts.Coverage, candidates, opts.OverlapThresh)
	}

	// Idempotent retry cleanup: delete stale pending reviews of this bot.
	cleanupStalePending(p)

	// Summary anchor (pre-review body) before posting.
	anchor := anchorSummary(p, opts, stats, noLine, routed, result.Warnings)

	// Post the review.
	// submitState assumes posting succeeds; failure paths demote the
	// recorded state to COMMENT. Surviving open entries downgrade an
	// otherwise clean approval (design D4).
	submitState := decideState(result, opts.ApproveBelow, true, openSurvivors)
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

	// Resolution tracking: fold the run's inline findings into the ledger
	// after the review submit settled, before rendering the final summary.
	var resolutions []format.LedgerEntry
	var mergedLedger *format.Ledger
	if opts.ResolutionTracking {
		addSet := inline
		if stats.Inline != len(inline) {
			addSet = nil // nothing was actually posted: no entries added (D3)
		}
		mergedLedger, resolutions = mergeLedger(priorLedger, opts.Coverage, candidates, addSet, opts.RunTag, resolvedToday(), opts.OverlapThresh)
		stats.Resolved = len(resolutions)
	}

	// Finalize the summary.
	finalBody := finalizeBody(opts, stats, noLine, routed, failed, inline, result.Warnings, mergedLedger, resolutions)
	if stats.State == StateApproved {
		// Approvals record the reviewed head for the query mode; the
		// helper returns "" when the SHA is unknown.
		finalBody += format.FormatLastReviewedMarker(result.Manifest.Input.ResolvedHead)
	} else if openSurvivors > 0 && countFindings(result, opts.ApproveBelow) == 0 {
		// Downgraded run: note why the run did not approve. Computed
		// from the survivor count directly because when no inline review
		// was attempted the recorded state only settles below.
		finalBody += fmt.Sprintf("\n\nℹ️ Delta clean, %d finding(s) remain open.", openSurvivors)
	}
	stats.SummaryURL = upsertSummary(p, opts, anchor, finalBody)
	if stats.State == "" {
		// No inline review was attempted: decide from the posting outcome.
		// A summary-only run still requests changes when the summary
		// published; posting failures had already recorded COMMENT.
		stats.State = decideState(result, opts.ApproveBelow, stats.SummaryURL != "", openSurvivors)
	}
	return stats, nil
}

// publishFailed handles a result that reported status "failed": it never
// submits an approval, leaves the resolution ledger untouched, and relays
// the failure note as a COMMENT note review plus the sticky summary. Any
// findings in a failed result are informational only.
func publishFailed(result *review.Result, p poster.Poster, opts Options, stats *Stats) *Stats {
	stats.State = StateComment
	stats.RunStatus = RunStatusFailed
	// Nothing was posted: the OCR_COMMENTS_* partition stays fully zero
	// even when the failed result carries findings, which only appear
	// informationally in the failure note.
	stats.Total = 0

	cleanupStalePending(p)
	commitID := bestCommitID(p, opts)
	note := format.BuildFailureBody(result.Message, result.Warnings)
	reviewID, err := p.CreatePullReview(nil, commitID)
	if err != nil {
		// The failure note still reaches the sticky summary below.
		log.Printf("[failed-run] note review creation failed: %v", err)
	} else if err := p.SubmitPullReview(reviewID, StateComment, note); err != nil {
		// Mirror the approval path: the created pending review is
		// removed on refusal so a persistent failure cannot
		// accumulate orphans.
		_ = p.DeletePullReview(reviewID)
		log.Printf("[failed-run] note review submit failed: %v", err)
	}

	// Findings from a failed run cannot be trusted to anchor: they are
	// listed in the summary as information only.
	body := note
	if len(result.Comments) > 0 {
		items := make([]format.SummaryItem, 0, len(result.Comments))
		for i := range result.Comments {
			items = append(items, format.SummaryItem{
				Comment: &result.Comments[i],
				Reason:  "review run failed (informational)",
			})
		}
		body += format.FormatSummaryComments(items, false)
	}

	anchor := findSummaryComment(p, opts)
	if opts.ResolutionTracking {
		// The ledger lives inside the sticky summary: re-encode the
		// prior ledger unchanged so open entries survive the run
		// (no entries added, none resolved).
		prior := loadLedger(p, opts)
		body += "\n" + format.EncodeLedger(prior)
	}

	stats.SummaryURL = upsertSummary(p, opts, anchor, format.WrapSummaryBody(body, opts.RunTag))
	return stats
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

// cleanupStalePending deletes stale pending reviews of the bot account.
// Unknown identity means ownership cannot be proven, so cleanup is skipped
// to avoid deleting other users' pending reviews.
func cleanupStalePending(p poster.Poster) {
	botLogin, err := p.WhoAmI()
	if err != nil {
		log.Printf("[cleanup] skipping stale pending review cleanup, identity unknown: %v", err)
		return
	}
	deleteStalePending(p, botLogin)
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
	// Only anchor on the bot's own summaries: editing a foreign comment is
	// impossible anyway, and trusting its marker would re-trigger the
	// failed-edit fallback on every run.
	me, err := p.WhoAmI()
	if err != nil {
		log.Printf("[summary] could not identify bot user: %v", err)
		return 0
	}
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
		if comments[i].Author != me || !strings.Contains(comments[i].Body, needle) {
			continue
		}
		return comments[i].ID
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

func finalizeBody(opts Options, stats *Stats, noLine, routed []format.SummaryItem, failed []failedItem, posted []inlineItem, warnings []review.Warning, ledger *format.Ledger, resolutions []format.LedgerEntry) string {
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
	if ledger != nil {
		// Open and resolved findings as distinct sections, then the
		// machine-readable ledger state for the next run.
		body += format.FormatLedgerSections(ledger.Open, resolutions)
		body += "\n" + format.EncodeLedger(ledger)
	}
	return format.WrapSummaryBody(body, opts.RunTag)
}
