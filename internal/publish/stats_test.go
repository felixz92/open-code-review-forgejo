package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

func TestWriteStatsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocr-stats.env")
	stats := &Stats{Total: 5, Inline: 2, Summary: 1, Routed: 1, Skipped: 0, Failed: 1, SummaryURL: "comment:55"}
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

func TestShouldFailOnSeverity(t *testing.T) {
	r := result(c("a.go", 1, 1, "medium"), c("b.go", 1, 1, "low"))
	if ShouldFailOnSeverity(r, "high") {
		t.Fatal("medium is below high, must not trip")
	}
	if !ShouldFailOnSeverity(r, "medium") {
		t.Fatal("at-threshold must trip")
	}
	if !ShouldFailOnSeverity(r, "low") {
		t.Fatal("low threshold must trip on any known severity")
	}
	if ShouldFailOnSeverity(result(), "critical") {
		t.Fatal("empty result must not trip")
	}
	if ShouldFailOnSeverity(result(review.Comment{Severity: "weird"}), "high") {
		t.Fatal("unknown severity must not trip")
	}
	if ShouldFailOnSeverity(r, "") {
		t.Fatal("no threshold must never trip")
	}
}
