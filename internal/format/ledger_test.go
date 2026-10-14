package format

import (
	"errors"
	"strings"
	"testing"
)

func TestEncodeLedgerRoundTrip(t *testing.T) {
	in := &Ledger{
		Version: LedgerVersion,
		Open: []LedgerEntry{
			{Path: "a.go", StartLine: 3, EndLine: 9, FirstSeen: "run-1", Content: "unused var"},
		},
		Resolved: []LedgerEntry{
			{Path: "b.go", StartLine: 1, EndLine: 1, FirstSeen: "run-1", ResolvedIn: "run-2", ResolvedDate: "2026-10-14", Content: "off-by-one"},
		},
	}
	block := EncodeLedger(in)
	if !strings.HasPrefix(block, LedgerStartMarker) || !strings.HasSuffix(block, LedgerEndMarker) {
		t.Fatalf("ledger block must be wrapped in markers, got %q", block)
	}
	got, found, err := ParseLedger(block)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected ledger block to be found")
	}
	if got.Version != LedgerVersion || len(got.Open) != 1 || len(got.Resolved) != 1 {
		t.Fatalf("round trip lost data: %+v", got)
	}
	e := got.Open[0]
	if e != (LedgerEntry{Path: "a.go", StartLine: 3, EndLine: 9, FirstSeen: "run-1", Content: "unused var"}) {
		t.Fatalf("open entry mismatch: %+v", e)
	}
	if got.Resolved[0].ResolvedIn != "run-2" || got.Resolved[0].ResolvedDate != "2026-10-14" {
		t.Fatalf("resolved entry mismatch: %+v", got.Resolved[0])
	}
}

func TestParseLedgerMalformedBlockReturnsError(t *testing.T) {
	body := LedgerStartMarker + "not json at all" + LedgerEndMarker
	if _, _, err := ParseLedger(body); err == nil {
		t.Fatal("expected error for corrupt ledger JSON")
	}
}

func TestParseLedgerMissingBodyMarkerReturnsNotFound(t *testing.T) {
	got, found, err := ParseLedger("just a body without any ledger block")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected not-found when body has no ledger block")
	}
	if got != nil {
		t.Fatalf("expected nil ledger when not found, got %+v", got)
	}
}

func TestParseLedgerTolerantOfMissingFields(t *testing.T) {
	body := LedgerStartMarker + `{"open":[{"path":"a.go","start_line":2,"end_line":2}]}` + LedgerEndMarker
	got, found, err := ParseLedger(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected ledger to be found")
	}
	if got.Version != LedgerVersion {
		t.Fatalf("missing version should decode to current version, got %d", got.Version)
	}
	e := got.Open[0]
	if e.Path != "a.go" || e.StartLine != 2 || e.EndLine != 2 || e.FirstSeen != "" {
		t.Fatalf("entry fields missing from payload must decode as zero values, got %+v", e)
	}
}

func TestParseLedgerRejectsUnsupportedNewerVersion(t *testing.T) {
	body := LedgerStartMarker + `{"version":42,"open":[]}` + LedgerEndMarker
	if _, _, err := ParseLedger(body); err == nil {
		t.Fatal("expected error for unsupported newer ledger version")
	}
}

func TestStripLedgerBodyWithBlock(t *testing.T) {
	body := "intro\n\n" + LedgerStartMarker + `{"version":1}` + LedgerEndMarker
	clean, _, found, err := StripLedger(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected block to be found")
	}
	if clean != "intro" {
		t.Fatalf("block must be stripped including separator newlines, got %q", clean)
	}
}

func TestEncodeLedgerSelfContainedComment(t *testing.T) {
	// The payload must stay inside one HTML comment line: a start marker
	// that closes itself would render the JSON as visible Markdown text.
	block := EncodeLedger(&Ledger{Version: LedgerVersion})
	if strings.ContainsAny(block, "\n") {
		t.Fatalf("ledger block must be a single self-contained HTML comment line, got %q", block)
	}
	if !strings.HasPrefix(block, "<!--") || !strings.HasSuffix(block, "-->") {
		t.Fatalf("ledger block must be one HTML comment, got %q", block)
	}
}

