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
