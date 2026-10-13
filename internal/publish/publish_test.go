package publish

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/felixz92/open-code-review-forgejo/internal/format"
	"github.com/felixz92/open-code-review-forgejo/internal/policy"
	"github.com/felixz92/open-code-review-forgejo/internal/poster"
	"github.com/felixz92/open-code-review-forgejo/internal/review"
)

// fakePoster records calls and serves scripted data.
type fakePoster struct {
	// scripted
	reviews        []poster.ReviewInfo
	listReviewsErr error
	reviewComments map[int64][]poster.ReviewCommentInfo
	issueComments  []poster.IssueCommentInfo
	diff           []byte
	headSHA        string
	createErr      error

	// recorded
	createdComments []poster.ReviewCommentInput
	createdCommitID string
	submitted       []string
	submitBody      string
	deletedReviews  []int64
	createdSummary  []string
	editedSummary   map[int64]string
}

func newFake() *fakePoster {
	return &fakePoster{reviewComments: map[int64][]poster.ReviewCommentInfo{}, editedSummary: map[int64]string{}}
}

func (f *fakePoster) CreatePullReview(comments []poster.ReviewCommentInput, commitID string) (int64, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.createdComments = comments
	f.createdCommitID = commitID
	return 101, nil
}
func (f *fakePoster) SubmitPullReview(reviewID int64, body string) error {
	f.submitted = append(f.submitted, fmt.Sprint(reviewID))
	f.submitBody = body
	return nil
}
func (f *fakePoster) DeletePullReview(reviewID int64) error {
	f.deletedReviews = append(f.deletedReviews, reviewID)
	return nil
}
func (f *fakePoster) ListPullReviews() ([]poster.ReviewInfo, error) {
	if f.listReviewsErr != nil {
		return nil, f.listReviewsErr
	}
	return f.reviews, nil
}
func (f *fakePoster) ListPullReviewComments(reviewID int64) ([]poster.ReviewCommentInfo, error) {
	return f.reviewComments[reviewID], nil
}
func (f *fakePoster) ListIssueComments() ([]poster.IssueCommentInfo, error) {
	return f.issueComments, nil
}
func (f *fakePoster) CreateIssueComment(body string) (int64, error) {
	f.createdSummary = append(f.createdSummary, body)
	return 55, nil
}
func (f *fakePoster) EditIssueComment(commentID int64, body string) error {
	f.editedSummary[commentID] = body
	return nil
}
func (f *fakePoster) GetDiff() ([]byte, error) { return f.diff, nil }
func (f *fakePoster) GetPullRequest() (*poster.PullRequestInfo, error) {
	return &poster.PullRequestInfo{HeadSHA: f.headSHA}, nil
}
func (f *fakePoster) WhoAmI() (string, error) { return "ci-bot", nil }

func result(comments ...review.Comment) *review.Result {
	return &review.Result{Comments: comments, Message: "done"}
}

func c(path string, start, end int64, severity string) review.Comment {
	return review.Comment{Path: path, StartLine: start, EndLine: end, Content: "content " + path, Severity: severity, Category: "bug"}
}

