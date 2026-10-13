package incremental

import "testing"

func TestCommentSpan(t *testing.T) {
	tests := []struct {
		name               string
		start, end         int64
		wantNil            bool
		wantStart, wantEnd int64
		wantMultiline      bool
	}{
		{"no lines", 0, 0, true, 0, 0, false},
		{"end only", 0, 5, false, 5, 5, false},
		{"start only", 5, 0, false, 5, 5, false},
		{"single", 5, 5, false, 5, 5, false},
		{"multi ordered", 5, 9, false, 5, 9, true},
		{"multi unordered", 9, 5, false, 5, 9, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := NewSpan(tt.start, tt.end)
			if tt.wantNil {
				if span != nil {
					t.Fatalf("expected nil span, got %+v", span)
				}
				return
			}
			if span == nil || span.Start != tt.wantStart || span.End != tt.wantEnd || span.Multiline != tt.wantMultiline {
				t.Fatalf("span = %+v, want start=%d end=%d multiline=%v", span, tt.wantStart, tt.wantEnd, tt.wantMultiline)
			}
		})
	}
}

func TestSameCommentSpan(t *testing.T) {
	tests := []struct {
		name      string
		a, b      Span
		wantMatch bool
	}{
		{"single equal", Span{Start: 5, End: 5}, Span{Start: 5, End: 5}, true},
		{"single different", Span{Start: 5, End: 5}, Span{Start: 6, End: 6}, false},
		{"multi exact", Span{Start: 5, End: 9, Multiline: true}, Span{Start: 5, End: 9, Multiline: true}, true},
		{"multi full overlap", Span{Start: 5, End: 8, Multiline: true}, Span{Start: 6, End: 8, Multiline: true}, true},
		{"multi disjoint", Span{Start: 5, End: 9, Multiline: true}, Span{Start: 10, End: 14, Multiline: true}, false},
		{"multi slight overlap below threshold", Span{Start: 1, End: 10, Multiline: true}, Span{Start: 10, End: 20, Multiline: true}, false},
		{"multi ioU equal to threshold", Span{Start: 1, End: 6, Multiline: true}, Span{Start: 2, End: 9, Multiline: true}, false},
		{"multi vs single", Span{Start: 5, End: 9, Multiline: true}, Span{Start: 5, End: 5}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameCommentSpan(tt.a, tt.b, DefaultOverlapThreshold); got != tt.wantMatch {
				t.Fatalf("SameCommentSpan(%+v, %+v) = %v, want %v", tt.a, tt.b, got, tt.wantMatch)
			}
		})
	}
}

func TestSameCommentSpanIoUJustAboveThreshold(t *testing.T) {
	// IoU = 6/10 = 0.6 exactly at default threshold: not a match (strict >).
	if SameCommentSpan(Span{Start: 1, End: 6, Multiline: true}, Span{Start: 1, End: 10, Multiline: true}, 0.6) {
		t.Fatal("IoU exactly at threshold must not match")
	}
	// IoU = 7/10 = 0.7 > 0.6: match.
	if !SameCommentSpan(Span{Start: 1, End: 7, Multiline: true}, Span{Start: 1, End: 10, Multiline: true}, 0.6) {
		t.Fatal("IoU above threshold must match")
	}
}

func TestOverlapsHistory(t *testing.T) {
	history := []HistoryEntry{
		{Path: "a.go", Span: Span{Start: 5, End: 9, Multiline: true}},
		{Path: "b.go", Span: Span{Start: 1, End: 1}},
	}
	if !Overlaps("a.go", Span{Start: 6, End: 9, Multiline: true}, history, DefaultOverlapThreshold) {
		t.Fatal("overlapping span on same path should match")
	}
	if Overlaps("c.go", Span{Start: 6, End: 8}, history, DefaultOverlapThreshold) {
		t.Fatal("unknown path should not match")
	}
	if Overlaps("b.go", Span{Start: 99, End: 99}, history, DefaultOverlapThreshold) {
		t.Fatal("non-overlapping span should not match")
	}
}

func TestOverlapsEmptyHistory(t *testing.T) {
	if Overlaps("a.go", Span{Start: 1, End: 1}, nil, DefaultOverlapThreshold) {
		t.Fatal("empty history never overlaps")
	}
}
