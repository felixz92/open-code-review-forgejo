package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/felixz92/open-code-review-forgejo/internal/diffinv"
	"github.com/felixz92/open-code-review-forgejo/internal/format"
	"github.com/felixz92/open-code-review-forgejo/internal/policy"
	"github.com/felixz92/open-code-review-forgejo/internal/poster"
	"github.com/felixz92/open-code-review-forgejo/internal/publish"
	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

// Config holds the resolved CLI configuration.
type Config struct {
	URL                      string
	Owner                    string
	Repo                     string
	PRIndex                  int64
	Token                    string
	InputPath                string
	StderrLogPath            string
	StatsFilePath            string
	DryRun                   bool
	StickySummary            bool
	Incremental              bool
	IncrementalOverlapThresh float64
	RouteSeverityBelow       string
	RouteCategories          string
	ApproveBelow             string
	RunTag                   string
	Suggestions              bool
	ResolutionTracking       bool
	PrintLastReviewed        bool
	CoverageDiffPath         string
}

const (
	defaultStatsFile = ".ocr/ocr-stats.env"
	defaultStderrLog = ".ocr/ocr-stderr.log"
	defaultInputPath = ".ocr/ocr-result.json"
)

// ParseFlags parses command line flags and arguments into a Config.
func ParseFlags(args []string) (*Config, error) {
	fs := flag.NewFlagSet("ocr-forgejo", flag.ContinueOnError)
	cfg := &Config{}
	fs.StringVar(&cfg.URL, "url", "", "Forgejo base URL (required)")
	fs.StringVar(&cfg.Repo, "repo", "", "repository as owner/name (required)")
	fs.Int64Var(&cfg.PRIndex, "pr", 0, "pull request index (required)")
	fs.StringVar(&cfg.Token, "token", "", "API token (default: GIT_TOKEN env)")
	fs.StringVar(&cfg.InputPath, "input", defaultInputPath, "review result JSON path")
	fs.StringVar(&cfg.StderrLogPath, "stderr-log", defaultStderrLog, "OCR stderr log path, read on parse failure")
	fs.StringVar(&cfg.StatsFilePath, "stats-file", defaultStatsFile, "dotenv output path for posting stats")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "print review comments and summary instead of posting")
	fs.BoolVar(&cfg.StickySummary, "sticky-summary", true, "update the summary comment in place across runs")
	fs.BoolVar(&cfg.Incremental, "incremental", false, "skip comments overlapping prior bot review comments")
	fs.Float64Var(&cfg.IncrementalOverlapThresh, "incremental-overlap-threshold", 0.6, "IoU threshold for incremental overlap (0 < x <= 1)")
	fs.StringVar(&cfg.RouteSeverityBelow, "route-severity-below", "", "route findings at-or-below this severity to the summary")
	fs.StringVar(&cfg.RouteCategories, "route-categories", "", "comma-separated categories to route to the summary")
	fs.StringVar(&cfg.ApproveBelow, "approve-below", "low", "findings above this severity block approval; the named severity and below approve")
	fs.StringVar(&cfg.RunTag, "run-tag", "", "per-run identity for idempotency tags (default: <pr>-<head sha>)")
	fs.BoolVar(&cfg.Suggestions, "suggestions", true, "include Before/After suggestion blocks in comment bodies")
	fs.BoolVar(&cfg.ResolutionTracking, "resolution-tracking", false, "maintain an open/resolved findings ledger in the sticky summary across runs")
	fs.BoolVar(&cfg.PrintLastReviewed, "print-last-reviewed", false, "print the last-reviewed head SHA from the sticky summary and exit; failures print nothing and exit 0")
	fs.StringVar(&cfg.CoverageDiffPath, "coverage-diff", "", "unified diff of the exact range this run reviewed; open ledger entries resolve only when covered (requires --resolution-tracking)")

	if err := fs.Parse(reorderArgs(args, boolFlags(fs))); err != nil {
		return nil, err
	}
	if fs.NArg() > 1 {
		return nil, fmt.Errorf("unexpected extra arguments: %v", fs.Args()[1:])
	}
	if fs.NArg() == 1 {
		cfg.InputPath = fs.Arg(0)
	}
	if err := cfg.validate(fs); err != nil {
		return cfg, err
	}
	if cfg.Token == "" {
		cfg.Token = os.Getenv("GIT_TOKEN")
	}
	return cfg, nil
}