func TestPublishAllInline(t *testing.T) {
	f := newFake()
	r := result(c("a.go", 1, 1, "low"), c("a.go", 5, 9, "high"), c("b.go", 2, 2, "medium"))
	stats, err := Publish(r, f, Options{RunTag: "42-x", CommitID: "sha1"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 3 || stats.Total != 3 || stats.Failed != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(f.createdComments) != 3 || f.createdCommitID != "sha1" {
		t.Fatalf("created = %+v commit=%q", f.createdComments, f.createdCommitID)
	}
	// Deterministic order: a.go:1, a.go:5-9, b.go:2.
	if f.createdComments[0].Path != "a.go" || f.createdComments[0].NewLine != 1 ||
		f.createdComments[1].NewLine != 9 || f.createdComments[2].Path != "b.go" {
		t.Fatalf("order = %+v", f.createdComments)
	}
	// One pending create then one submit with non-empty body.
	if len(f.submitted) != 1 || f.submitBody == "" {
		t.Fatalf("submitted = %v body=%q", f.submitted, f.submitBody)
	}
	// Summary anchor created before, finalized after (two writes to same id).
	if len(f.createdSummary) != 1 || len(f.editedSummary) != 1 {
		t.Fatalf("created=%d edited=%d", len(f.createdSummary), len(f.editedSummary))
	}
	if !strings.Contains(f.editedSummary[55], format.SummaryMarker) {
		t.Fatal("final summary must carry the sticky marker")
	}
}

func TestPublishMultilineBodyStatesSpan(t *testing.T) {
	f := newFake()
	r := result(c("a.go", 5, 9, "high"))
	if _, err := Publish(r, f, Options{RunTag: "t", CommitID: "s"}); err != nil {
		t.Fatal(err)
	}
	if f.createdComments[0].NewLine != 9 {
		t.Fatalf("multiline must anchor at end_line, got %d", f.createdComments[0].NewLine)
	}
	if !strings.Contains(f.createdComments[0].Body, "lines 5-9") {
		t.Fatalf("body must state the span: %q", f.createdComments[0].Body)
	}
}

func TestPublishNoLineAndRoutedGoToSummary(t *testing.T) {
	f := newFake()
	r := result(
		review.Comment{Path: "a.go", Content: "no lines", Category: "bug", Severity: "low"},
		c("a.go", 2, 2, "low"),
		c("a.go", 5, 5, "critical"),
	)
	stats, err := Publish(r, f, Options{
		RunTag: "t", CommitID: "s",
		Route: policy.Build("medium", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Summary != 1 || stats.Routed != 1 || stats.Inline != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	final := f.editedSummary[55]
	if !strings.Contains(final, "No line information provided") {
		t.Fatal("no-line comment must appear with reason")
	}
	if !strings.Contains(final, "Routed to summary (severity low") {
		t.Fatalf("routed comment must appear with reason:\n%s", final)
	}
}

func TestPublishOutOfDiffDroppedBeforeCreate(t *testing.T) {
	f := newFake()
	f.diff = []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,2 @@\n keep()\n-removed()\n+added()\n")
	r := result(c("a.go", 1, 2, "low"), c("a.go", 0, 99, "low"))
	stats, err := Publish(r, f, Options{RunTag: "t", CommitID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 1 || stats.Inline != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(f.createdComments) != 1 {
		t.Fatalf("only the in-diff comment may be submitted, got %+v", f.createdComments)
	}
	if !strings.Contains(f.editedSummary[55], "out of diff") {
		t.Fatal("dropped comment must be reported as out of diff")
	}
}

func TestPublishIncompleteDiffKeepsComment(t *testing.T) {
	f := newFake()
	f.diff = []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,10 @@\n+short()\n+diff()\n")
	r := result(c("a.go", 0, 99, "low"))
	stats, err := Publish(r, f, Options{RunTag: "t", CommitID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 1 || stats.Failed != 0 {
		t.Fatalf("unknown classification must keep the comment: %+v", stats)
	}
}

func TestPublishIncrementalSkipsOverlap(t *testing.T) {
	f := newFake()
	f.reviews = []poster.ReviewInfo{{ID: 9, State: "COMMENT", Reviewer: "ci-bot"}}
	f.reviewComments[9] = []poster.ReviewCommentInfo{
		{Body: format.BotMarker + " old -->\n> File `a.go` lines 5-8", Path: "a.go", NewLine: 8},
	}
	r := result(c("a.go", 5, 8, "low"), c("b.go", 1, 1, "low"))
	stats, err := Publish(r, f, Options{RunTag: "t", CommitID: "s", Incremental: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 1 || stats.Inline != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(f.createdComments) != 1 || f.createdComments[0].Path != "b.go" {
		t.Fatalf("only b.go may be created, got %+v", f.createdComments)
	}
	if !strings.Contains(f.editedSummary[55], "Skipped (overlap with history): 1 comment(s)") {
		t.Fatal("summary must report the skip")
	}
}

func TestPublishIncrementalDisabledWhenHistoryFails(t *testing.T) {
	f := newFake()
	f.listReviewsErr = errors.New("read API down")
	r := result(c("a.go", 1, 1, "low"))
	stats, err := Publish(r, f, Options{RunTag: "t", CommitID: "s", Incremental: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 1 || stats.Skipped != 0 {
		t.Fatalf("history failure must disable filtering and proceed: %+v", stats)
	}
	if stats.Failed != 0 {
		t.Fatalf("no comments may be marked failed on history read failure: %+v", stats)
	}
}

func TestPublishDeletesStalePending(t *testing.T) {
	f := newFake()
	f.reviews = []poster.ReviewInfo{
		{ID: 7, State: "PENDING", Reviewer: "ci-bot"},
		{ID: 8, State: "PENDING", Reviewer: "someone-else"},
		{ID: 9, State: "COMMENT", Reviewer: "ci-bot"},
	}
	r := result(c("a.go", 1, 1, "low"))
	if _, err := Publish(r, f, Options{RunTag: "t", CommitID: "s"}); err != nil {
		t.Fatal(err)
	}
	if len(f.deletedReviews) != 1 || f.deletedReviews[0] != 7 {
		t.Fatalf("only own pending reviews deleted, got %v", f.deletedReviews)
	}
}

func TestPublishStickySummaryUpdatesInPlace(t *testing.T) {
	f := newFake()
	f.issueComments = []poster.IssueCommentInfo{
		{ID: 30, Body: "unrelated"},
		{ID: 31, Body: format.SummaryMarker + "\nold body"},
	}
	r := result(c("a.go", 1, 1, "low"))
	if _, err := Publish(r, f, Options{RunTag: "t", CommitID: "s", Sticky: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.createdSummary) != 0 {
		t.Fatal("sticky summary must update, not create a second one")
	}
	if body, ok := f.editedSummary[31]; !ok || !strings.Contains(body, "found **1** issue(s)") {
		t.Fatalf("summary 31 must be updated in place, got %q", body)
	}
}

func TestPublishNoCommentsLGTM(t *testing.T) {
	f := newFake()
	stats, err := Publish(result(), f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 0 || stats.Inline != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(f.createdComments) != 0 || len(f.submitted) != 0 {
		t.Fatal("no review must be created without comments")
	}
	if !strings.Contains(f.createdSummary[0], "✅") {
		t.Fatalf("LGTM summary expected, got %q", f.createdSummary[0])
	}
}

func TestPublishCommitDriftFailsCommentsToSummary(t *testing.T) {
	f := newFake()
	f.createErr = fmt.Errorf("422: commit sha does not exist")
	r := result(c("a.go", 1, 2, "low"))
	stats, err := Publish(r, f, Options{RunTag: "t", CommitID: "stale-sha"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 0 || stats.Failed != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if !strings.Contains(f.editedSummary[55], "stale-sha") || !strings.Contains(f.editedSummary[55], "content a.go") {
		t.Fatalf("failed comments must be reported with content:\n%s", f.editedSummary[55])
	}
}

func TestPublishNonStickyCreatesFreshSummary(t *testing.T) {
	f := newFake()
	f.issueComments = []poster.IssueCommentInfo{{ID: 31, Body: format.SummaryMarker + "\nold"}}
	r := result()
	if _, err := Publish(r, f, Options{RunTag: "t-2", Sticky: false}); err != nil {
		t.Fatal(err)
	}
	// Non-sticky mode must not reuse the old cross-run summary.
	if len(f.createdSummary) != 1 {
		t.Fatalf("non-sticky must create its own summary, created=%d", len(f.createdSummary))
	}
}
