// Package review parses the JSON output of `ocr review --format json`.
package review

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"
)

// Comment is a single finding from open-code-review.
type Comment struct {
	Path           string `json:"path"`
	StartLine      int64  `json:"start_line"`
	EndLine        int64  `json:"end_line"`
	Content        string `json:"content"`
	SuggestionCode string `json:"suggestion_code"`
	ExistingCode   string `json:"existing_code"`
	Category       string `json:"category"`
	Severity       string `json:"severity"`
}

// Warning is a non-fatal issue reported during the review run.
type Warning struct {
	File    string `json:"file"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ManifestInput carries the resolved SHAs OCR actually reviewed.
type ManifestInput struct {
	ResolvedHead string `json:"resolved_head"`
	ResolvedBase string `json:"resolved_base"`
}

// Manifest is the run manifest embedded in the result.
type Manifest struct {
	Input ManifestInput `json:"input"`
}

// StatusFailed is the only Status value that classifies a run as failed.
// Publication and CLI code must compare against this constant rather than
// a duplicated string literal.
const StatusFailed = "failed"

// Result is the parsed `ocr review --format json` document.
type Result struct {
	Comments []Comment `json:"comments"`
	Warnings []Warning `json:"warnings"`
	Message  string    `json:"message"`
	Manifest Manifest  `json:"manifest"`
	// Status is the run-status marker from `ocr review --format json`.
	// Only the exact value StatusFailed means a failed run; absent or any
	// other value keeps the pre-status behavior.
	Status string `json:"status"`
}

const stderrTailLimit = 2000

// Option customizes loading.
type Option func(*loader)

type loader struct {
	stderrLogPath string
}

// WithStderrLog points the loader at the OCR stderr log, whose tail is
// included in the error when the result JSON cannot be parsed.
func WithStderrLog(path string) Option {
	return func(l *loader) { l.stderrLogPath = path }
}

// Load reads and parses the review result JSON at path.
func Load(path string, opts ...Option) (*Result, error) {
	l := &loader{}
	for _, o := range opts {
		o(l)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read review result %s: %w", path, err)
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse review result %s: %w%s", path, err, stderrTail(l.stderrLogPath))
	}
	return &r, nil
}

// stderrTail returns a human-readable suffix with the last lines of the
// stderr log, or "" when the log is absent or unreadable.
func stderrTail(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(data)
	if len(s) > stderrTailLimit {
		s = s[len(s)-stderrTailLimit:]
		// Advance to the next rune boundary so a split multi-byte rune at
		// the cut does not emit invalid UTF-8.
		for len(s) > 0 && !utf8.RuneStart(s[0]) {
			s = s[1:]
		}
	}
	return "\nOCR stderr log tail:\n" + s
}
