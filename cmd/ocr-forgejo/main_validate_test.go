package main

import (
	"strings"
	"testing"
)

func TestParseFlagsValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"missing url", []string{"--repo", "o/r", "--pr", "1"}, "--url"},
		{"missing repo", []string{"--url", "https://x.io", "--pr", "1"}, "--repo"},
		{"missing pr", []string{"--url", "https://x.io", "--repo", "o/r"}, "--pr"},
		{"multiple missing", []string{}, "--url"},
		{"bad repo shape", []string{"--url", "https://x.io", "--repo", "noslash", "--pr", "1"}, "--repo must be owner/name"},
		{"bad repo shape empty owner", []string{"--url", "https://x.io", "--repo", "/r", "--pr", "1"}, "--repo must be owner/name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFlags(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestTokenMissing(t *testing.T) {
	// tokenMissing sees the config AFTER ParseFlags resolved GIT_TOKEN into
	// cfg.Token, so the test sets the field directly.
	if !tokenMissing(&Config{}) {
		t.Fatal("missing token outside dry-run must be reported")
	}
	if !tokenMissing(&Config{Token: ""}) {
		t.Fatal("empty token outside dry-run must be reported")
	}
	if tokenMissing(&Config{DryRun: true}) {
		t.Fatal("dry-run does not need a token")
	}
	if tokenMissing(&Config{Token: "secret"}) {
		t.Fatal("set token must not be reported")
	}
}

func TestEarlyStatsPath(t *testing.T) {
	if got := earlyStatsPath(nil); got != defaultStatsFile {
		t.Fatalf("nil config should fall back to default, got %q", got)
	}
	if got := earlyStatsPath(&Config{}); got != defaultStatsFile {
		t.Fatalf("empty config should fall back to default, got %q", got)
	}
	if got := earlyStatsPath(&Config{StatsFilePath: "/tmp/s.env"}); got != "/tmp/s.env" {
		t.Fatalf("configured path should win, got %q", got)
	}
}
