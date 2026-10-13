package diffinv

import "testing"

func TestEmptyNewFileOnlyDiff(t *testing.T) {
	// An empty new file yields "diff --git" metadata without any hunk.
	// The inventory must be accepted but marked incomplete: with no hunks we
	// cannot prove any line in or out of the diff.
	inv, err := New([]byte("diff --git a/README.md b/README.md\nnew file mode 100644\nindex 0000000..e69de29\n"))
	if err != nil {
		t.Fatalf("empty-file-only diff must parse, got %v", err)
	}
	if inv.Complete {
		t.Fatal("hunkless diff must be incomplete")
	}
	if _, ok := inv.Files["README.md"]; !ok {
		t.Fatalf("file should be known, files = %v", inv.Files)
	}
	if got := inv.Classify("README.md", 0, 99); got != Unknown {
		t.Fatalf("no hunks to prove anything, got %v", got)
	}
}

func TestPlainNonDiffContent(t *testing.T) {
	if _, err := New([]byte("<html>oops</html>")); err == nil {
		t.Fatal("non-diff content must still error")
	}
}
