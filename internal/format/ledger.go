package format

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// The findings ledger is a resolution-tracking block embedded in the sticky
// summary comment body. It is delimited by its own HTML markers and carries a
// versioned JSON payload so runs can maintain open/resolved state across runs
// (Forgejo has no conversation-resolve API; the summary is the only durable,
// bot-writable state on a pull request).
const (
	// LedgerStartMarker opens the ledger block: a single self-contained
	// HTML comment whose JSON payload stays inside the comment so the
	// block is invisible in rendered Markdown.
	LedgerStartMarker = "<!-- ocr-ledger "
	// LedgerEndMarker closes the ledger block on the same line.
	LedgerEndMarker = " -->"
	// LedgerVersion is the schema version this build writes and reads.
	LedgerVersion = 1
	// MaxResolvedEntries bounds the retained resolved entries: the cap keeps
	// the summary from growing forever on long-lived PRs.
	MaxResolvedEntries = 20
)

// LedgerEntry is one tracked finding: a file path plus the line span it was
// posted on, the run that first saw it, and — once fixed — the run and date
// of its resolution. Content keeps a short snippet of the finding text.
type LedgerEntry struct {
	Path         string `json:"path"`
	StartLine    int64  `json:"start_line"`
	EndLine      int64  `json:"end_line"`
	FirstSeen    string `json:"first_seen"`
	ResolvedIn   string `json:"resolved_in,omitempty"`
	ResolvedDate string `json:"resolved_date,omitempty"`
	Content      string `json:"content,omitempty"`
}

// Ledger is the decoded JSON payload between the ledger markers: the schema
// version, the still-open findings, and the recently resolved findings (most
// recent last).
type Ledger struct {
	Version  int           `json:"version"`
	Open     []LedgerEntry `json:"open,omitempty"`
	Resolved []LedgerEntry `json:"resolved,omitempty"`
}

// EncodeLedger serializes the ledger as a single self-contained HTML comment
// suitable for embedding in a summary body. The payload must stay inside the
// comment: a start marker that closes itself would render the JSON as
// visible Markdown text. Ledger entry content is neutralized upstream
// (snippet), so the payload cannot contain the end marker.
func EncodeLedger(l *Ledger) string {
	payload, err := json.Marshal(l)
	if err != nil {
		// Struct-only payload: marshalling cannot fail.
		return ""
	}
	return LedgerStartMarker + string(payload) + LedgerEndMarker
}

// errTruncatedLedger is returned when a ledger block has no end marker.
var errTruncatedLedger = errors.New("ledger block is truncated: missing end marker")

// StripLedger removes the ledger block from a summary body. It returns the
// body with the block (and its surrounding newlines) removed, the decoded
// ledger, and whether the block was present. A present-but-malformed block
// yields an error; a body without a block returns found=false and no error.
func StripLedger(body string) (string, *Ledger, bool, error) {
	// The genuine block is always the last ledger comment in the body
	// (finalizeBody/trackedNoIssuesBody append it after all rendered
	// sections), and ledger payloads neutralize comment terminators while
	// summary-rendered content does not. Anchor at the LAST start marker
	// so a ledger marker quoted in summary content cannot relocate the
	// block start.
	i := strings.LastIndex(body, LedgerStartMarker)
	if i < 0 {
		return body, nil, false, nil
	}
	rest := body[i+len(LedgerStartMarker):]
	j := strings.Index(rest, LedgerEndMarker)
	if j < 0 {
		return "", nil, false, errTruncatedLedger
	}
	payload := rest[:j]
	after := strings.TrimLeft(rest[j+len(LedgerEndMarker):], "\n")
	clean := strings.TrimRight(body[:i], "\n")
	switch {
	case after == "":
		// Block was at the very end: keep only the text before it.
	case clean == "":
		clean = after
	default:
		clean = clean + "\n\n" + after
	}
	l, err := decodeLedger(payload)
	if err != nil {
		return "", nil, false, err
	}
	return clean, l, true, nil
}

// ParseLedger extracts and decodes the ledger block from a summary body.
// found reports whether a block was present; err is non-nil when a present
// block is corrupt (which callers degrade from by starting a fresh ledger).
func ParseLedger(body string) (*Ledger, bool, error) {
	_, l, found, err := StripLedger(body)
	if err != nil {
		return nil, false, err
	}
	return l, found, nil
}

// decodeLedger parses the ledger JSON payload. Missing fields decode as zero
// values; a missing version decodes as the current version, and a payload
// written by a newer schema is rejected so state is never misinterpreted.
func decodeLedger(payload string) (*Ledger, error) {
	var l Ledger
	if err := json.Unmarshal([]byte(payload), &l); err != nil {
		return nil, fmt.Errorf("ledger JSON corrupt: %w", err)
	}
	if l.Version > LedgerVersion {
		return nil, fmt.Errorf("unsupported ledger version %d (max supported %d)", l.Version, LedgerVersion)
	}
	if l.Version == 0 {
		l.Version = LedgerVersion
	}
	return &l, nil
}

// FormatLedgerSections renders the resolution-tracking sections appended to a
// tracking-enabled summary: the findings still open (summary-fallback style:
// path, span, content) followed by the findings resolved during this run with
// their resolution stamp. Entries render sorted by path and span for
// deterministic output.
func FormatLedgerSections(open, resolved []LedgerEntry) string {
	if len(open) == 0 && len(resolved) == 0 {
		return ""
	}
	var b strings.Builder
	if len(open) > 0 {
		fmt.Fprintf(&b, "\n\n---\n\n### 📌 Findings still open: %d\n", len(open))
		for _, e := range sortedLedgerEntries(open) {
			b.WriteString("\n\n")
			b.WriteString(ledgerItem(e, "", ""))
		}
	}
	if len(resolved) > 0 {
		fmt.Fprintf(&b, "\n\n---\n\n### ✅ Resolved **%d** finding(s) in this run\n", len(resolved))
		for _, e := range sortedLedgerEntries(resolved) {
			b.WriteString("\n\n")
			b.WriteString(ledgerItem(e, e.ResolvedIn, e.ResolvedDate))
		}
	}
	return b.String()
}

// sortedLedgerEntries returns the entries ordered by path and span.
func sortedLedgerEntries(entries []LedgerEntry) []LedgerEntry {
	out := append([]LedgerEntry(nil), entries...)
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if a.Path != c.Path {
			return a.Path < c.Path
		}
		if a.StartLine != c.StartLine {
			return a.StartLine < c.StartLine
		}
		return a.EndLine < c.EndLine
	})
	return out
}

// ledgerItem renders one ledger entry like a summary fallback item. Resolved
// entries show the run and date of their resolution.
func ledgerItem(e LedgerEntry, runTag, resolvedDate string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### 📄 `%s`", e.Path)
	if e.StartLine > 0 && e.EndLine > 0 {
		if e.StartLine == e.EndLine {
			fmt.Fprintf(&b, " (L%d)", e.EndLine)
		} else {
			fmt.Fprintf(&b, " (L%d-L%d)", e.StartLine, e.EndLine)
		}
	}
	b.WriteString("\n\n")
	if runTag != "" || resolvedDate != "" {
		fmt.Fprintf(&b, "✅ RESOLVED (run %s, %s)\n\n", runTag, resolvedDate)
	}
	b.WriteString(e.Content)
	return b.String()
}