// boolFlags records the FlagSet's boolean flags: they never consume a
// separate value token in reorderArgs.
func boolFlags(fs *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		if bv, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bv.IsBoolFlag() {
			out[f.Name] = true
		}
	})
	return out
}

// reorderArgs lets flags appear after the positional input by partitioning
// args into flag arguments (with their values) and positional arguments.
// "--" sends everything after it to positionals; a lone "-" is a positional.
// Boolean flags never consume the next token as their value.
func reorderArgs(args []string, bools map[string]bool) (reordered []string) {
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case isFlag(arg):
			reordered = append(reordered, arg)
			name := strings.TrimLeft(arg, "-")
			if !bools[name] && !strings.Contains(arg, "=") && i+1 < len(args) && !isFlag(args[i+1]) {
				reordered = append(reordered, args[i+1])
				i++
			}
		default:
			positional = append(positional, arg)
		}
	}
	return append(reordered, positional...)
}

func isFlag(arg string) bool {
	return len(arg) > 1 && strings.HasPrefix(arg, "-")
}

func (c *Config) validate(fs *flag.FlagSet) error {
	var missing []string
	if c.URL == "" {
		missing = append(missing, "--url")
	}
	if c.Repo == "" {
		missing = append(missing, "--repo")
	}
	if c.PRIndex == 0 {
		missing = append(missing, "--pr")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flag(s): %s", strings.Join(missing, ", "))
	}
	if c.ResolutionTracking && !c.StickySummary {
		return fmt.Errorf("--resolution-tracking requires the sticky summary: cross-run ledger state lives in the sticky summary body")
	}
	if c.CoverageDiffPath != "" && !c.ResolutionTracking {
		return fmt.Errorf("--coverage-diff requires --resolution-tracking: coverage-aware approval safety depends on the ledger")
	}
	if (c.Incremental || c.ResolutionTracking) && (c.IncrementalOverlapThresh <= 0 || c.IncrementalOverlapThresh >= 1) {
		return fmt.Errorf("--incremental-overlap-threshold must be in (0, 1), got %v", c.IncrementalOverlapThresh)
	}
	parts := strings.SplitN(c.Repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("--repo must be owner/name, got %q", c.Repo)
	}
	c.Owner, c.Repo = parts[0], parts[1]
	return nil
}

func main() {
	cfg, err := ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		writeEarlyStats(cfg)
		os.Exit(2)
	}
	if tokenMissing(cfg) {
		fmt.Fprintln(os.Stderr, "error: GIT_TOKEN is not set")
		writeEarlyStats(cfg)
		os.Exit(2)
	}
	os.Exit(run(cfg))
}

// tokenMissing reports whether the run cannot authenticate: no token and not
// a dry-run.
func tokenMissing(cfg *Config) bool {
	return cfg.Token == "" && !cfg.DryRun
}

// writeEarlyStats best-effort writes a zero-filled stats file on early exit
// paths so downstream CI jobs always find the artifact.
func writeEarlyStats(cfg *Config) {
	path := earlyStatsPath(cfg)
	if err := publish.WriteStatsFile(path, &publish.Stats{}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not write stats file:", err)
	}
}

func earlyStatsPath(cfg *Config) string {
	if cfg != nil && cfg.StatsFilePath != "" {
		return cfg.StatsFilePath
	}
	return defaultStatsFile
}

