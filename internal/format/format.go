// Package format renders OCR comments, warnings, and summary bodies as Markdown.
package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

// SummaryMarker tags the cross-run sticky summary comment.
const SummaryMarker = "<!-- ocr-summary -->"

// PerRunMarkerFmt is the per-run tag embedded alongside SummaryMarker so
// non-sticky mode can find this run's comment.
const PerRunMarkerFmt = "<!-- ocr-summary-run:%s -->"

// BotMarker flags a review comment or note as authored by this bot.
const BotMarker = "<!-- ocr-"

// NoLineReason is attached to comments that have no valid line range.
const NoLineReason = "No line information provided"

// SummaryItem is a comment rendered into the summary with an optional reason.
type SummaryItem struct {
	Comment *review.Comment
	Reason  string
}

var controlChars = regexp.MustCompile(`[\x00-\x1f\x7f-\x9f]`)

// BuildBadge renders the `[category · severity]` badge, omitting absent parts.
func BuildBadge(c *review.Comment) string {
	if c == nil {
		return ""
	}
	category := controlChars.ReplaceAllString(c.Category, "")
	severity := controlChars.ReplaceAllString(c.Severity, "")
	switch {
	case category != "" && severity != "":
		return fmt.Sprintf("[%s · %s]", category, severity)
	case category != "":
		return "[" + category + "]"
	case severity != "":
		return "[" + severity + "]"
	default:
		return ""
	}
}

// Span is a resolved line span; Multiline is true when start != end.
type Span struct {
	Start, End int64
}

// SafeFence picks a backtick fence long enough to enclose content (min 3).
func SafeFence(content string) string {
	maxTicks := 0
	for _, run := range regexp.MustCompile("`+").FindAllString(content, -1) {
		if n := len(run); n > maxTicks {
			maxTicks = n
		}
	}
	return strings.Repeat("`", max(3, maxTicks+1))
}

func fencedBlock(content string) string {
	fence := SafeFence(content)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return fence + "\n" + content + fence
}

// SuggestionBlocks renders Before/After fenced blocks when a comment has both
// existing and suggested code, an After-only block when it has only suggested
// code. Returns "" otherwise.
func SuggestionBlocks(c *review.Comment) string {
	if c == nil || c.SuggestionCode == "" {
		return ""
	}
	if c.ExistingCode == "" {
		return "\n\n**After:**\n" + fencedBlock(c.SuggestionCode)
	}
	return "\n\n**Before:**\n" + fencedBlock(c.ExistingCode) +
		"\n\n**After:**\n" + fencedBlock(c.SuggestionCode)
}

// FormatComment assembles the visible inline comment body: the per-comment id
// tag (for idempotent matching and incremental detection), the badge, the
// content, and the stated line span when the comment covers multiple lines.
// When withSuggestions is true, suggestion blocks are appended when the
// comment carries suggestion code. Inline callers must keep this false: the
// diff-validity gating for inline suggestions lives in publish, which appends
// SuggestionBlocks itself after the pre-check; passing true here bypasses it.
func FormatComment(c *review.Comment, commentID string, startLine, endLine int64, withSuggestions bool) string {
	var b strings.Builder
	if commentID != "" {
		b.WriteString("<!-- " + commentID + " -->\n")
	}
	if badge := BuildBadge(c); badge != "" {
		b.WriteString(badge + "\n")
	}
	// Neutralize HTML-comment terminators so review content cannot close or
	// forge marker comments used for sticky-summary and incremental matching.
	b.WriteString(strings.ReplaceAll(c.Content, "-->", "-- >"))
	if startLine > 0 && endLine > 0 && startLine != endLine {
		fmt.Fprintf(&b, "\n\n> File `%s` lines %d-%d", c.Path, startLine, endLine)
	}
	if withSuggestions {
		b.WriteString(SuggestionBlocks(c))
	}
	return b.String()
}

