package diffinv

import "testing"

const simpleDiff = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,4 @@
 package a
 
-func old() {}
+func new() {}
+func extra() {}
diff --git a/b.go b/b.go
index 333..444 100644
--- a/b.go
+++ b/b.go
@@ -10,2 +10,2 @@
 kept()
-removed()
+added()
`

const truncatedDiff = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,10 @@
+only three
+new lines
+here
`

func TestInventoryFromSimpleDiff(t *testing.T) {
	inv, err := New([]byte(simpleDiff))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !inv.Complete {
		t.Fatal("simple diff should yield a complete inventory")
	}
	a, ok := inv.Files["a.go"]
	if !ok {
		t.Fatalf("a.go missing from inventory: %+v", inv.Files)
	}
	if len(a) != 1 || a[0].Start != 1 || a[0].End != 4 {
		t.Fatalf("a.go ranges = %+v, want [1-4]", a)
	}
	b, ok := inv.Files["b.go"]
	if !ok || len(b) != 1 || b[0].Start != 10 || b[0].End != 11 {
		t.Fatalf("b.go ranges = %+v, want [10-11]", b)
	}
}

func TestClassify(t *testing.T) {
	inv, err := New([]byte(simpleDiff))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path string
		start, end int64
		want       Class
	}{
		{"in range", "a.go", 1, 4, Valid},
		{"single in range", "a.go", 0, 3, Valid},
		{"out of range", "a.go", 0, 99, Invalid},
		{"reversed", "a.go", 4, 1, Valid},
		{"unknown file", "c.go", 0, 3, Invalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inv.Classify(tt.path, tt.start, tt.end); got != tt.want {
				t.Fatalf("Classify(%q, %d, %d) = %v, want %v", tt.path, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestClassifyIncompleteInventory(t *testing.T) {
	inv, err := New([]byte(truncatedDiff))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Complete {
		t.Fatal("truncated hunk should make inventory incomplete")
	}
	// Even out-of-range comments stay unknown: we cannot prove anything.
	if got := inv.Classify("a.go", 0, 99); got != Unknown {
		t.Fatalf("incomplete inventory should classify unknown, got %v", got)
	}
}

func TestNewEmptyDiff(t *testing.T) {
	inv, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil): %v", err)
	}
	if len(inv.Files) != 0 || inv.Complete {
		t.Fatalf("empty diff should yield empty incomplete inventory, got %+v", inv)
	}
}

func TestClassifyNoEndLine(t *testing.T) {
	inv, _ := New([]byte(simpleDiff))
	if got := inv.Classify("a.go", 0, 0); got != Unknown {
		t.Fatalf("comment without end line should be unknown, got %v", got)
	}
}

func TestParseGitPathWithSpaces(t *testing.T) {
	got := parseGitPath("diff --git a/foo bar.go b/foo bar.go")
	if got != "foo bar.go" {
		t.Fatalf("parseGitPath = %q, want %q", got, "foo bar.go")
	}
}

func TestParseGitPathQuoted(t *testing.T) {
	got := parseGitPath(`diff --git "a/\346\226\207" "b/\346\226\207"`)
	if got != "文" {
		t.Fatalf("parseGitPath = %q", got)
	}
}
