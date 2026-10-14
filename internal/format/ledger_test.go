package format

import (
	"errors"
	"strings"
	"testing"
)

func TestStripLedgerBodyWithBlock(t *testing.T) {
	body := "intro\n\n<!-- ocr-ledger -->\n{\"version\":1}\n<!-- /ocr-ledger -->"
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
	body := "body\n\n<!-- ocr-ledger -->\n{broken\n<!-- /ocr-ledger -->"
	if _, _, _, err := StripLedger(body); err == nil {
		t.Fatal("expected error for malformed ledger block")
	}
}

func TestStripLedgerTruncatedBlockReturnsError(t *testing.T) {
	body := "body\n\n<!-- ocr-ledger -->\n{\"version\":1}"
	_, _, _, err := StripLedger(body)
	if err == nil {
		t.Fatal("expected error for truncated ledger block")
	}
	if !errors.Is(err, errTruncatedLedger) && !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("expected truncation error, got %v", err)
	}
}
