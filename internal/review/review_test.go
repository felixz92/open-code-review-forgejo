package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMinimalResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	err := os.WriteFile(path, []byte(`{"comments": [], "warnings": [], "message": "no issues"}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(r.Comments) != 0 || len(r.Warnings) != 0 || r.Message != "no issues" {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestLoadFullResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	data := `{
	  "comments": [{"path": "a.go", "start_line": 10, "end_line": 14, "content": "fix this",
	    "suggestion_code": "fixed", "existing_code": "broken", "category": "bug", "severity": "critical"}],
	  "warnings": [{"file": "a.go", "type": "llm", "message": "flaky"}],
	  "message": "done",
	  "manifest": {"input": {"resolved_head": "abc123", "resolved_base": "def456"}}
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := r.Comments[0]
	if c.Path != "a.go" || c.StartLine != 10 || c.EndLine != 14 || c.Content != "fix this" ||
		c.SuggestionCode != "fixed" || c.ExistingCode != "broken" ||
		c.Category != "bug" || c.Severity != "critical" {
		t.Fatalf("unexpected comment: %+v", c)
	}
	if len(r.Warnings) != 1 || r.Warnings[0].File != "a.go" || r.Warnings[0].Type != "llm" || r.Warnings[0].Message != "flaky" {
		t.Fatalf("unexpected warnings: %+v", r.Warnings)
	}
	if r.Manifest.Input.ResolvedHead != "abc123" || r.Manifest.Input.ResolvedBase != "def456" {
		t.Fatalf("unexpected manifest: %+v", r.Manifest)
	}
	if r.Message != "done" {
		t.Fatalf("unexpected message %q", r.Message)
	}
}

func TestLoadToleratesMissingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load on empty object should not fail: %v", err)
	}
	if r.Comments != nil {
		t.Fatalf("comments should be zero value, got %+v", r.Comments)
	}
}

func TestLoadMalformedJSONIncludesStderrLog(t *testing.T) {
	dir := t.TempDir()
	resultPath := filepath.Join(dir, "result.json")
	stderrPath := filepath.Join(dir, "ocr-stderr.log")
	if err := os.WriteFile(resultPath, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderrPath, []byte("traceback line 1\ntraceback line 2"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(resultPath, WithStderrLog(stderrPath))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "traceback line 2") {
		t.Fatalf("error should include stderr log tail, got: %v", err)
	}
}

func TestLoadMalformedJSONWithoutStderrLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse review result") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
