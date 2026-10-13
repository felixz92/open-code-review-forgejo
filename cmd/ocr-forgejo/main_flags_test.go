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
