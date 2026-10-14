package publish

import (
	"errors"
	"strings"
	"testing"

	"github.com/felixz92/open-code-review-forgejo/internal/format"
	"github.com/felixz92/open-code-review-forgejo/internal/incremental"
	"github.com/felixz92/open-code-review-forgejo/internal/policy"
	"github.com/felixz92/open-code-review-forgejo/internal/poster"
	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

func mergeItem(path string, start, end int64) inlineItem {
	c := review.Comment{Path: path, StartLine: start, EndLine: end, Content: "content " + path}
	return inlineItem{comment: &c, span: incremental.NewSpan(start, end)}
}

func TestMergeLedgerPersistsMatchedFinding(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1", Content: "content a.go"}},
	}
	var posted []inlineItem
	posted = append(posted, mergeItem("a.go", 10, 10))
	next, resolved := mergeLedger(prior, posted, posted, "run-2", "2026-10-14", 0.6)
	if len(resolved) != 0 {
		t.Fatalf("matched entry must stay open, resolved=%d", len(resolved))
	}
	if len(next.Open) != 1 {
		t.Fatalf("expected single open entry, got %+v", next.Open)
	}
	e := next.Open[0]
	if e.Path != "a.go" || e.StartLine != 10 || e.EndLine != 10 || e.FirstSeen != "run-1" {
		t.Fatalf("matched entry must persist unchanged, got %+v", e)
	}
	if e.ResolvedIn != "" || e.ResolvedDate != "" {
		t.Fatalf("open entry must not carry resolution, got %+v", e)
	}
	if len(next.Resolved) != 0 {
		t.Fatalf("nothing must be resolved, got %+v", next.Resolved)
	}
}

func TestMergeLedgerResolvesUnmatchedAndAddsNew(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1"}},
	}
	var posted []inlineItem
	posted = append(posted, mergeItem("b.go", 5, 5))
	next, resolved := mergeLedger(prior, posted, posted, "run-2", "2026-10-14", 0.6)
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(resolved))
	}
	if len(next.Open) != 1 || next.Open[0].Path != "b.go" {
		t.Fatalf("new finding must be added as open entry, got %+v", next.Open)
	}
	if next.Open[0].FirstSeen != "run-2" || next.Open[0].Content != "content b.go" {
		t.Fatalf("new entry must record first-seen run and content, got %+v", next.Open[0])
	}
	if len(next.Resolved) != 1 {
		t.Fatalf("unmatched open entry must move to resolved, got %+v", next.Resolved)
	}
	r := next.Resolved[0]
	if r.Path != "a.go" || r.ResolvedIn != "run-2" || r.ResolvedDate != "2026-10-14" {
		t.Fatalf("resolved entry must record run and date, got %+v", r)
	}
}

func TestMergeLedgerLineShiftKeepsSameEntry(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 50, EndLine: 60, FirstSeen: "run-1"}},
	}
	var posted []inlineItem
	posted = append(posted, mergeItem("a.go", 52, 61))
	next, resolved := mergeLedger(prior, posted, posted, "run-2", "2026-10-14", 0.6)
	if len(resolved) != 0 {
		t.Fatalf("shifted span within IoU threshold must stay open, resolved=%d", len(resolved))
	}
	if len(next.Open) != 1 {
		t.Fatalf("entry must not duplicate, got %+v", next.Open)
	}
	if s, e := inlineSpanOf(next.Open[0]); s != 52 || e != 61 {
		t.Fatalf("recorded span must refresh to current run's span, got L%d-L%d", s, e)
	}
}

func inlineSpanOf(e format.LedgerEntry) (int64, int64) {
	return e.StartLine, e.EndLine
}

func TestMergeLedgerIsPathScoped(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1"}},
	}
	var posted []inlineItem
	posted = append(posted, mergeItem("b.go", 10, 10))
	next, resolved := mergeLedger(prior, posted, posted, "run-2", "2026-10-14", 0.6)
	if len(resolved) != 1 || len(next.Open) != 1 || next.Open[0].Path != "b.go" {
		t.Fatalf("same span on another path must not match: resolved=%d open=%+v", len(resolved), next.Open)
	}
}

