package publish

import (
	"github.com/felixz92/open-code-review-forgejo/internal/format"
	"github.com/felixz92/open-code-review-forgejo/internal/incremental"
)

// maxSnippetRunes caps a finding's stored text so the ledger summary cannot
// balloon.
const maxSnippetRunes = 200

// snippet truncates a finding text for ledger storage.
func snippet(content string) string {
	runes := []rune(content)
	if len(runes) <= maxSnippetRunes {
		return content
	}
	return string(runes[:maxSnippetRunes]) + "…"
}

// mergeLedger folds the current run's inline findings into the prior ledger
// and returns the merged ledger plus the number of open entries this run
// resolved. Matching reuses the incremental span semantics: posted findings
// (plus incrementally skipped ones, which were already posted by a prior run
// and are still live findings) keep matching entries open; findings that
// match no entry are added as new open entries; open entries matched by no
// current finding move to the resolved list with the resolution stamp
// (design D2/D3). The resolved list is pruned to MaxResolvedEntries.
func mergeLedger(prior *format.Ledger, posted, skipped []inlineItem, runTag, resolvedDate string, thresh float64) (*format.Ledger, int) {
	if prior == nil {
		prior = &format.Ledger{Version: format.LedgerVersion}
	}
	next := &format.Ledger{Version: format.LedgerVersion, Resolved: append([]format.LedgerEntry(nil), prior.Resolved...)}
	open := append([]format.LedgerEntry(nil), prior.Open...)
	matchedOpen := make([]bool, len(open))

	matchSet := make([]inlineItem, 0, len(posted)+len(skipped))
	matchSet = append(matchSet, posted...)
	matchSet = append(matchSet, skipped...)
	matchedCurrent := make([]bool, len(matchSet))

	for i, it := range matchSet {
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
			matchedOpen[j], matchedCurrent[i] = true, true
			// Refresh the recorded span and wording so slow line drift
			// does not accumulate into a mismatch (design D2).
			open[j].StartLine, open[j].EndLine = it.span.Start, it.span.End
			open[j].Content = snippet(it.comment.Content)
			break
		}
	}

	resolvedCount := 0
	for j, e := range open {
		if matchedOpen[j] {
			next.Open = append(next.Open, e) // survivors keep ledger order
			continue
		}
		e.ResolvedIn, e.ResolvedDate = runTag, resolvedDate
		next.Resolved = append(next.Resolved, e)
		resolvedCount++
	}
	for i, it := range matchSet {
		if i >= len(posted) {
			break // only successfully posted findings enter the ledger
		}
		if it.span == nil || it.comment == nil || matchedCurrent[i] {
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
	return next, resolvedCount
}
