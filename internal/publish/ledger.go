package publish

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/felixz92/open-code-review-forgejo/internal/diffinv"
	"github.com/felixz92/open-code-review-forgejo/internal/format"
	"github.com/felixz92/open-code-review-forgejo/internal/incremental"
	"github.com/felixz92/open-code-review-forgejo/internal/poster"
	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

// maxSnippetRunes caps a finding's stored text so the ledger summary cannot
// balloon.
const maxSnippetRunes = 200

// snippet truncates a finding text for ledger storage and neutralizes HTML
// comment terminators so content cannot forge or truncate the markers
// framing the ledger block (same neutralization as inline comment bodies).
func snippet(content string) string {
	// A quoted ledger start marker inside the payload would re-anchor
	// StripLedger below the real block and silently reset tracking
	// state; a quoted terminators pair would splice the block. Both are
	// neutralized with a harmless space injection.
	content = strings.ReplaceAll(content, format.LedgerStartMarker, "<!- - ocr-ledger ")
	content = strings.ReplaceAll(content, "-->", "-- >")
	runes := []rune(content)
	if len(runes) <= maxSnippetRunes {
		return content
	}
	return string(runes[:maxSnippetRunes]) + "…"
}

// mergeLedger folds the current run's inline candidates into the prior
// ledger and returns the merged ledger plus the entries this run resolved
// (design D2/D3; the spec's "fixed finding is marked resolved" scenario keys
// resolution on the run's results, not posting success). Span matching
// reuses the incremental semantics, path-scoped: single-line exact, multi-line
// IoU strictly above the threshold. A candidate that matches an open entry
// keeps it open, refreshing the recorded span and wording; a candidate that
// matches nothing creates a new open entry exactly when it was successfully
// posted. Open entries matched by no candidate move to the resolved list
// with the resolution stamp; the resolved list is pruned to
// MaxResolvedEntries.
// Coverage gates which open ledger entries may resolve on a run.
// Nil keeps the pre-coverage full-range semantics: unmatched open entries
// resolve as before. A non-nil Coverage with a nil inventory means "no
// coverage" (the diff file was unreadable or unparseable): nothing resolves.
// A non-nil inventory resolves an unmatched entry only when the diff
// inventory classifies its path and span as valid.
type Coverage struct {
	Inv *diffinv.Inventory
}

// NewCoverage wraps a parsed diff inventory.
func NewCoverage(inv *diffinv.Inventory) *Coverage { return &Coverage{Inv: inv} }

// NoCoverage returns the "coverage requested but unavailable" state:
// nothing may resolve this run.
func NoCoverage() *Coverage { return &Coverage{} }

// resolves reports whether an unmatched open entry may resolve under this
// coverage state at the given path and span.
func (c *Coverage) resolves(path string, start, end int64) bool {
	if c == nil {
		return true // no coverage flag: full-range semantics
	}
	if c.Inv == nil {
		return false // no coverage: resolve nothing
	}
	return c.Inv.Classify(path, start, end) == diffinv.Valid
}

func mergeLedger(prior *format.Ledger, cov *Coverage, candidates, posted []inlineItem, runTag, resolvedDate string, thresh float64) (*format.Ledger, []format.LedgerEntry) {
	if prior == nil {
		prior = &format.Ledger{Version: format.LedgerVersion}
	}
	next := &format.Ledger{Version: format.LedgerVersion, Resolved: append([]format.LedgerEntry(nil), prior.Resolved...)}
	open := append([]format.LedgerEntry(nil), prior.Open...)
	matchedOpen := make([]bool, len(open))
	matchedCurrent := make(map[*review.Comment]bool, len(posted))

	for _, it := range candidates {
		if it.span == nil || it.comment == nil {
			continue
		}
		for j := range open {
			if matchedOpen[j] || open[j].Path != it.comment.Path {
				continue
			}
			entry := incremental.NewSpan(open[j].StartLine, open[j].EndLine)
			if entry == nil || !incremental.SameCommentSpan(*it.span, *entry, thresh) {
				continue
			}
			matchedOpen[j], matchedCurrent[it.comment] = true, true
			// Refresh the recorded span and wording so slow line drift
			// does not accumulate into a mismatch (design D2).
			open[j].StartLine, open[j].EndLine = it.span.Start, it.span.End
			open[j].Content = snippet(it.comment.Content)
			break
		}
	}

	var thisRun []format.LedgerEntry
	for j, e := range open {
		if matchedOpen[j] {
			next.Open = append(next.Open, e) // survivors keep ledger order
			continue
		}
		if !cov.resolves(e.Path, e.StartLine, e.EndLine) {
			// Coverage says the code under this finding was not
			// provably changed (or coverage is unavailable): the entry
			// was not re-reviewed, so it stays open instead of
			// silently resolving.
			next.Open = append(next.Open, e)
			continue
		}
		e.ResolvedIn, e.ResolvedDate = runTag, resolvedDate
		next.Resolved = append(next.Resolved, e)
		thisRun = append(thisRun, e)
	}
	for _, it := range posted {
		if it.span == nil || it.comment == nil || matchedCurrent[it.comment] {
			continue
		}
		next.Open = append(next.Open, format.LedgerEntry{
			Path:      it.comment.Path,
			StartLine: it.span.Start,
			EndLine:   it.span.End,
			FirstSeen: runTag,
			Content:   snippet(it.comment.Content),
		})
	}
	if excess := len(next.Resolved) - format.MaxResolvedEntries; excess > 0 {
		next.Resolved = next.Resolved[excess:]
	}
	return next, thisRun
}