func TestStripLedgerIgnoresLedgerMarkerQuotedInContent(t *testing.T) {
	block := EncodeLedger(&Ledger{
		Version: LedgerVersion,
		Open:    []LedgerEntry{{Path: "b.go", StartLine: 7, EndLine: 7, Content: "naming"}},
	})
	body := "### 📄 `a.go`\n\nquotes the marker: <!-- ocr-ledger -->\n\n" + block
	clean, l, found, err := StripLedger(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found || l == nil || len(l.Open) != 1 || l.Open[0].Path != "b.go" {
		t.Fatalf("real block (appended last) must win over a marker quoted in content, got found=%v ledger=%+v", found, l)
	}
	if !strings.Contains(clean, "quotes the marker") {
		t.Fatalf("content before the block must be preserved, got %q", clean)
	}
}

func TestStripLedgerBlockEmbeddedMidBody(t *testing.T) {
	body := "first\n\n" + LedgerStartMarker + `{"version":1}` + LedgerEndMarker + "\n\nafter text"
	clean, _, found, err := StripLedger(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected block to be found")
	}
	if want := "first\n\nafter text"; clean != want {
		t.Fatalf("mid-body block must be stripped and rejoined with a paragraph break, got %q", clean)
	}
}

func TestStripLedgerBodyWithBlockAtStart(t *testing.T) {
	body := LedgerStartMarker + `{"version":1}` + LedgerEndMarker + "\n\nafter text"
	clean, _, found, err := StripLedger(body)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected block to be found")
	}
	if clean != "after text" {
		t.Fatalf("block at the very start must leave the following text, got %q", clean)
	}
}

func TestStripLedgerBodyWithoutBlock(t *testing.T) {
	body := "just a classic summary\n\nwith paragraphs"
	clean, l, found, err := StripLedger(body)
	if err != nil {
		t.Fatal(err)
	}
	if found || l != nil {
		t.Fatalf("expected not-found, got found=%v ledger=%+v", found, l)
	}
	if clean != body {
		t.Fatalf("body without block must be unchanged, got %q", clean)
	}
}

func TestStripLedgerMalformedBlockReturnsError(t *testing.T) {
	body := "body\n\n" + LedgerStartMarker + "{broken" + LedgerEndMarker
	if _, _, _, err := StripLedger(body); err == nil {
		t.Fatal("expected error for malformed ledger block")
	}
}

func TestStripLedgerTruncatedBlockReturnsError(t *testing.T) {
	body := "body\n\n" + LedgerStartMarker + `{"version":1}`
	_, _, _, err := StripLedger(body)
	if err == nil {
		t.Fatal("expected error for truncated ledger block")
	}
	if !errors.Is(err, errTruncatedLedger) {
		t.Fatalf("expected truncation error, got %v", err)
	}
}

func TestFormatLedgerSectionsOpenAndResolved(t *testing.T) {
	open := []LedgerEntry{
		{Path: "z.go", StartLine: 5, EndLine: 9, FirstSeen: "run-1", Content: "zeta"},
		{Path: "a.go", StartLine: 1, EndLine: 1, FirstSeen: "run-1", Content: "alpha"},
	}
	resolved := []LedgerEntry{{Path: "m.go", StartLine: 3, EndLine: 4, FirstSeen: "run-1", ResolvedIn: "run-2", ResolvedDate: "2026-10-14", Content: "mid"}}
	out := FormatLedgerSections(open, resolved)
	if !strings.Contains(out, "still open: 2") {
		t.Fatalf("open section must state the open count: %q", out)
	}
	for _, want := range []string{"`z.go`", "`a.go`", "(L1)", "(L5-L9)", "alpha", "zeta", "`m.go`", "(L3-L4)", "run-2", "2026-10-14"} {
		if !strings.Contains(out, want) {
			t.Fatalf("section missing %q: %s", want, out)
		}
	}
	if !strings.Contains(out, "Resolved **1**") {
		t.Fatalf("resolved count must be stated: %q", out)
	}
	if idx := strings.Index(out, "still open"); idx < 0 || strings.Index(out, "Resolved") < idx {
		t.Fatalf("open section must come first: %q", out)
	}
	if strings.Index(out, "`a.go`") > strings.Index(out, "`z.go`") {
		t.Fatalf("open findings must render sorted by path: %q", out)
	}
	if !strings.Contains(out, "✅") {
		t.Fatalf("resolved entries need a resolved indication: %q", out)
	}
}

func TestFormatLedgerSectionsOpenOnlyStatesNoResolutionCount(t *testing.T) {
	open := []LedgerEntry{{Path: "a.go", StartLine: 1, EndLine: 1, FirstSeen: "run-1", Content: "alpha"}}
	out := FormatLedgerSections(open, nil)
	if strings.Contains(out, "Resolved") || strings.Contains(out, "resolved") {
		t.Fatalf("no resolved section when nothing resolved this run: %q", out)
	}
	if !strings.Contains(out, "`a.go`") || !strings.Contains(out, "alpha") {
		t.Fatalf("open findings must render: %q", out)
	}
}

func TestFormatLedgerSectionsEmptyLedgerRendersNothing(t *testing.T) {
	if out := FormatLedgerSections(nil, nil); out != "" {
		t.Fatalf("empty ledger must render nothing, got %q", out)
	}
}
