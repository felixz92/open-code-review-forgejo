package publish

import (
	"errors"
	"testing"

	"github.com/felixz92/open-code-review-forgejo/internal/format"
	"github.com/felixz92/open-code-review-forgejo/internal/incremental"
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
	var posted, skipped []inlineItem
	posted = append(posted, mergeItem("a.go", 10, 10))
	next, resolved := mergeLedger(prior, posted, skipped, "run-2", "2026-10-14", 0.6)
	if resolved != 0 {
		t.Fatalf("matched entry must stay open, resolved=%d", resolved)
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
	next, resolved := mergeLedger(prior, posted, nil, "run-2", "2026-10-14", 0.6)
	if resolved != 1 {
		t.Fatalf("expected 1 resolution, got %d", resolved)
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
	next, resolved := mergeLedger(prior, posted, nil, "run-2", "2026-10-14", 0.6)
	if resolved != 0 {
		t.Fatalf("shifted span within IoU threshold must stay open, resolved=%d", resolved)
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
	next, resolved := mergeLedger(prior, posted, nil, "run-2", "2026-10-14", 0.6)
	if resolved != 1 || len(next.Open) != 1 || next.Open[0].Path != "b.go" {
		t.Fatalf("same span on another path must not match: resolved=%d open=%+v", resolved, next.Open)
	}
}

func TestMergeLedgerSkippedFindsKeepEntryOpenWithoutAdding(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1"}},
	}
	var skipped []inlineItem
	skipped = append(skipped, mergeItem("a.go", 10, 10))
	next, resolved := mergeLedger(prior, nil, skipped, "run-2", "2026-10-14", 0.6)
	if resolved != 0 {
		t.Fatalf("incrementally skipped finding must keep entry open, resolved=%d", resolved)
	}
	if len(next.Open) != 1 || next.Open[0].FirstSeen != "run-1" {
		t.Fatalf("skipped finding must not be re-added, got %+v", next.Open)
	}
}

func TestMergeLedgerPrunesResolvedBeyondCap(t *testing.T) {
	thisRun := false
	resolved := make([]format.LedgerEntry, 0, format.MaxResolvedEntries)
	for i := 0; i < format.MaxResolvedEntries; i++ {
		resolved = append(resolved, format.LedgerEntry{Path: "old.go", StartLine: int64(i), EndLine: int64(i), FirstSeen: "run-1"})
	}
	prior := &format.Ledger{
		Version:  format.LedgerVersion,
		Open:     []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1"}},
		Resolved: resolved,
	}
	next, resolvedCount := mergeLedger(prior, nil, nil, "run-2", "2026-10-14", 0.6)
	if resolvedCount != 1 {
		t.Fatalf("expected the open entry to resolve this run, got %d", resolvedCount)
	}
	if len(next.Resolved) != format.MaxResolvedEntries {
		t.Fatalf("resolved list must be capped at %d, got %d", format.MaxResolvedEntries, len(next.Resolved))
	}
	for _, e := range next.Resolved {
		if e.Path == "old.go" && e.StartLine == 0 {
			t.Fatalf("oldest resolved entry must be pruned first; kept %+v", e)
		}
		if e.Path == "a.go" && e.ResolvedIn == "run-2" {
			thisRun = true
		}
	}
	if !thisRun {
		t.Fatal("this run's resolution must be retained (most recent kept)")
	}
}

func TestMergeLedgerNilPriorSeedsFresh(t *testing.T) {
	var posted []inlineItem
	posted = append(posted, mergeItem("a.go", 1, 3))
	next, resolved := mergeLedger(nil, posted, nil, "run-1", "2026-10-14", 0.6)
	if resolved != 0 {
		t.Fatalf("fresh ledger must not resolve anything, got %d", resolved)
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
	next, _ := mergeLedger(prior, posted, nil, "run-2", "2026-10-14", 0.6)
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
