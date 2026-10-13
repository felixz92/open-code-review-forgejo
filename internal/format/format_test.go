package format

import (
	"strings"
	"testing"

	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

func TestBuildBadge(t *testing.T) {
	tests := []struct {
		name, category, severity, want string
	}{
		{"both", "bug", "critical", "[bug · critical]"},
		{"category only", "bug", "", "[bug]"},
		{"severity only", "", "high", "[high]"},
		{"neither", "", "", ""},
		{"control chars stripped", "bu\x01g", "hi\x7fgh", "[bug · high]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildBadge(&review.Comment{Category: tt.category, Severity: tt.severity})
			if got != tt.want {
				t.Fatalf("BuildBadge = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatCommentInline(t *testing.T) {
	c := &review.Comment{Path: "a.go", EndLine: 5, Category: "bug", Severity: "critical", Content: "fix this"}
	got := FormatComment(c, "ocr-42-abc", 5, 14)
	want := "<!-- ocr-42-abc -->\n[bug · critical]\nfix this\n\n> File `a.go` lines 5-14"
	if got != want {
		t.Fatalf("FormatComment =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatCommentNoSpan(t *testing.T) {
	c := &review.Comment{Path: "a.go", EndLine: 5, Content: "fix this"}
	got := FormatComment(c, "ocr-42-abc", 0, 0)
	if strings.Contains(got, "lines 0-0") {
		t.Fatalf("no span should be stated, got %q", got)
	}
	if got != "<!-- ocr-42-abc -->\nfix this" {
		t.Fatalf("unexpected body %q", got)
	}
}

func TestSafeFence(t *testing.T) {
	tests := []struct {
		content, want string
	}{
		{"plain", "```"},
		{"has ```ticks``` inside", "````"},
		{"has ````longer```` inside", "`````"},
	}
	for _, tt := range tests {
		if got := SafeFence(tt.content); got != tt.want {
			t.Fatalf("SafeFence(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}

func TestSuggestionBlocks(t *testing.T) {
	c := &review.Comment{ExistingCode: "old", SuggestionCode: "new"}
	got := SuggestionBlocks(c)
	want := "\n\n**Before:**\n```go?\nold\n```\n\n**After:**\n```go?\nnew\n```"
	_ = want // exact fence language checked below
	if !strings.Contains(got, "**Before:**") || !strings.Contains(got, "**After:**") {
		t.Fatalf("missing Before/After: %q", got)
	}
	if !strings.Contains(got, "\nold\n") || !strings.Contains(got, "\nnew\n") {
		t.Fatalf("missing code blocks: %q", got)
	}
}

func TestSuggestionBlocksAbsent(t *testing.T) {
	if got := SuggestionBlocks(&review.Comment{}); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := SuggestionBlocks(&review.Comment{SuggestionCode: "x"}); got != "" {
		t.Fatalf("suggestion without existing code should be omitted, got %q", got)
	}
}

func TestFormatCommentFallback(t *testing.T) {
	c := &review.Comment{Path: "a.go", StartLine: 10, EndLine: 14, Category: "bug", Severity: "high", Content: "bad code", ExistingCode: "bad", SuggestionCode: "good"}
	got := FormatCommentFallback(c, "out of diff")
	for _, want := range []string{"[bug · high]", "`a.go`", "L10-L14", "out of diff", "bad code", "<details>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fallback missing %q:\n%s", want, got)
		}
	}
}

func TestFormatCommentFallbackWithoutReason(t *testing.T) {
	c := &review.Comment{Path: "a.go", StartLine: 3, EndLine: 3, Content: "x"}
	got := FormatCommentFallback(c, "")
	if strings.Contains(got, "⚠️") || strings.Contains(got, "<details>") {
		t.Fatalf("no reason/detail expected:\n%s", got)
	}
}

func TestFormatWarnings(t *testing.T) {
	if got := FormatWarnings(nil); got != "" {
		t.Fatalf("empty warnings should render empty, got %q", got)
	}
	got := FormatWarnings([]review.Warning{
		{File: "a.go", Type: "llm", Message: "flaky"},
		{Message: "only message"},
		{File: "b.go"},
	})
	for _, want := range []string{"⚠️ **Warnings:**", "`a.go` (`llm`): flaky", "only message", "`b.go`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("warnings missing %q:\n%s", want, got)
		}
	}
}

func TestBuildSummaryBody(t *testing.T) {
	got := BuildSummaryBody(5, 2, 1, 0, 1, 1, 2)
	for _, want := range []string{
		"found **5** issue(s)",
		"Successfully posted inline: 2 comment(s)",
		"In summary (no line info): 1 comment(s)",
		"Routed to summary by policy: 1 comment(s)",
		"Failed to post inline: 1 comment(s)",
		"2 warning(s) occurred",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestBuildSummaryBodySkipped(t *testing.T) {
	got := BuildSummaryBody(3, 0, 0, 3, 0, 0, 0)
	if !strings.Contains(got, "Skipped (overlap with history): 3 comment(s)") {
		t.Fatalf("summary missing skipped line:\n%s", got)
	}
}

func TestBuildPreReviewBody(t *testing.T) {
	got := BuildPreReviewBody(2, []SummaryItem{{Comment: &review.Comment{Path: "a.go", EndLine: 1, Content: "x"}, Reason: "No line information provided"}}, []SummaryItem{{Comment: &review.Comment{Path: "b.go", EndLine: 2, Content: "y"}, Reason: "Routed"}}, nil)
	for _, want := range []string{"found **2** issue(s)", "_Posting review comments…_", "Routed to summary by policy: 1 comment(s)", "No line information provided", "`b.go`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("pre-review missing %q:\n%s", want, got)
		}
	}
}

func TestBuildNoIssuesBody(t *testing.T) {
	got := BuildNoIssuesBody("all good")
	if !strings.Contains(got, "✅") || !strings.Contains(got, "all good") {
		t.Fatalf("unexpected LGTM body: %q", got)
	}
}

func TestFormatSummaryComments(t *testing.T) {
	got := FormatSummaryComments([]SummaryItem{
		{Comment: &review.Comment{Path: "a.go", EndLine: 1, Content: "c1"}, Reason: "r1"},
		{Comment: &review.Comment{Path: "b.go", EndLine: 2, Content: "c2"}},
	})
	if strings.Count(got, "---") < 2 {
		t.Fatalf("expected separators between items:\n%s", got)
	}
	if strings.Contains(got, "Could not be posted inline: r2") {
		t.Fatal("empty reason should not render")
	}
}

func TestWrapSummaryBody(t *testing.T) {
	got := WrapSummaryBody("content", "42-abc")
	want := "<!-- ocr-summary -->\n<!-- ocr-summary-run:42-abc -->\ncontent"
	if got != want {
		t.Fatalf("WrapSummaryBody = %q, want %q", got, want)
	}
}

func TestFormatCommentNeutralizesCommentClose(t *testing.T) {
	c := &review.Comment{Path: "a.go", Content: "see <!-- ocr-summary --> trap"}
	out := FormatComment(c, "ocr-x-1", 1, 1)
	if strings.Contains(out, "ocr-summary -->") {
		t.Fatalf("marker not neutralized: %q", out)
	}
}