func TestMergeLedgerSkippedFindsKeepEntryOpenWithoutAdding(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1"}},
	}
	var skipped []inlineItem
	skipped = append(skipped, mergeItem("a.go", 10, 10))
	next, resolved := mergeLedger(prior, skipped, nil, "run-2", "2026-10-14", 0.6)
	if len(resolved) != 0 {
		t.Fatalf("incrementally skipped finding must keep entry open, resolved=%d", len(resolved))
	}
	if len(next.Open) != 1 || next.Open[0].FirstSeen != "run-1" {
		t.Fatalf("skipped finding must not be re-added, got %+v", next.Open)
	}
}

func TestMergeLedgerPrunesResolvedBeyondCap(t *testing.T) {
	stamped := make([]format.LedgerEntry, 0, format.MaxResolvedEntries)
	for i := 0; i < format.MaxResolvedEntries; i++ {
		stamped = append(stamped, format.LedgerEntry{Path: "old.go", StartLine: int64(i), EndLine: int64(i), FirstSeen: "run-1"})
	}
	prior := &format.Ledger{
		Version:  format.LedgerVersion,
		Open:     []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1"}},
		Resolved: stamped,
	}
	next, thisRun := mergeLedger(prior, nil, nil, "run-2", "2026-10-14", 0.6)
	if len(thisRun) != 1 || thisRun[0].Path != "a.go" {
		t.Fatalf("expected the open entry to resolve this run, got %+v", thisRun)
	}
	if len(next.Resolved) != format.MaxResolvedEntries {
		t.Fatalf("resolved list must be capped at %d, got %d", format.MaxResolvedEntries, len(next.Resolved))
	}
	retainedThisRun := false
	for _, e := range next.Resolved {
		if e.Path == "old.go" && e.StartLine == 0 {
			t.Fatalf("oldest resolved entry must be pruned first; kept %+v", e)
		}
		if e.Path == "a.go" && e.ResolvedIn == "run-2" {
			retainedThisRun = true
		}
	}
	if !retainedThisRun {
		t.Fatal("this run's resolution must be retained (most recent kept)")
	}
}

func TestMergeLedgerNilPriorSeedsFresh(t *testing.T) {
	var posted []inlineItem
	posted = append(posted, mergeItem("a.go", 1, 3))
	next, resolved := mergeLedger(nil, posted, posted, "run-1", "2026-10-14", 0.6)
	if len(resolved) != 0 {
		t.Fatalf("fresh ledger must not resolve anything, got %d", len(resolved))
	}
	if len(next.Open) != 1 || next.Open[0].FirstSeen != "run-1" {
		t.Fatalf("fresh ledger must seed from posted findings, got %+v", next.Open)
	}
}

func TestMergeLedgerKeepsStableOpenOrder(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open: []format.LedgerEntry{
			{Path: "a.go", StartLine: 1, EndLine: 1, FirstSeen: "run-1"},
			{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1"},
		},
	}
	var posted []inlineItem
	posted = append(posted, mergeItem("a.go", 1, 1), mergeItem("a.go", 10, 20))
	next, _ := mergeLedger(prior, posted, posted, "run-2", "2026-10-14", 0.6)
	if len(next.Open) != 2 || next.Open[0].StartLine != 1 || next.Open[1].StartLine != 10 {
		t.Fatalf("open entries must keep stable order with new ones appended, got %+v", next.Open)
	}
}

func loadLedgerWithStickyBody(t *testing.T, body string) *format.Ledger {
	t.Helper()
	f := newFake()
	f.issueComments = []poster.IssueCommentInfo{
		{ID: 1, Body: "unrelated"},
		{ID: 2, Body: format.SummaryMarker + "\n<!-- ocr-summary run: own -->\nold body\n" + body},
	}
	return loadLedger(f, Options{RunTag: "t", Sticky: true})
}

func TestLoadLedgerParsesPriorLedgerFromStickySummary(t *testing.T) {
	ledger := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 1, EndLine: 1, FirstSeen: "run-1"}},
	}
	l := loadLedgerWithStickyBody(t, format.EncodeLedger(ledger))
	if l == nil || len(l.Open) != 1 || l.Open[0].Path != "a.go" {
		t.Fatalf("prior ledger must be parsed from the sticky summary, got %+v", l)
	}
}

func TestLoadLedgerFailsOpenOnUnreadableSummary(t *testing.T) {
	f := newFake()
	f.issueCommentsErr = errors.New("list boom")
	l := loadLedger(f, Options{RunTag: "t", Sticky: true})
	if l == nil || l.Version != format.LedgerVersion || len(l.Open) != 0 || len(l.Resolved) != 0 {
		t.Fatalf("unreadable summary must degrade to a fresh ledger, got %+v", l)
	}
}

