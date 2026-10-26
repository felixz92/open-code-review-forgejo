package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felixz92/open-code-review-forgejo/internal/format"
	"github.com/felixz92/open-code-review-forgejo/internal/poster"
)

// queryFake records every mutating call so tests can assert the query
// mode performs no writes.
type queryFake struct {
	comments []poster.IssueCommentInfo
	listErr  error

	createdReviews  int
	createdComments []poster.ReviewCommentInput
	submitStates    []string
	deletedReviews  []int64
	createdBodies   []string
	editedBodies    map[int64]string
}

func (f *queryFake) CreatePullReview(comments []poster.ReviewCommentInput, commitID string) (int64, error) {
	f.createdReviews++
	f.createdComments = comments
	return 1, nil
}

func (f *queryFake) SubmitPullReview(reviewID int64, state, body string) error {
	f.submitStates = append(f.submitStates, state)
	return nil
}

func (f *queryFake) DeletePullReview(reviewID int64) error {
	f.deletedReviews = append(f.deletedReviews, reviewID)
	return nil
}

func (f *queryFake) ListPullReviews() ([]poster.ReviewInfo, error) { return nil, nil }

func (f *queryFake) ListPullReviewComments(reviewID int64) ([]poster.ReviewCommentInfo, error) {
	return nil, nil
}

func (f *queryFake) ListIssueComments() ([]poster.IssueCommentInfo, error) {
	return f.comments, f.listErr
}

func (f *queryFake) CreateIssueComment(body string) (int64, error) {
	f.createdBodies = append(f.createdBodies, body)
	return 99, nil
}

func (f *queryFake) EditIssueComment(commentID int64, body string) error {
	if f.editedBodies == nil {
		f.editedBodies = map[int64]string{}
	}
	f.editedBodies[commentID] = body
	return nil
}

func (f *queryFake) GetDiff() ([]byte, error) { return nil, nil }

func (f *queryFake) GetPullRequest() (*poster.PullRequestInfo, error) {
	return &poster.PullRequestInfo{HeadSHA: "head"}, nil
}

func (f *queryFake) WhoAmI() (string, error) { return "ci-bot", nil }

func summaryBody(sha string) string {
	body := format.SummaryMarker + "\ncontent"
	if sha != "" {
		body += "\n" + format.FormatLastReviewedMarker(sha)
	}
	return body
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestPrintLastReviewedFound(t *testing.T) {
	f := &queryFake{comments: []poster.IssueCommentInfo{
		{ID: 2, Body: "unrelated comment"},
		{ID: 3, Body: summaryBody("abc123")},
	}}
	out := captureStdout(t, func() {
		if code := runPrintLastReviewed(f); code != 0 {
			t.Fatalf("runPrintLastReviewed = %d, want 0", code)
		}
	})
	if out != "abc123\n" {
		t.Fatalf("stdout = %q, want %q", out, "abc123\n")
	}
}

func TestPrintLastReviewedNoSummary(t *testing.T) {
	f := &queryFake{comments: []poster.IssueCommentInfo{{ID: 2, Body: "unrelated"}}}
	out := captureStdout(t, func() {
		if code := runPrintLastReviewed(f); code != 0 {
			t.Fatalf("runPrintLastReviewed = %d, want 0", code)
		}
	})
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
}

func TestPrintLastReviewedSummaryWithoutMarker(t *testing.T) {
	f := &queryFake{comments: []poster.IssueCommentInfo{{ID: 3, Body: summaryBody("")}}}
	out := captureStdout(t, func() {
		if code := runPrintLastReviewed(f); code != 0 {
			t.Fatalf("runPrintLastReviewed = %d, want 0", code)
		}
	})
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
}

func TestPrintLastReviewedAPIError(t *testing.T) {
	f := &queryFake{listErr: os.ErrPermission}
	var out, errOut string
	out = captureStdout(t, func() {
		errOut = captureStderr(t, func() {
			if code := runPrintLastReviewed(f); code != 0 {
				t.Fatalf("runPrintLastReviewed = %d, want 0 on API failure", code)
			}
		})
	})
	if out != "" {
		t.Fatalf("stdout = %q, want empty on API failure", out)
	}
	if !strings.Contains(errOut, "warning") || !strings.Contains(errOut, "last-reviewed") {
		t.Fatalf("stderr = %q, want a warning naming the last-reviewed lookup", errOut)
	}
}

func TestPrintLastReviewedNewestSummaryWins(t *testing.T) {
	// Many comments, two sticky summaries: the newest match must win.
	comments := []poster.IssueCommentInfo{{ID: 1, Body: summaryBody("oldest111")}}
	for i := 0; i < 50; i++ {
		comments = append(comments, poster.IssueCommentInfo{ID: int64(i + 2), Body: "filler"})
	}
	comments = append(comments, poster.IssueCommentInfo{ID: 100, Body: summaryBody("newest222")})
	f := &queryFake{comments: comments}
	out := captureStdout(t, func() { _ = runPrintLastReviewed(f) })
	if out != "newest222\n" {
		t.Fatalf("stdout = %q, want %q (newest summary wins)", out, "newest222\n")
	}
}

func TestPrintLastReviewedPerformsNoWrites(t *testing.T) {
	f := &queryFake{comments: []poster.IssueCommentInfo{{ID: 3, Body: summaryBody("abc123")}}}
	out := captureStdout(t, func() { _ = runPrintLastReviewed(f) })
	if !strings.Contains(out, "abc123") {
		t.Fatalf("query must still report the SHA, got %q", out)
	}
	if f.createdReviews != 0 || len(f.createdComments) != 0 || len(f.submitStates) != 0 ||
		len(f.deletedReviews) != 0 || len(f.createdBodies) != 0 || len(f.editedBodies) != 0 {
		t.Fatalf("query mode must not write: %+v", f)
	}
}

func TestLoadCoverageDiff(t *testing.T) {
	if got := loadCoverageDiff(""); got != nil {
		t.Fatalf("absent flag must keep nil coverage, got %+v", got)
	}
	var errOut string
	errOut = captureStderr(t, func() {
		if got := loadCoverageDiff(filepath.Join(t.TempDir(), "missing.diff")); got == nil || got.Inv != nil {
			t.Fatalf("unreadable diff must mean no coverage, got %+v", got)
		}
	})
	if !strings.Contains(errOut, "warning") {
		t.Fatalf("stderr = %q, want a warning for the unreadable diff", errOut)
	}
	garbage := filepath.Join(t.TempDir(), "garbage.diff")
	if err := os.WriteFile(garbage, []byte("not a diff"), 0o600); err != nil {
		t.Fatal(err)
	}
	errOut = captureStderr(t, func() {
		if got := loadCoverageDiff(garbage); got == nil || got.Inv != nil {
			t.Fatalf("unparseable diff must mean no coverage, got %+v", got)
		}
	})
	if !strings.Contains(errOut, "warning") {
		t.Fatalf("stderr = %q, want a warning for the unparseable diff", errOut)
	}
	good := filepath.Join(t.TempDir(), "good.diff")
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-x()\n+y()\n"
	if err := os.WriteFile(good, []byte(diff), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadCoverageDiff(good); got == nil || got.Inv == nil {
		t.Fatalf("parseable diff must produce an inventory, got %+v", got)
	}
}
