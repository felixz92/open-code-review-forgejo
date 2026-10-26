package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
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
	// The exit reason must be logged to stderr for the workflow log.
	oldErr := os.Stderr
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wr
	code := run(&Config{DryRun: true, InputPath: input, StatsFilePath: stats, PRIndex: 1})
	os.Stderr = oldErr
	if err := wr.Close(); err != nil {
		t.Fatal(err)
	}
	var stderrOut bytes.Buffer
	if _, err := io.Copy(&stderrOut, rd); err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("run = %d, want 1 for a failed review run", code)
	}
	if !strings.Contains(stderrOut.String(), `error: review run reported status "failed"`) {
		t.Fatalf("failure reason must be logged to stderr, got %q", stderrOut.String())
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

// TestRunPostingFailureStillWritesStats pins the carried spec scenario
// "Stats file always written": posting fails partway through and the run
// still writes the stats file with the counts achieved so far.
func TestRunPostingFailureStillWritesStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message": "denied"}`))
	}))
	defer srv.Close()
	input, stats := writeRunFixture(t, `{"comments": [], "warnings": [], "message": "no issues"}`)
	code := run(&Config{URL: srv.URL, Owner: "o", Repo: "r", PRIndex: 1, Token: "t", InputPath: input, StatsFilePath: stats})
	if code != 0 {
		t.Fatalf("posting failures are best-effort: run = %d, want 0", code)
	}
	data := readRunStats(t, stats)
	if !strings.Contains(data, "OCR_COMMENTS_TOTAL=0") || !strings.Contains(data, "OCR_RUN_STATUS=completed") {
		t.Fatalf("stats file must carry the counts achieved so far, got %q", data)
	}
}

// TestRunMalformedJSONExitsOneWithLogTail pins the carried spec scenario
// "Malformed JSON with stderr log" at the CLI level: unparseable input
// exits 1, writes the stats file, and relays the OCR stderr-log tail.
func TestRunMalformedJSONExitsOneWithLogTail(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "result.json")
	logPath := filepath.Join(dir, "ocr-stderr.log")
	statsPath := filepath.Join(dir, "ocr-stats.env")
	if err := os.WriteFile(input, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("tool search_issues err: boom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldErr := os.Stderr
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wr
	code := run(&Config{DryRun: true, InputPath: input, StderrLogPath: logPath, StatsFilePath: statsPath, PRIndex: 1})
	os.Stderr = oldErr
	if err := wr.Close(); err != nil {
		t.Fatal(err)
	}
	var stderrOut bytes.Buffer
	if _, err := io.Copy(&stderrOut, rd); err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("run = %d, want 1 for malformed JSON", code)
	}
	if !strings.Contains(stderrOut.String(), "tool search_issues err: boom") {
		t.Fatalf("error output must include the stderr-log tail, got %q", stderrOut.String())
	}
	readRunStats(t, statsPath) // stats file still written on this exit path
}

func TestRunPrintLastReviewedWritesStatsFile(t *testing.T) {
	input, stats := writeRunFixture(t, `{"comments": [], "warnings": []}`)
	code := run(&Config{DryRun: true, PrintLastReviewed: true, InputPath: input, StatsFilePath: stats, PRIndex: 1})
	if code != 0 {
		t.Fatalf("query mode must exit 0, got %d", code)
	}
	// Every exit path writes the stats file; the query mode posts
	// nothing, so the stats stay zeroed.
	data := readRunStats(t, stats)
	if !strings.Contains(data, "OCR_RUN_STATUS=completed") {
		t.Fatalf("query mode must still write the stats file, got %q", data)
	}
}
