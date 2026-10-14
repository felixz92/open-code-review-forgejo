package format

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The findings ledger is a resolution-tracking block embedded in the sticky
// summary comment body. It is delimited by its own HTML markers and carries a
// versioned JSON payload so runs can maintain open/resolved state across runs
// (Forgejo has no conversation-resolve API; the summary is the only durable,
// bot-writable state on a pull request).
const (
	// LedgerStartMarker opens the ledger block in a summary body.
	LedgerStartMarker = "<!-- ocr-ledger -->"
	// LedgerEndMarker closes the ledger block in a summary body.
	LedgerEndMarker = "<!-- /ocr-ledger -->"
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

// EncodeLedger serializes the ledger as the marker-delimited block suitable
// for embedding in a summary body.
func EncodeLedger(l *Ledger) string {
	payload, err := json.Marshal(l)
	if err != nil {
		// Struct-only payload: marshalling cannot fail.
		return ""
	}
	return LedgerStartMarker + "\n" + string(payload) + "\n" + LedgerEndMarker
}

// errTruncatedLedger is returned when a ledger block has no end marker.
var errTruncatedLedger = errors.New("ledger block is truncated: missing end marker")

// StripLedger removes the ledger block from a summary body. It returns the
// body with the block (and its surrounding newlines) removed, the decoded
// ledger, and whether the block was present. A present-but-malformed block
// yields an error; a body without a block returns found=false and no error.
func StripLedger(body string) (string, *Ledger, bool, error) {
	i := strings.Index(body, LedgerStartMarker)
	if i < 0 {
		return body, nil, false, nil
	}
	j := strings.Index(body[i:], LedgerEndMarker)
	if j < 0 {
		return "", nil, false, errTruncatedLedger
	}
	j += i + len(LedgerEndMarker)
	clean := strings.TrimRight(body[:i], "\n")
	after := strings.TrimLeft(body[j:], "\n")
	switch {
	case after == "":
		// Block was at the very end: keep only the text before it.
	case clean == "":
		clean = after
	default:
		clean = clean + "\n\n" + after
	}
	payload := strings.Trim(body[i+len(LedgerStartMarker):j-len(LedgerEndMarker)], "\n")
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
