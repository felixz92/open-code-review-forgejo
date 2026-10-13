package main

import "testing"

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