// FormatCommentFallback renders a comment for display inside the summary.
// When withSuggestions is true, collapsible suggestion blocks are appended
// when the comment carries suggestion code.
func FormatCommentFallback(c *review.Comment, reason string, withSuggestions bool) string {
	var b strings.Builder
	if badge := BuildBadge(c); badge != "" {
		b.WriteString(badge + "\n")
	}
	fmt.Fprintf(&b, "### 📄 `%s`", c.Path)
	if c.StartLine > 0 && c.EndLine > 0 {
		if c.StartLine == c.EndLine {
			fmt.Fprintf(&b, " (L%d)", c.EndLine)
		} else {
			fmt.Fprintf(&b, " (L%d-L%d)", c.StartLine, c.EndLine)
		}
	}
	b.WriteString("\n\n")
	if reason != "" {
		b.WriteString("⚠️ Could not be posted inline: " + reason + "\n\n")
	}
	b.WriteString(c.Content)
	if withSuggestions {
		if blocks := SuggestionBlocks(c); blocks != "" {
			b.WriteString("\n\n<details><summary>💡 Suggested Change</summary>\n" + blocks + "\n\n</details>")
		}
	}
	return b.String()
}

// FormatSummaryComments renders a list of summary items, separated by rules.
func FormatSummaryComments(items []SummaryItem, withSuggestions bool) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString("\n\n---\n\n")
		b.WriteString(FormatCommentFallback(it.Comment, it.Reason, withSuggestions))
	}
	return b.String()
}

func formatWarning(w review.Warning) string {
	var prefix []string
	if w.File != "" {
		prefix = append(prefix, "`"+w.File+"`")
	}
	if w.Type != "" {
		prefix = append(prefix, "(`"+w.Type+"`)")
	}
	p := strings.Join(prefix, " ")
	switch {
	case p != "" && w.Message != "":
		return p + ": " + w.Message
	case w.Message != "":
		return w.Message
	default:
		return p
	}
}

// FormatWarnings renders warnings as a bulleted list under a heading ("" when empty).
func FormatWarnings(warnings []review.Warning) string {
	if len(warnings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n---\n\n⚠️ **Warnings:**")
	for _, w := range warnings {
		b.WriteString("\n- " + formatWarning(w))
	}
	return b.String()
}

// BuildSummaryBody renders the final summary header from mutually exclusive counts.
func BuildSummaryBody(total, inline, summary, skipped, routed, failed, warningCount int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔍 **OpenCodeReview** found **%d** issue(s) in this PR.", total)
	if total > 0 {
		fmt.Fprintf(&b, "\n- ✅ Successfully posted inline: %d comment(s)", inline)
		if summary > 0 {
			fmt.Fprintf(&b, "\n- 📝 In summary (no line info): %d comment(s)", summary)
		}
		if routed > 0 {
			fmt.Fprintf(&b, "\n- 📋 Routed to summary by policy: %d comment(s)", routed)
		}
		if skipped > 0 {
			fmt.Fprintf(&b, "\n- ⏭️ Skipped (overlap with history): %d comment(s)", skipped)
		}
		if failed > 0 {
			fmt.Fprintf(&b, "\n- ❌ Failed to post inline: %d comment(s)", failed)
		}
	}
	if warningCount > 0 {
		fmt.Fprintf(&b, "\n\n⚠️ %d warning(s) occurred during review.", warningCount)
	}
	return b.String()
}

// BuildPreReviewBody renders the anchor summary body shown while inline
// comments are being posted.
func BuildPreReviewBody(total int, noLine, routed []SummaryItem, warnings []review.Warning, withSuggestions bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔍 **OpenCodeReview** found **%d** issue(s) in this PR.", total)
	if total > 0 {
		b.WriteString("\n- ⏳ _Posting review comments…_")
		if len(routed) > 0 {
			fmt.Fprintf(&b, "\n- 📋 Routed to summary by policy: %d comment(s)", len(routed))
		}
	}
	b.WriteString(FormatSummaryComments(noLine, withSuggestions))
	b.WriteString(FormatSummaryComments(routed, withSuggestions))
	b.WriteString(FormatWarnings(warnings))
	return b.String()
}

// BuildNoIssuesBody renders the summary for a run without comments.
func BuildNoIssuesBody(message string) string {
	return "✅ **OpenCodeReview**: " + message
}

// WrapSummaryBody prefixes the summary body with both the cross-run marker
// and the per-run tag.
func WrapSummaryBody(content, runTag string) string {
	return SummaryMarker + "\n" + fmt.Sprintf(PerRunMarkerFmt, runTag) + "\n" + content
}
