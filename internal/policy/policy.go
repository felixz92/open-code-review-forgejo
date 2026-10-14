// Package policy implements publication routing of OCR findings to the summary.
package policy

import (
	"strings"

	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

// Categories is the known set of OCR categories.
var Categories = []string{
	"bug", "security", "performance", "maintainability",
	"test", "style", "documentation", "other",
}

// Severities ranks from least to most severe.
var Severities = []string{"low", "medium", "high", "critical"}

var severityRank = func() map[string]int {
	m := make(map[string]int, len(Severities))
	for i, s := range Severities {
		m[s] = i + 1
	}
	return m
}()

var categorySet = func() map[string]bool {
	m := make(map[string]bool, len(Categories))
	for _, c := range Categories {
		m[c] = true
	}
	return m
}()

// Policy is the normalized publication routing policy.
type Policy struct {
	routeBySeverity bool
	severityRank    int
	routeByCategory bool
	categories      map[string]bool
}

// Active reports whether the policy routes anything.
func (p Policy) Active() bool {
	return p.routeBySeverity || p.routeByCategory
}

// Build parses the policy from raw opt-in values. Any malformed or missing
// value fails open (inactive policy).
func Build(severityBelow, categories string) Policy {
	p := Policy{}
	if rank, ok := severityRank[strings.ToLower(strings.TrimSpace(severityBelow))]; ok {
		p.routeBySeverity = true
		p.severityRank = rank
	}
	var cats map[string]bool
	for _, t := range strings.Split(categories, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if categorySet[t] {
			if cats == nil {
				cats = map[string]bool{}
			}
			cats[t] = true
		}
	}
	if len(cats) > 0 {
		p.routeByCategory = true
		p.categories = cats
	}
	return p
}

// RouteResult says whether a comment routes to the summary and why.
type RouteResult struct {
	Routed bool
	Reason string
}

// Route decides whether a comment routes to the summary. Unknown or malformed
// metadata never matches.
func Route(c *review.Comment, p Policy) RouteResult {
	if !p.Active() {
		return RouteResult{}
	}
	cat := strings.ToLower(strings.TrimSpace(c.Category))
	sev := strings.ToLower(strings.TrimSpace(c.Severity))
	catKnown := cat != "" && categorySet[cat]
	sevKnown := sev != "" && severityRank[sev] > 0

	if p.routeBySeverity && sevKnown && severityRank[sev] <= p.severityRank {
		reason := "Routed to summary (severity " + sev
		if catKnown {
			reason += " · category " + cat
		}
		return RouteResult{Routed: true, Reason: reason + ")"}
	}
	if p.routeByCategory && catKnown && p.categories[cat] {
		reason := "Routed to summary (category " + cat
		if sevKnown {
			reason += " · severity " + sev
		}
		return RouteResult{Routed: true, Reason: reason + ")"}
	}
	return RouteResult{}
}

// SeverityAtOrAbove reports whether a comment's severity is at-or-above the
// named threshold. Unknown severities never trip.
func SeverityAtOrAbove(c *review.Comment, threshold string) bool {
	threshold = strings.ToLower(strings.TrimSpace(threshold))
	t, ok := severityRank[threshold]
	if !ok {
		return false
	}
	sev := strings.ToLower(strings.TrimSpace(c.Severity))
	r, ok := severityRank[sev]
	return ok && r >= t
}

// SeverityAbove reports whether c's severity ranks strictly above the given
// approval threshold (--approve-below value): the threshold severity and
// everything below it does not count. An unknown threshold fails safe by
// ranking below `low`, so every known-severity comment counts as a blocking
// finding (a typoed flag must not approve a broken PR). A comment with an
// unknown severity never counts.
func SeverityAbove(c *review.Comment, threshold string) bool {
	threshold = strings.ToLower(strings.TrimSpace(threshold))
	t := severityRank[threshold] // 0 (below low) when unknown: fail-safe
	sev := strings.ToLower(strings.TrimSpace(c.Severity))
	r, ok := severityRank[sev]
	return ok && r > t
}
