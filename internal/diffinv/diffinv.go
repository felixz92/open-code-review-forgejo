// Package diffinv parses a unified diff into per-file new-line inventories
// and classifies comment line ranges against it.
package diffinv

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Class is the tri-state classification of a comment against the diff.
type Class int

const (
	// Valid means the range is provably inside the diff.
	Valid Class = iota
	// Invalid means the range is provably outside the diff.
	Invalid
	// Unknown means classification could not be proven either way.
	Unknown
)

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// Range is an inclusive new-file line range covered by a hunk.
type Range struct {
	Start, End int64
}

// Inventory maps files to their new-line ranges and records whether the
// inventory is complete enough to prove a comment out-of-diff.
type Inventory struct {
	Files    map[string][]Range
	Complete bool
}

var errNotADiff = fmt.Errorf("content is not a unified diff")

// New parses a unified diff into an inventory. The inventory is complete
// only when the diff is a unified diff and every hunk's observed line count
// matches its declared count.
func New(diff []byte) (*Inventory, error) {
	inv := &Inventory{Files: map[string][]Range{}}
	if len(bytes.TrimSpace(diff)) == 0 {
		return inv, nil
	}
	sc := bufio.NewScanner(bytes.NewReader(diff))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var curFile string
	var cur *Range
	var observed, expected int64
	sawHunk := false

	flush := func() {
		if cur == nil {
			return
		}
		if observed != expected {
			inv.Complete = false
		}
		if cur.End >= cur.Start {
			inv.Files[curFile] = append(inv.Files[curFile], *cur)
		}
		cur = nil
	}
	inv.Complete = true
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			curFile = parseGitPath(line)
		case hunkHeader.MatchString(line):
			flush()
			m := hunkHeader.FindStringSubmatch(line)
			start, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse hunk start: %w", err)
			}
			expected = 1
			if m[2] != "" {
				expected, err = strconv.ParseInt(m[2], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("parse hunk length: %w", err)
				}
			}
			r := Range{Start: start, End: start - 1}
			cur = &r
			observed = 0
			sawHunk = true
		case cur == nil:
			continue
		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file" - not a content line.
		case strings.HasPrefix(line, "+") || strings.HasPrefix(line, " "):
			cur.End++
			observed++
		case strings.HasPrefix(line, "-"):
			// removed line: no new-file position
		default:
			flush()
		}
	}
	flush()
	if sc.Err() != nil {
		return nil, fmt.Errorf("scan diff: %w", sc.Err())
	}
	if !sawHunk || curFile == "" && len(inv.Files) == 0 {
		return nil, errNotADiff
	}
	return inv, nil
}

func parseGitPath(line string) string {
	// "diff --git a/<path> b/<path>"
	parts := strings.Fields(line)
	if len(parts) >= 4 {
		return strings.TrimPrefix(parts[3], "b/")
	}
	return ""
}

// Classify resolves a comment's line range against the inventory:
// Valid (provably inside), Invalid (provably outside), Unknown (unprovable).
func (inv *Inventory) Classify(path string, start, end int64) Class {
	if end <= 0 {
		return Unknown
	}
	if start <= 0 {
		start = end
	}
	if start > end {
		return Invalid
	}
	if !inv.Complete {
		return Unknown
	}
	ranges, ok := inv.Files[path]
	if !ok {
		return Invalid
	}
	if len(ranges) == 0 {
		return Unknown
	}
	for _, r := range ranges {
		if start >= r.Start && end <= r.End {
			return Valid
		}
	}
	return Invalid
}