func TestLoadLedgerFailsOpenOnCorruptLedger(t *testing.T) {
	l := loadLedgerWithStickyBody(t, format.LedgerStartMarker+"\n{corrupt\n"+format.LedgerEndMarker)
	if l == nil || l.Version != format.LedgerVersion || len(l.Open) != 0 || len(l.Resolved) != 0 {
		t.Fatalf("corrupt ledger must degrade to a fresh ledger, got %+v", l)
	}
}

func TestLoadLedgerFreshWhenSummaryLacksLedger(t *testing.T) {
	l := loadLedgerWithStickyBody(t, "plain summary body")
	if l == nil || l.Version != format.LedgerVersion || len(l.Open) != 0 || len(l.Resolved) != 0 {
		t.Fatalf("missing ledger block must yield a fresh ledger, got %+v", l)
	}
}

func TestPublishCleanRunResolvesAllOpenLedger(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1", Content: "content a.go"}},
	}
	f := newFake()
	f.issueComments = []poster.IssueCommentInfo{{ID: 31, Body: format.SummaryMarker + "\nold body\n" + format.EncodeLedger(prior)}}
	r := &review.Result{Message: "nothing to comment on"}
	stats, err := Publish(r, f, Options{RunTag: "run-2", CommitID: "s", Sticky: true, ResolutionTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Resolved != 1 {
		t.Fatalf("clean run must resolve the one open entry, got %d", stats.Resolved)
	}
	body, ok := f.editedSummary[31]
	if !ok {
		t.Fatalf("summary must update in place, summaries=%v", f.createdSummary)
	}
	if !strings.Contains(body, "Resolved **1** finding(s) in this run") {
		t.Fatalf("clean run with open ledger must report resolutions, got %q", body)
	}
	if !strings.Contains(body, "✅ RESOLVED (run run-2") || !strings.Contains(body, "`a.go` (L10)") {
		t.Fatalf("resolved entries must render with stamp and span, got %q", body)
	}
	_, l, found, err := format.StripLedger(body)
	if err != nil || !found {
		t.Fatalf("summary must carry the ledger block: found=%v err=%v", found, err)
	}
	if len(l.Open) != 0 || len(l.Resolved) != 1 || l.Resolved[0].Path != "a.go" {
		t.Fatalf("ledger must move the entry to resolved, got %+v", l)
	}
}

func TestPublishCleanRunEmptyLedgerKeepsExistingBody(t *testing.T) {
	f := newFake()
	f.issueComments = []poster.IssueCommentInfo{{ID: 31, Body: format.SummaryMarker + "\nold body"}}
	r := &review.Result{Message: "done"}
	stats, err := Publish(r, f, Options{RunTag: "run-2", CommitID: "s", Sticky: true, ResolutionTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Resolved != 0 {
		t.Fatalf("empty ledger must resolve nothing, got %d", stats.Resolved)
	}
	body := f.editedSummary[31]
	if strings.Contains(body, "Resolved") || strings.Contains(body, "RESOLVED") {
		t.Fatalf("no resolved section for an empty ledger, got %q", body)
	}
	if !strings.Contains(body, "**OpenCodeReview**: done") {
		t.Fatalf("existing no-issues body must be kept, got %q", body)
	}
	if _, l, found, err := format.StripLedger(body); err != nil || !found || len(l.Open) != 0 || len(l.Resolved) != 0 {
		t.Fatalf("empty ledger must persist in the summary body, found=%v err=%v ledger=%+v", found, err, l)
	}
}

func finalSummaryBody(f *fakePoster) string {
	var body string
	for _, b := range f.editedSummary {
		if body != "" {
			panic("test fake expects at most one summary edit")
		}
		body = b
	}
	if body == "" && len(f.createdSummary) > 0 {
		body = f.createdSummary[len(f.createdSummary)-1]
	}
	return body
}

func TestPublishTrackingDisabledStripsLegacyLedgerAndMatchesClassicBody(t *testing.T) {
	legacyPrior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 1, EndLine: 1, FirstSeen: "run-1"}},
	}
	withPrior := newFake()
	withPrior.issueComments = []poster.IssueCommentInfo{{
		ID:   31,
		Body: format.SummaryMarker + "\nold body\n" + format.EncodeLedger(legacyPrior),
	}}
	r := result(c("a.go", 3, 3, "low"))
	if _, err := Publish(r, withPrior, Options{RunTag: "t", CommitID: "s", Sticky: true}); err != nil {
		t.Fatal(err)
	}
	withoutPrior := newFake()
	if _, err := Publish(result(c("a.go", 3, 3, "low")), withoutPrior, Options{RunTag: "t", CommitID: "s", Sticky: true}); err != nil {
		t.Fatal(err)
	}
	b1 := finalSummaryBody(withPrior)
	b2 := finalSummaryBody(withoutPrior)
	if b1 == "" || b2 == "" {
		t.Fatalf("both runs must produce a final summary: %q / %q", b1, b2)
	}
	if strings.Contains(b1, format.LedgerStartMarker) || strings.Contains(b1, format.LedgerEndMarker) {
		t.Fatalf("tracking-off summary must strip the legacy ledger block, got %q", b1)
	}
	if b1 != b2 {
		t.Fatalf("tracking-off summary must be byte-identical to the classic format:\nwant %q\ngot  %q", b2, b1)
	}
}