// survivingOpenCount reports how many prior open entries will still be open
// after the merge: entries re-reported this run (matched by a candidate) plus
// entries the coverage does not resolve. It mirrors mergeLedger's matching
// and coverage rules so the review state can be downgraded before the review
// is submitted (design D4). Posting success only decides whether new entries
// are added, never which prior entries survive, so the count is exact.
func survivingOpenCount(prior *format.Ledger, cov *Coverage, candidates []inlineItem, thresh float64) int {
	if prior == nil {
		return 0
	}
	open := prior.Open
	matched := make([]bool, len(open))
	for _, it := range candidates {
		if it.span == nil || it.comment == nil {
			continue
		}
		for j := range open {
			if matched[j] || open[j].Path != it.comment.Path {
				continue
			}
			entry := incremental.NewSpan(open[j].StartLine, open[j].EndLine)
			if entry == nil || !incremental.SameCommentSpan(*it.span, *entry, thresh) {
				continue
			}
			matched[j] = true
			break
		}
	}
	count := 0
	for j := range open {
		if matched[j] || !cov.resolves(open[j].Path, open[j].StartLine, open[j].EndLine) {
			count++
		}
	}
	return count
}

// freshLedger returns the empty ledger a run degrades to when prior state is
// unreadable (design D6).
func freshLedger() *format.Ledger {
	return &format.Ledger{Version: format.LedgerVersion}
}

// loadLedger reads the prior findings ledger from the existing summary
// comment. It fails open: a failed comment listing, a corrupt or truncated
// ledger block, or an absent block all degrade to a fresh empty ledger,
// never blocking posting. Callers then seed a new ledger from this run.
func loadLedger(p poster.Poster, opts Options) *format.Ledger {
	comments, err := p.ListIssueComments()
	if err != nil {
		log.Printf("[resolution] could not list comments, starting fresh ledger: %v", err)
		return freshLedger()
	}
	needle := format.SummaryMarker
	if !opts.Sticky {
		// Non-sticky summaries are not durable across runs: the per-run
		// needle only matches a summary written for this exact run tag
		// (PR + head SHA), so cross-run state never loads. The CLI
		// therefore rejects --resolution-tracking combined with
		// --sticky-summary=false instead of silently tracking nothing.
		needle = fmt.Sprintf(format.PerRunMarkerFmt, opts.RunTag)
	}
	for i := len(comments) - 1; i >= 0; i-- {
		if !strings.Contains(comments[i].Body, needle) {
			continue
		}
		ledger, found, err := format.ParseLedger(comments[i].Body)
		if err != nil {
			log.Printf("[resolution] prior ledger unusable, starting fresh: %v", err)
			return freshLedger()
		}
		if !found {
			return freshLedger() // summary exists but tracking was off before
		}
		return ledger
	}
	return freshLedger()
}

// resolvedToday is the resolution date stamp for entries resolved this run.
func resolvedToday() string {
	return time.Now().UTC().Format("2006-01-02")
}

// trackedNoIssuesBody builds the clean-run summary body under resolution
// tracking. A non-empty open ledger resolves all entries and notes the
// resolutions instead of presenting a bare no-issues body; an empty ledger
// keeps the existing no-issues body. Either way the merged ledger is
// re-embedded so retained state survives the summary rewrite.
func trackedNoIssuesBody(prior *format.Ledger, opts Options, message string) (string, int, int) {
	merged, thisRun := mergeLedger(prior, opts.Coverage, nil, nil, opts.RunTag, resolvedToday(), opts.OverlapThresh)
	content := format.BuildNoIssuesBody(message)
	if len(merged.Open) > 0 || len(thisRun) > 0 {
		// Surviving open entries (e.g. a delta run whose coverage does
		// not reach them) must stay visible instead of a bare LGTM body.
		content += format.FormatLedgerSections(merged.Open, thisRun)
	}
	return format.WrapSummaryBody(content+"\n"+format.EncodeLedger(merged), opts.RunTag), len(thisRun), len(merged.Open)
}
