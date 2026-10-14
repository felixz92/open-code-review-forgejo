package policy

import (
	"testing"

	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

func TestBuildPolicyFailOpen(t *testing.T) {
	for _, tc := range []struct{ severity, categories string }{
		{"", ""},
		{"banana", ""},
		{"", "nope,also-nope"},
		{"banana", "nope"},
		{"", " , "},
	} {
		p := Build(tc.severity, tc.categories)
		if p.Active() {
			t.Fatalf("policy(%q, %q) should be inactive, got %+v", tc.severity, tc.categories, p)
		}
	}
}

func TestBuildPolicyActive(t *testing.T) {
	if p := Build("high", ""); !p.Active() {
		t.Fatal("severity policy should be active")
	}
	if p := Build("", "bug,style"); !p.Active() {
		t.Fatal("category policy should be active")
	}
	if p := Build(" HIGH ", " Bug "); !p.Active() {
		t.Fatal("values should be normalized case/whitespace")
	}
}

func TestRouteBySeverity(t *testing.T) {
	p := Build("high", "")
	tests := []struct {
		severity string
		routed   bool
	}{
		{"low", true},
		{"medium", true},
		{"high", true}, // at-or-below routes
		{"critical", false},
		{"", false}, // unknown never matches
		{"weird", false},
	}
	for _, tt := range tests {
		c := &review.Comment{Severity: tt.severity}
		res := Route(c, p)
		if res.Routed != tt.routed {
			t.Fatalf("severity %q: routed = %v, want %v", tt.severity, res.Routed, tt.routed)
		}
	}
}

func TestRouteByCategory(t *testing.T) {
	p := Build("", "bug,style")
	tests := []struct {
		category string
		routed   bool
	}{
		{"bug", true},
		{"style", true},
		{"performance", false},
		{"", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		res := Route(&review.Comment{Category: tt.category}, p)
		if res.Routed != tt.routed {
			t.Fatalf("category %q: routed = %v, want %v", tt.category, res.Routed, tt.routed)
		}
	}
}

func TestRouteReasons(t *testing.T) {
	res := Route(&review.Comment{Severity: "low"}, Build("high", ""))
	if !res.Routed || res.Reason == "" {
		t.Fatalf("routed result should carry a reason, got %+v", res)
	}
	res = Route(&review.Comment{Category: "style", Severity: "low"}, Build("", "style"))
	if !res.Routed || res.Reason == "" {
		t.Fatalf("routed result should carry a reason, got %+v", res)
	}
	if res := Route(&review.Comment{}, Build("", "")); res.Routed {
		t.Fatal("inactive policy routes nothing")
	}
}

func TestRouteCaseInsensitive(t *testing.T) {
	p := Build("high", "bug")
	if res := Route(&review.Comment{Severity: " Low ", Category: " BUG "}, p); !res.Routed {
		t.Fatal("metadata should be matched case/whitespace-insensitively")
	}
}

func TestSeverityAbove(t *testing.T) {
	tests := []struct {
		name      string
		threshold string
		severity  string
		want      bool
	}{
		{"default low: low does not block", "low", "low", false},
		{"default low: medium blocks", "low", "medium", true},
		{"default low: critical blocks", "low", "critical", true},
		{"high threshold: medium approves", "high", "medium", false},
		{"high threshold: high does not block", "high", "high", false},
		{"critical threshold: critical does not block", "critical", "critical", false},
		{"malformed threshold fails safe on low", "banana", "low", true},
		{"malformed threshold fails safe on critical", "banana", "critical", true},
		{"empty threshold fails safe", "", "low", true},
		{"case and whitespace normalized", " LOW ", "medium", true},
		{"unknown comment severity never blocks", "low", "weird", false},
		{"empty comment severity never blocks", "low", "", false},
	}
	for _, tc := range tests {
		c := &review.Comment{Severity: tc.severity}
		if got := SeverityAbove(c, tc.threshold); got != tc.want {
			t.Errorf("SeverityAbove(severity %q, threshold %q) = %v, want %v (%s)",
				tc.severity, tc.threshold, got, tc.want, tc.name)
		}
	}
}

func TestSeverityKnown(t *testing.T) {
	// Exported so the approval verdict can fail safe: an unranked severity
	// (e.g. a missing field in the external result JSON) must block.
	tests := []struct {
		severity string
		want     bool
	}{
		{"low", true},
		{"medium", true},
		{"high", true},
		{"critical", true},
		{" HIGH ", true},
		{"", false},
		{"weird", false},
		{"banana", false},
	}
	for _, tt := range tests {
		if got := SeverityKnown(tt.severity); got != tt.want {
			t.Errorf("SeverityKnown(%q) = %v, want %v", tt.severity, got, tt.want)
		}
	}
}