func TestPublishTracksPostedFindingsThenResolves(t *testing.T) {
	// Run 1: one posting-eligible finding, posted inline.
	f1 := newFake()
	stats1, err := Publish(result(c("a.go", 1, 1, "high")), f1, Options{RunTag: "run-1", CommitID: "s", Sticky: true, ResolutionTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Inline != 1 || stats1.Resolved != 0 {
		t.Fatalf("run 1 must post 1 inline and resolve none, got %+v", stats1)
	}
	summary1 := finalSummaryBody(f1)
	_, l1, found1, err := format.StripLedger(summary1)
	if err != nil || !found1 {
		t.Fatalf("run 1 summary must embed the ledger: found=%v err=%v", found1, err)
	}
	if len(l1.Open) != 1 || l1.Open[0].Path != "a.go" || l1.Open[0].FirstSeen != "run-1" {
		t.Fatalf("run 1 ledger must open the posted finding, got %+v", l1)
	}

	// Run 2: same PR, the finding is fixed (zero results) -> resolved.
	f2 := newFake()
	f2.issueComments = []poster.IssueCommentInfo{{ID: 71, Body: summary1}}
	stats2, err := Publish(result(), f2, Options{RunTag: "run-2", CommitID: "s", Sticky: true, ResolutionTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Resolved != 1 {
		t.Fatalf("run 2 must resolve the open entry, got %d", stats2.Resolved)
	}
	summary2 := finalSummaryBody(f2)
	if !strings.Contains(summary2, "Resolved **1** finding(s) in this run") {
		t.Fatalf("run 2 summary must report the resolution, got %q", summary2)
	}
	_, l2, found2, err := format.StripLedger(summary2)
	if err != nil || !found2 {
		t.Fatalf("run 2 summary must embed the ledger: found=%v err=%v", found2, err)
	}
	if len(l2.Open) != 0 || len(l2.Resolved) != 1 || l2.Resolved[0].ResolvedIn != "run-2" || l2.Resolved[0].ResolvedDate == "" {
		t.Fatalf("run 2 ledger must hold the resolved entry, got %+v", l2)
	}

	// Run 3: a new finding posts; earlier resolutions stay in the ledger
	// state but must not re-render into the visible resolved section.
	f3 := newFake()
	f3.issueComments = []poster.IssueCommentInfo{{ID: 72, Body: summary2}}
	stats3, err := Publish(result(c("b.go", 5, 6, "high")), f3, Options{RunTag: "run-3", CommitID: "s", Sticky: true, ResolutionTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats3.Inline != 1 || stats3.Resolved != 0 {
		t.Fatalf("run 3 must add 1 and resolve none, got %+v", stats3)
	}
	body3 := finalSummaryBody(f3)
	if strings.Contains(body3, "✅ RESOLVED (run run-2") || strings.Contains(body3, "Resolved **1**") {
		t.Fatalf("prior-run resolutions must not re-render into the summary, got %q", body3)
	}
	_, l3, found3, err := format.StripLedger(body3)
	if err != nil || !found3 {
		t.Fatalf("run 3 summary must embed the ledger, found=%v err=%v", found3, err)
	}
	if len(l3.Open) != 1 || l3.Open[0].Path != "b.go" || len(l3.Resolved) != 1 || l3.Resolved[0].ResolvedIn != "run-2" {
		t.Fatalf("prior resolutions must persist in the ledger state, got %+v", l3)
	}
}

func TestPublishFailedAndRoutedCommentsStayOutOfLedger(t *testing.T) {
	f := newFake()
	f.createErr = errors.New("review boom")
	stats, err := Publish(result(c("a.go", 1, 1, "low")), f, Options{RunTag: "t", CommitID: "s", Sticky: true, ResolutionTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 1 {
		t.Fatalf("expected the comment to fail, got %+v", stats)
	}
	_, l, found, err := format.StripLedger(finalSummaryBody(f))
	if err != nil || !found {
		t.Fatalf("summary must embed the ledger, found=%v err=%v", found, err)
	}
	if len(l.Open) != 0 {
		t.Fatalf("failed comments must not enter the ledger, got %+v", l)
	}
}

func TestPublishRoutedFindingKeepsPriorLedgerEntryOpen(t *testing.T) {
	f1 := newFake()
	if _, err := Publish(result(c("a.go", 1, 1, "high")), f1, Options{RunTag: "run-1", CommitID: "s", Sticky: true, ResolutionTracking: true}); err != nil {
		t.Fatal(err)
	}
	summary1 := finalSummaryBody(f1)

	// Run 2: the finding is still reported but policy now routes it to the
	// summary. It must keep the prior entry open (never resolve what OCR
	// still reports); routed comments never create entries.
	f2 := newFake()
	f2.issueComments = []poster.IssueCommentInfo{{ID: 71, Body: summary1}}
	stats2, err := Publish(result(c("a.go", 1, 1, "low")), f2, Options{
		RunTag: "run-2", CommitID: "s", Sticky: true, ResolutionTracking: true,
		Route: policy.Build("low", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Routed != 1 || stats2.Resolved != 0 {
		t.Fatalf("routed finding must not resolve its entry, routed=%d resolved=%d", stats2.Routed, stats2.Resolved)
	}
	_, l, found, err := format.StripLedger(finalSummaryBody(f2))
	if err != nil || !found {
		t.Fatalf("summary must embed the ledger, found=%v err=%v", found, err)
	}
	if len(l.Open) != 1 || l.Open[0].FirstSeen != "run-1" || l.Open[0].StartLine != 1 {
		t.Fatalf("prior entry must stay open untouched, got %+v", l.Open)
	}
}

func TestPublishPrecheckDropsAndSubmitFailuresKeepPriorEntriesOpen(t *testing.T) {
	t.Run("diff pre-check drop", func(t *testing.T) {
		prior := &format.Ledger{
			Version: format.LedgerVersion,
			Open:    []format.LedgerEntry{{Path: "b.go", StartLine: 1, EndLine: 1, FirstSeen: "run-1"}},
		}
		f := newFake()
		f.issueComments = []poster.IssueCommentInfo{{ID: 71, Body: format.SummaryMarker + "\nold\n" + format.EncodeLedger(prior)}}
		// The finding sits in b.go, which no longer exists in the diff
		// (only a.go changed): the pre-check drops it as out of diff.
		f.diff = []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,2 @@\n keep()\n-removed()\n+added()\n")
		stats, err := Publish(result(c("b.go", 1, 1, "high")), f, Options{RunTag: "run-2", CommitID: "s", Sticky: true, ResolutionTracking: true})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Failed != 1 || stats.Resolved != 0 {
			t.Fatalf("dropped candidate must keep its entry open, failed=%d resolved=%d", stats.Failed, stats.Resolved)
		}
		_, l, found, err := format.StripLedger(finalSummaryBody(f))
		if err != nil || !found {
			t.Fatalf("summary must embed the ledger, found=%v err=%v", found, err)
		}
		if len(l.Open) != 1 || l.Open[0].StartLine != 1 || l.Open[0].FirstSeen != "run-1" {
			t.Fatalf("prior entry must remain open untouched, got %+v", l.Open)
		}
	})
	t.Run("review submit failure", func(t *testing.T) {
		prior := &format.Ledger{
			Version: format.LedgerVersion,
			Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 1, EndLine: 1, FirstSeen: "run-1"}},
		}
		f := newFake()
		f.issueComments = []poster.IssueCommentInfo{{ID: 71, Body: format.SummaryMarker + "\nold\n" + format.EncodeLedger(prior)}}
		f.createErr = errors.New("review boom")
		stats, err := Publish(result(c("a.go", 1, 1, "low")), f, Options{RunTag: "run-2", CommitID: "s", Sticky: true, ResolutionTracking: true})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Failed != 1 || stats.Resolved != 0 {
			t.Fatalf("submit-failed candidate must keep its entry open, failed=%d resolved=%d", stats.Failed, stats.Resolved)
		}
		_, l, found, err := format.StripLedger(finalSummaryBody(f))
		if err != nil || !found {
			t.Fatalf("summary must embed the ledger, found=%v err=%v", found, err)
		}
		if len(l.Open) != 1 || l.Open[0].FirstSeen != "run-1" {
			t.Fatalf("prior entry must remain open untouched, got %+v", l.Open)
		}
	})
}

func TestPublishUnpostableCommentCategoriesNeverCreateLedgerEntries(t *testing.T) {
	t.Run("failed", func(t *testing.T) {
		f := newFake()
		f.createErr = errors.New("review boom")
		stats, err := Publish(result(c("a.go", 1, 1, "low")), f, Options{RunTag: "t", CommitID: "s", Sticky: true, ResolutionTracking: true})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Failed != 1 {
			t.Fatalf("expected the comment to fail, got %+v", stats)
		}
		_, l, found, err := format.StripLedger(finalSummaryBody(f))
		if err != nil || !found {
			t.Fatalf("summary must embed the ledger, found=%v err=%v", found, err)
		}
		if len(l.Open) != 0 {
			t.Fatalf("failed comments must not enter the ledger, got %+v", l)
		}
	})
	t.Run("routed", func(t *testing.T) {
		f := newFake()
		stats, err := Publish(result(c("a.go", 1, 1, "low")), f, Options{
			RunTag: "t", CommitID: "s", Sticky: true, ResolutionTracking: true,
			Route: policy.Build("low", ""),
		})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Routed != 1 {
			t.Fatalf("expected the comment to be routed, got %+v", stats)
		}
		_, l, found, err := format.StripLedger(finalSummaryBody(f))
		if err != nil || !found {
			t.Fatalf("summary must embed the ledger, found=%v err=%v", found, err)
		}
		if len(l.Open) != 0 {
			t.Fatalf("routed comments must not create ledger entries, got %+v", l)
		}
	})
	t.Run("no-line", func(t *testing.T) {
		f := newFake()
		stats, err := Publish(result(review.Comment{Content: "no line info", Severity: "low"}), f, Options{RunTag: "t", CommitID: "s", Sticky: true, ResolutionTracking: true})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Summary != 1 {
			t.Fatalf("expected the comment in the no-line summary, got %+v", stats)
		}
		_, l, found, err := format.StripLedger(finalSummaryBody(f))
		if err != nil || !found {
			t.Fatalf("summary must embed the ledger, found=%v err=%v", found, err)
		}
		if len(l.Open) != 0 {
			t.Fatalf("no-line comments must not create ledger entries, got %+v", l)
		}
	})
}

func TestPublishRunRendersOpenAndResolvedSectionsInOneSummary(t *testing.T) {
	f1 := newFake()
	if _, err := Publish(result(c("a.go", 1, 1, "high")), f1, Options{RunTag: "run-1", CommitID: "s", Sticky: true, ResolutionTracking: true}); err != nil {
		t.Fatal(err)
	}
	summary1 := finalSummaryBody(f1)

	// Run 2: a.go is fixed, a new finding arrives on b.go.
	f2 := newFake()
	f2.issueComments = []poster.IssueCommentInfo{{ID: 71, Body: summary1}}
	stats2, err := Publish(result(c("b.go", 5, 6, "high")), f2, Options{RunTag: "run-2", CommitID: "s", Sticky: true, ResolutionTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Inline != 1 || stats2.Resolved != 1 {
		t.Fatalf("run 2 must add 1 and resolve 1, got %+v", stats2)
	}
	out := finalSummaryBody(f2)
	if !strings.Contains(out, "Findings still open: 1") || !strings.Contains(out, "`b.go` (L5-L6)") {
		t.Fatalf("open section must render the new finding, got %q", out)
	}
	if !strings.Contains(out, "Resolved **1**") || !strings.Contains(out, "✅ RESOLVED (run run-2") || !strings.Contains(out, "`a.go` (L1)") {
		t.Fatalf("resolved section must render the resolution, got %q", out)
	}
	if idxOpen, idxRes := strings.Index(out, "still open"), strings.Index(out, "Resolved **1**"); idxRes < idxOpen {
		t.Fatalf("open section must come first: %q", out)
	}
}
