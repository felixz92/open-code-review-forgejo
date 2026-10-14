package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteStatsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocr-stats.env")
	stats := &Stats{Total: 5, Inline: 2, Summary: 1, Routed: 1, Skipped: 0, Failed: 1, Resolved: 4, SummaryURL: "comment:55"}
	if err := WriteStatsFile(path, stats); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"OCR_COMMENTS_TOTAL=5",
		"OCR_COMMENTS_INLINE=2",
		"OCR_COMMENTS_SUMMARY=1",
		"OCR_COMMENTS_ROUTED=1",
		"OCR_COMMENTS_SKIPPED=0",
		"OCR_COMMENTS_FAILED=1",
		"OCR_FINDINGS_RESOLVED=4",
		"OCR_SUMMARY_URL=comment:55",
	}
	got := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("lines = %v, want %d", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWriteStatsFileZeroFilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocr-stats.env")
	if err := WriteStatsFile(path, &Stats{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "OCR_COMMENTS_TOTAL=0") || !strings.Contains(string(data), "OCR_SUMMARY_URL=") {
		t.Fatalf("zero stats not written: %q", data)
	}
	if !strings.Contains(string(data), "OCR_FINDINGS_RESOLVED=0") {
		t.Fatalf("resolved count must be zero-filled: %q", data)
	}
}

func TestWriteStatsFileIncludesResolvedCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocr-stats.env")
	stats := &Stats{Total: 3, Inline: 3, Resolved: 2}
	if err := WriteStatsFile(path, stats); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "OCR_FINDINGS_RESOLVED=2") {
		t.Fatalf("resolved count missing from stats file: %q", data)
	}
	if !strings.Contains(string(data), "OCR_COMMENTS_TOTAL=3") {
		t.Fatalf("existing contract lines must be intact: %q", data)
	}
}

func TestWriteStatsFileCreatesDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "ocr-stats.env")
	if err := WriteStatsFile(path, &Stats{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stats file should exist: %v", err)
	}
}
