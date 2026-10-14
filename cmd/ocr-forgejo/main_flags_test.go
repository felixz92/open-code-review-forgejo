package main

import (
	"flag"
	"testing"
)

func TestParseFlagsAfterPositional(t *testing.T) {
	cfg, err := ParseFlags([]string{
		"/tmp/result.json",
		"--url", "https://forge.example.com",
		"--repo", "owner/repo",
		"--pr", "42",
		"--stats-file", "/tmp/ocr-stats.env",
		"--dry-run",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InputPath != "/tmp/result.json" {
		t.Fatalf("input = %q", cfg.InputPath)
	}
	if cfg.URL != "https://forge.example.com" || cfg.Owner != "owner" || cfg.Repo != "repo" ||
		cfg.PRIndex != 42 || cfg.StatsFilePath != "/tmp/ocr-stats.env" || !cfg.DryRun {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseFlagsMixedOrder(t *testing.T) {
	cfg, err := ParseFlags([]string{
		"--url", "https://forge.example.com",
		"/tmp/result.json",
		"--repo=owner/repo",
		"--pr", "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InputPath != "/tmp/result.json" || cfg.URL != "https://forge.example.com" ||
		cfg.Owner != "owner" || cfg.Repo != "repo" || cfg.PRIndex != 7 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseFlagsNegativeValuesNotFlags(t *testing.T) {
	// A lone "-" is a positional, not a flag.
	cfg, err := ParseFlags([]string{
		"--url", "https://forge.example.com", "--repo", "o/r", "--pr", "1", "-",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InputPath != "-" {
		t.Fatalf("input = %q", cfg.InputPath)
	}
}

func TestParseFlagsBoolFlagDoesNotConsumeNextToken(t *testing.T) {
	// A boolean flag followed by the positional input and more flags must
	// parse fully: --dry-run must not swallow "input.json".
	cfg, err := ParseFlags([]string{
		"--dry-run", "/tmp/result.json", "--pr", "42",
		"--url", "https://forge.example.com", "--repo", "o/r",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DryRun || cfg.InputPath != "/tmp/result.json" || cfg.PRIndex != 42 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseFlagsSuggestionsDefaultTrue(t *testing.T) {
	cfg, err := ParseFlags([]string{
		"--url", "https://forge.example.com", "--repo", "o/r", "--pr", "1",
		"/tmp/result.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Suggestions {
		t.Fatalf("--suggestions must default to true, cfg = %+v", cfg)
	}
}

func TestParseFlagsSuggestionsFalse(t *testing.T) {
	cfg, err := ParseFlags([]string{
		"--url", "https://forge.example.com", "--repo", "o/r", "--pr", "1",
		"--suggestions=false", "/tmp/result.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Suggestions {
		t.Fatalf("--suggestions=false must disable suggestions, cfg = %+v", cfg)
	}
}

func TestParseFlagsApproveBelowDefaultLow(t *testing.T) {
	cfg, err := ParseFlags([]string{
		"--url", "https://forge.example.com", "--repo", "o/r", "--pr", "1",
		"/tmp/result.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApproveBelow != "low" {
		t.Fatalf("--approve-below must default to low, got %q", cfg.ApproveBelow)
	}
}

func TestParseFlagsApproveBelowCustom(t *testing.T) {
	cfg, err := ParseFlags([]string{
		"--url", "https://forge.example.com", "--repo", "o/r", "--pr", "1",
		"--approve-below", "medium", "/tmp/result.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApproveBelow != "medium" {
		t.Fatalf("--approve-below = %q, want medium", cfg.ApproveBelow)
	}
}

func TestParseFlagsRejectsRemovedFailOnSeverity(t *testing.T) {
	// --fail-on-severity was removed; it must be rejected as unknown.
	if _, err := ParseFlags([]string{
		"--url", "https://forge.example.com", "--repo", "o/r", "--pr", "1",
		"--fail-on-severity", "high", "/tmp/result.json",
	}); err == nil {
		t.Fatal("--fail-on-severity must no longer parse")
	}
}

func TestValidateOverlapThresholdRange(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	for _, tc := range []struct {
		v      float64
		increm bool
		wantOK bool
	}{
		{0.6, true, true},
		{0, true, false},
		{1, true, false},
		{1.5, true, false},
		{0, false, true},
	} {
		c := &Config{URL: "https://x", Repo: "o/r", PRIndex: 1, Incremental: tc.increm, IncrementalOverlapThresh: tc.v}
		err := c.validate(fs)
		if tc.wantOK != (err == nil) {
			t.Fatalf("validate(%v, incremental=%v) err = %v, wantOK = %v", tc.v, tc.increm, err, tc.wantOK)
		}
	}
}
