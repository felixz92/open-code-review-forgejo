package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRunFixture(t *testing.T, json string) (inputPath, statsPath string) {
	t.Helper()
	dir := t.TempDir()
	input := filepath.Join(dir, "result.json")
	stats := filepath.Join(dir, "ocr-stats.env")
	if err := os.WriteFile(input, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	return input, stats
}

func readRunStats(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("stats file must be written: %v", err)
	}
	return string(data)
}

func TestRunFailedResultExitsOne(t *testing.T) {
	input, stats := writeRunFixture(t, `{"status": "failed", "comments": null}`)
	code := run(&Config{DryRun: true, InputPath: input, StatsFilePath: stats, PRIndex: 1})
	if code != 1 {
		t.Fatalf("run = %d, want 1 for a failed review run", code)
	}
	data := readRunStats(t, stats)
	if !strings.Contains(data, "OCR_RUN_STATUS=failed") {
		t.Fatalf("stats must mark the failed run, got %q", data)
	}
	if !strings.Contains(data, "OCR_COMMENTS_TOTAL=0") {
		t.Fatalf("failed run posts no comment counts, got %q", data)
	}
}

func TestRunCompletedResultExitsZero(t *testing.T) {
	input, stats := writeRunFixture(t, `{"comments": [], "warnings": [], "message": "no issues"}`)
	code := run(&Config{DryRun: true, InputPath: input, StatsFilePath: stats, PRIndex: 1})
	if code != 0 {
		t.Fatalf("run = %d, want 0 for a completed review run", code)
	}
	data := readRunStats(t, stats)
	if !strings.Contains(data, "OCR_RUN_STATUS=completed") {
		t.Fatalf("stats must mark the completed run, got %q", data)
	}
}
