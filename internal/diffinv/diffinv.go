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

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Range is an inclusive new-file line range covered by a hunk.
type Range struct {
	Start, End int64
}

// Inventory maps files to their new-line ranges and records whether the
// inventory is complete enough to prove a comment out-of-diff.
type Inventory struct {
	// Files holds the changed regions of the NEW file (post-image line
	// numbering); OldFiles the touched regions of the OLD file (pre-image
	// line numbering).
	Files    map[string][]Range
	OldFiles map[string][]Range
	Complete bool
}

var errNotADiff = fmt.Errorf("content is not a unified diff")

// New parses a unified diff into an inventory. The inventory is complete
// only when the diff is a unified diff and every hunk's observed line count
// matches its declared count.
func New(diff []byte) (*Inventory, error) {
	inv := &Inventory{Files: map[string][]Range{}, OldFiles: map[string][]Range{}}
	if len(bytes.TrimSpace(diff)) == 0 {
		return inv, nil
	}
	sc := bufio.NewScanner(bytes.NewReader(diff))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var curFile string
	var cur, curOld *Range
	var observed, expected, oldObserved, oldExpected int64
	sawHunk := false

	flush := func() {
		if cur == nil {
			return
		}
		if observed != expected || oldObserved != oldExpected {
			inv.Complete = false
		}
		if cur.End >= cur.Start {
			inv.Files[curFile] = append(inv.Files[curFile], *cur)
		}
		if curOld.End >= curOld.Start {
			inv.OldFiles[curFile] = append(inv.OldFiles[curFile], *curOld)
		}
		cur, curOld = nil, nil
	}
	inv.Complete = true
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			curFile = parseGitPath(line)
			if curFile != "" {
				inv.Files[curFile] = nil
				inv.OldFiles[curFile] = nil
			}
		case hunkHeader.MatchString(line):
			flush()
			m := hunkHeader.FindStringSubmatch(line)
			oldStart, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse hunk old start: %w", err)
			}
			oldExpected = 1
			if m[2] != "" {
				oldExpected, err = strconv.ParseInt(m[2], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("parse hunk old length: %w", err)
				}
			}
			start, err := strconv.ParseInt(m[3], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse hunk start: %w", err)
			}
			expected = 1
			if m[4] != "" {
				expected, err = strconv.ParseInt(m[4], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("parse hunk length: %w", err)
				}
			}
			r := Range{Start: start, End: start - 1}
			cur = &r
			observed = 0
			or := Range{Start: oldStart, End: oldStart - 1}
			curOld = &or
			oldObserved = 0
			sawHunk = true
		case cur == nil:
			continue
		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file" - not a content line.
		case strings.HasPrefix(line, "+") || strings.HasPrefix(line, " "):
			cur.End++
			observed++
			if line[0] == ' ' {
				// Context lines occupy a position on both sides.
				curOld.End++
				oldObserved++
			}
		case strings.HasPrefix(line, "-"):
			curOld.End++
			oldObserved++
		default:
			flush()
		}
	}
	flush()
	if sc.Err() != nil {
		return nil, fmt.Errorf("scan diff: %w", sc.Err())
	}
	if !sawHunk {
		if len(inv.Files) == 0 {
			return nil, errNotADiff
		}
		// Files are present but no hunk anywhere (e.g. empty-file additions
		// only): accept as an incomplete inventory.
		inv.Complete = false
	}
	return inv, nil
}

func parseGitPath(line string) string {
	// "diff --git a/<path> b/<path>". Paths may contain spaces (git does not
	// quote them), so locate the new path by its " b/" separator rather than
	// splitting on whitespace. C-quoted paths use " \"b/" instead.
	const sep = " b/"
	const quotedSep = " \"b/"
	path := ""
	if i := strings.LastIndex(line, quotedSep); i >= 0 {
		path = line[i+len(quotedSep):]
		path = strings.TrimSuffix(path, "\"")
		if u, err := strconv.Unquote("\"" + path + "\""); err == nil {
			path = u
		}
	} else if i := strings.LastIndex(line, sep); i >= 0 {
		path = line[i+len(sep):]
		if strings.HasPrefix(path, "\"") {
			if u, err := strconv.Unquote(path); err == nil {
				path = u
			}
		}
	}
	return path
}

// Classify resolves a comment's line range against the inventory:
// Valid (provably inside), Invalid (provably outside), Unknown (unprovable).
func (inv *Inventory) Classify(path string, start, end int64) Class {
	start, end, ok := normalizeSpan(start, end)
	if !ok {
		return Unknown
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

// ClassifyOld classifies a span given in the OLD file's line numbering (the
// side the diff was applied to). It reports Valid when the span overlaps any
// old-side region the diff touched, i.e. when the code under the span was
// provably changed by this diff. Overlap — not containment — is the proof:
// a change touching part of a region changes the region. This answers a
// different question than Classify, which checks containment in the NEW
// file's numbering and is only meaningful for spans recorded at the new head.
func (inv *Inventory) ClassifyOld(path string, start, end int64) Class {
	start, end, ok := normalizeSpan(start, end)
	if !ok {
		return Unknown
	}
	if !inv.Complete {
		return Unknown
	}
	ranges, ok := inv.OldFiles[path]
	if !ok {
		return Invalid
	}
	if len(ranges) == 0 {
		return Unknown
	}
	for _, r := range ranges {
		if start <= r.End && end >= r.Start {
			return Valid
		}
	}
	return Invalid
}

// normalizeSpan normalizes a 1-based inclusive line span the same way
// incremental.NewSpan does: a missing end yields nothing to prove and
// reversed ranges are swapped instead of rejected.
func normalizeSpan(start, end int64) (int64, int64, bool) {
	if end <= 0 {
		return 0, 0, false
	}
	if start <= 0 {
		start = end
	}
	if start > end {
		start, end = end, start
	}
	return start, end, true
}
