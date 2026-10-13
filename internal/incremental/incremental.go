// Package incremental skips OCR findings that overlap prior bot activity.
package incremental

// DefaultOverlapThreshold is the default IoU threshold for multi-line overlap.
const DefaultOverlapThreshold = 0.6

// Span is a resolved line span; Multiline is true when Start != End.
type Span struct {
	Start, End int64
	Multiline  bool
}

// HistoryEntry is a prior bot comment's path and span.
type HistoryEntry struct {
	Path string
	Span Span
}

// NewSpan resolves a comment's start/end lines into a tagged span.
// Returns nil when neither line is present (<= 0).
func NewSpan(start, end int64) *Span {
	if start <= 0 && end <= 0 {
		return nil
	}
	if start > 0 && end > 0 && start != end {
		s, e := start, end
		if s > e {
			s, e = e, s
		}
		return &Span{Start: s, End: e, Multiline: true}
	}
	single := end
	if single <= 0 {
		single = start
	}
	return &Span{Start: single, End: single}
}

// SameCommentSpan reports whether two spans represent the same comment:
// exact line equality for single-line spans, IoU strictly greater than the
// threshold for multi-line spans. Multi-line never matches single-line.
func SameCommentSpan(cur, other Span, threshold float64) bool {
	if cur.Multiline != other.Multiline {
		return false
	}
	if !cur.Multiline {
		return cur.Start == other.Start
	}
	overlap := min(cur.End, other.End) - max(cur.Start, other.Start) + 1
	if overlap <= 0 {
		return false
	}
	union := (cur.End - cur.Start + 1) + (other.End - other.Start + 1) - overlap
	if union <= 0 {
		return false
	}
	return float64(overlap)/float64(union) > threshold
}

// Overlaps reports whether the span on path overlaps any prior bot comment
// on the same path.
func Overlaps(path string, span Span, history []HistoryEntry, threshold float64) bool {
	for _, h := range history {
		if h.Path != path {
			continue
		}
		if SameCommentSpan(span, h.Span, threshold) {
			return true
		}
	}
	return false
}