func run(cfg *Config) int {
	var p poster.Poster
	if cfg.DryRun {
		p = poster.NewDryRun()
	} else {
		fp, err := poster.NewForgejo(context.Background(), cfg.URL, cfg.Owner, cfg.Repo, cfg.PRIndex, cfg.Token, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			_ = publish.WriteStatsFile(cfg.StatsFilePath, &publish.Stats{})
			return 1
		}
		p = fp
	}

	// Query mode: read-only, exits before any result loading or posting.
	if cfg.PrintLastReviewed {
		code := runPrintLastReviewed(p)
		// Every exit path writes the stats file (main spec); the query
		// mode posts nothing, so the stats stay zeroed.
		_ = publish.WriteStatsFile(cfg.StatsFilePath, &publish.Stats{})
		return code
	}

	result, err := review.Load(cfg.InputPath, review.WithStderrLog(cfg.StderrLogPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		_ = publish.WriteStatsFile(cfg.StatsFilePath, &publish.Stats{})
		return 1
	}

	headSHA := ""
	if cfg.RunTag == "" {
		// Run tag from PR index + reviewed head SHA so reruns of the same
		// revision share identity; unknown head falls back to the PR index.
		headSHA = result.Manifest.Input.ResolvedHead
		if headSHA == "" {
			headSHA = "unknown"
		}
	}
	runTag := cfg.RunTag
	if runTag == "" {
		runTag = fmt.Sprintf("%d-%s", cfg.PRIndex, shortSHA(headSHA))
	}

	commitID := result.Manifest.Input.ResolvedHead
	stats, err := publish.Publish(result, p, publish.Options{
		RunTag:             runTag,
		Sticky:             cfg.StickySummary,
		Incremental:        cfg.Incremental,
		OverlapThresh:      cfg.IncrementalOverlapThresh,
		Route:              policy.Build(cfg.RouteSeverityBelow, cfg.RouteCategories),
		CommitID:           commitID,
		Suggestions:        cfg.Suggestions,
		ApproveBelow:       cfg.ApproveBelow,
		ResolutionTracking: cfg.ResolutionTracking,
		Coverage:           loadCoverageDiff(cfg.CoverageDiffPath),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if stats == nil {
			stats = &publish.Stats{}
		}
		_ = publish.WriteStatsFile(cfg.StatsFilePath, stats)
		return 1
	}
	if err := publish.WriteStatsFile(cfg.StatsFilePath, stats); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not write stats file:", err)
	}
	if stats.RunStatus == publish.RunStatusFailed {
		// A failed OCR run vouches for nothing: surface the failure to
		// the workflow instead of exiting zero (mirrors the malformed
		// JSON path).
		fmt.Fprintln(os.Stderr, "error: review run reported status \"failed\"; exiting non-zero without approval (failure-note posting is best-effort)")
		return 1
	}
	log.Printf("posted %d/%d inline, %d summary, %d routed, %d skipped, %d failed, %d resolved (total %d): %s",
		stats.Inline, stats.Total, stats.Summary, stats.Routed, stats.Skipped, stats.Failed, stats.Resolved, stats.Total, stats.State)
	return 0
}

// lastReviewedMarkerRe extracts the SHA from a last-reviewed marker.
var lastReviewedMarkerRe = regexp.MustCompile(`<!-- ocr-last-reviewed:(\S+) -->`)

// findLastReviewedSHA locates the sticky summary comment (newest match
// first) and extracts the SHA from its last-reviewed marker. An absent
// summary or marker yields an empty string.
func findLastReviewedSHA(p poster.Poster) (string, error) {
	comments, err := p.ListIssueComments()
	if err != nil {
		return "", err
	}
	for i := len(comments) - 1; i >= 0; i-- {
		if !strings.Contains(comments[i].Body, format.SummaryMarker) {
			continue
		}
		matches := lastReviewedMarkerRe.FindAllStringSubmatch(comments[i].Body, -1)
		if len(matches) == 0 {
			return "", nil
		}
		return matches[len(matches)-1][1], nil
	}
	return "", nil
}

// runPrintLastReviewed implements the query mode: print the last-reviewed
// head SHA to stdout and exit 0. Any failure prints nothing and exits 0 so
// callers can fall back to a full review (advisory-step contract).
func runPrintLastReviewed(p poster.Poster) int {
	sha, err := findLastReviewedSHA(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not read the last-reviewed marker:", err)
		return 0
	}
	if sha != "" {
		fmt.Println(sha)
	}
	return 0
}

// loadCoverageDiff turns the --coverage-diff file into a publish.Coverage.
// An empty path keeps full-range resolution semantics (nil). An unreadable
// or unparseable file means "no coverage": nothing resolves this run,
// with a logged warning — never a silent full-coverage fallback.
func loadCoverageDiff(path string) *publish.Coverage {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not read coverage diff:", err)
		return publish.NoCoverage()
	}
	inv, err := diffinv.New(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: coverage diff is not a parseable unified diff:", err)
		return publish.NoCoverage()
	}
	return publish.NewCoverage(inv)
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
