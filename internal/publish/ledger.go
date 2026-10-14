package publish

import (
	"fmt"
	"log"
	"strings"
	"time"

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
func mergeLedger(prior *format.Ledger, candidates, posted []inlineItem, runTag, resolvedDate string, thresh float64) (*format.Ledger, []format.LedgerEntry) {
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
func trackedNoIssuesBody(p poster.Poster, opts Options, message string) (string, int) {
	prior := loadLedger(p, opts)
	merged, thisRun := mergeLedger(prior, nil, nil, opts.RunTag, resolvedToday(), opts.OverlapThresh)
	content := format.BuildNoIssuesBody(message)
	if len(thisRun) > 0 {
		content += format.FormatLedgerSections(nil, thisRun)
	}
	return format.WrapSummaryBody(content+"\n"+format.EncodeLedger(merged), opts.RunTag), len(thisRun)
}
