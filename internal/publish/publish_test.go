package publish

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	reviews          []poster.ReviewInfo
	listReviewsErr   error
	reviewComments   map[int64][]poster.ReviewCommentInfo
	issueComments    []poster.IssueCommentInfo
	issueCommentsErr error
	diff             []byte
	getDiffErr       error
	headSHA          string
	createErr        error

	// recorded
	createdComments []poster.ReviewCommentInput
	createdCommitID string
	submitted       []string
	submitStates    []string
	submitBody      string
	submitErr       error
	deletedReviews  []int64
	createdSummary  []string
	editedSummary   map[int64]string
}

func newFake() *fakePoster {
	return &fakePoster{reviewComments: map[int64][]poster.ReviewCommentInfo{}, editedSummary: map[int64]string{}}
}

func (f *fakePoster) CreatePullReview(comments []poster.ReviewCommentInput, commitID string) (int64, error) {
	if commitID == "" {
		// Server contract: an empty commit makes the batch call fail.
		return 0, fmt.Errorf("empty commit id")
	}
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.createdComments = comments
	f.createdCommitID = commitID
	return 101, nil
}
func (f *fakePoster) SubmitPullReview(reviewID int64, state, body string) error {
	f.submitted = append(f.submitted, fmt.Sprint(reviewID))
	f.submitStates = append(f.submitStates, state)
	f.submitBody = body
	return f.submitErr
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
	return f.issueComments, f.issueCommentsErr
}
func (f *fakePoster) CreateIssueComment(body string) (int64, error) {
	f.createdSummary = append(f.createdSummary, body)
	return 55, nil
}
func (f *fakePoster) EditIssueComment(commentID int64, body string) error {
	f.editedSummary[commentID] = body
	return nil
}
func (f *fakePoster) GetDiff() ([]byte, error) { return f.diff, f.getDiffErr }
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
	f.headSHA = "head123"
	stats, err := Publish(result(), f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 0 || stats.Inline != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	// The empty approval review: no inline comments, resolved commit.
	if len(f.createdComments) != 0 || f.createdCommitID != "head123" {
		t.Fatalf("empty review must be created at the PR head: %+v commit=%q", f.createdComments, f.createdCommitID)
	}
	if len(f.submitStates) != 1 || f.submitStates[0] != StateApproved {
		t.Fatalf("submit states = %v, want [APPROVED]", f.submitStates)
	}
	if stats.State != StateApproved {
		t.Fatalf("stats.State = %q, want APPROVED", stats.State)
	}
	if !strings.Contains(f.createdSummary[0], "✅") {
		t.Fatalf("LGTM summary expected, got %q", f.createdSummary[0])
	}
}

func TestPublishNoCommentsExplicitCommit(t *testing.T) {
	f := newFake()
	stats, err := Publish(result(), f, Options{RunTag: "t", CommitID: "sha1"})
	if err != nil {
		t.Fatal(err)
	}
	if f.createdCommitID != "sha1" {
		t.Fatalf("commit = %q, want the configured --commit-id", f.createdCommitID)
	}
	if stats.State != StateApproved {
		t.Fatalf("stats.State = %q, want APPROVED", stats.State)
	}
}

func TestPublishNoCommentsCreationFailureLogsAndContinues(t *testing.T) {
	f := newFake()
	f.createErr = fmt.Errorf("500: boom")
	stats, err := Publish(result(), f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.State != StateComment {
		t.Fatalf("stats.State = %q, want COMMENT after creation failure", stats.State)
	}
	if len(f.createdSummary) != 1 || !strings.Contains(f.createdSummary[0], "✅") {
		t.Fatalf("LGTM summary must still publish, got %v", f.createdSummary)
	}
	if len(f.submitStates) != 0 {
		t.Fatalf("nothing may be submitted when creation failed: %v", f.submitStates)
	}
}

func TestPublishNoCommentsSubmitFailureRecordsComment(t *testing.T) {
	f := newFake()
	f.headSHA = "head123"
	f.submitErr = fmt.Errorf("403: self-approval refused")
	stats, err := Publish(result(), f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.State != StateComment {
		t.Fatalf("stats.State = %q, want COMMENT after submit failure", stats.State)
	}
	if len(f.submitted) != 1 {
		t.Fatalf("approval must be attempted before failing, submitted=%v", f.submitted)
	}
	if len(f.createdSummary) != 1 || !strings.Contains(f.createdSummary[0], "✅") {
		t.Fatalf("LGTM summary must still publish, got %v", f.createdSummary)
	}
	// The created pending review is cleaned up on submit refusal, mirroring
	// the findings path: a persistent refusal must not accumulate orphans.
	if len(f.deletedReviews) != 1 || f.deletedReviews[0] != 101 {
		t.Fatalf("pending review must be deleted after submit failure, got %v", f.deletedReviews)
	}
}

func TestPublishNoCommentsCleansStalePending(t *testing.T) {
	// Idempotency contract: stale pending bot reviews are deleted before
	// retrying — also on clean runs, which create a pending approval of
	// their own. Interrupted clean runs otherwise leave orphans forever
	// (this branch returns before the findings-path cleanup runs).
	f := newFake()
	f.headSHA = "head123"
	f.reviews = []poster.ReviewInfo{
		{ID: 7, State: "PENDING", Reviewer: "ci-bot"},
		{ID: 8, State: "PENDING", Reviewer: "someone-else"},
	}
	stats, err := Publish(result(), f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.deletedReviews) != 1 || f.deletedReviews[0] != 7 {
		t.Fatalf("only own stale pending review deleted, got %v", f.deletedReviews)
	}
	if len(f.submitStates) != 1 || f.submitStates[0] != StateApproved {
		t.Fatalf("approval must still submit, got %v", f.submitStates)
	}
	if stats.State != StateApproved {
		t.Fatalf("stats.State = %q, want APPROVED", stats.State)
	}
}

func TestPublishFindingsSubmitRequestChanges(t *testing.T) {
	f := newFake()
	r := result(c("a.go", 1, 2, "low"), c("b.go", 1, 1, "high"))
	stats, err := Publish(r, f, Options{RunTag: "t", CommitID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.submitStates) != 1 || f.submitStates[0] != StateRequestChanges {
		t.Fatalf("submit states = %v, want [REQUEST_CHANGES]", f.submitStates)
	}
	if stats.State != StateRequestChanges {
		t.Fatalf("stats.State = %q, want REQUEST_CHANGES", stats.State)
	}
}

func TestPublishLowOnlySubmitsApproved(t *testing.T) {
	f := newFake()
	stats, err := Publish(result(c("a.go", 1, 2, "low")), f, Options{RunTag: "t", CommitID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.submitStates) != 1 || f.submitStates[0] != StateApproved {
		t.Fatalf("submit states = %v, want [APPROVED]", f.submitStates)
	}
	if stats.State != StateApproved {
		t.Fatalf("stats.State = %q, want APPROVED", stats.State)
	}
	if stats.Inline != 1 {
		t.Fatalf("inline comment must still post: %+v", stats)
	}
}

func TestPublishDefaultApproveBelowIsLow(t *testing.T) {
	// Zero-value Options must behave like the CLI default --approve-below low:
	// a low finding does not block, medium and above do.
	f := newFake()
	stats, err := Publish(result(c("a.go", 1, 1, "medium")), f, Options{RunTag: "t", CommitID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.State != StateRequestChanges {
		t.Fatalf("empty ApproveBelow must default to low: state = %q", stats.State)
	}
}

func TestPublishRoutedFindingRequestsChanges(t *testing.T) {
	// Routed findings still count: no inline review is created, the summary
	// posts, and the run requests changes.
	f := newFake()
	r := result(c("a.go", 1, 2, "medium"))
	stats, err := Publish(r, f, Options{
		RunTag: "t", CommitID: "s",
		Route: policy.Build("medium", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Routed != 1 || stats.Inline != 0 {
		t.Fatalf("comment must be routed only: %+v", stats)
	}
	if len(f.createdComments) != 0 {
		t.Fatalf("routed findings must not create a review, got %+v", f.createdComments)
	}
	if stats.State != StateRequestChanges {
		t.Fatalf("routed finding must still request changes: %q", stats.State)
	}
}

func TestPublishSkippedFindingRequestsChanges(t *testing.T) {
	// Incremental skips are presentation: the skipped finding still counts.
	f := newFake()
	f.reviews = []poster.ReviewInfo{{ID: 9, State: "COMMENT", Reviewer: "ci-bot"}}
	f.reviewComments[9] = []poster.ReviewCommentInfo{
		{Body: format.BotMarker + " old -->\n> File `a.go` lines 1-2", Path: "a.go", NewLine: 2},
	}
	stats, err := Publish(result(c("a.go", 1, 2, "high")), f, Options{RunTag: "t", CommitID: "s", Incremental: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 1 || stats.Inline != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.State != StateRequestChanges {
		t.Fatalf("skipped finding must still request changes: %q", stats.State)
	}
}

func TestPublishSubmitFailureRecordsComment(t *testing.T) {
	f := newFake()
	f.submitErr = fmt.Errorf("403: self-approval refused")
	stats, err := Publish(result(c("a.go", 1, 2, "low"), c("b.go", 1, 1, "high")), f, Options{RunTag: "t", CommitID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.State != StateComment {
		t.Fatalf("stats.State = %q, want COMMENT after submit failure", stats.State)
	}
	// The submit attempt still carried the computed state before failing.
	if len(f.submitStates) != 1 || f.submitStates[0] != StateRequestChanges {
		t.Fatalf("submit states = %v, want [REQUEST_CHANGES]", f.submitStates)
	}
	if len(f.deletedReviews) != 1 || f.deletedReviews[0] != 101 {
		t.Fatalf("pending review must be deleted, got %v", f.deletedReviews)
	}
	if !strings.Contains(f.editedSummary[55], "review submit failed") {
		t.Fatal("failed comments must be reported in the summary")
	}
}

func TestPublishCreateFailureRecordsComment(t *testing.T) {
	f := newFake()
	f.createErr = fmt.Errorf("422: commit sha does not exist")
	stats, err := Publish(result(c("a.go", 1, 2, "low")), f, Options{RunTag: "t", CommitID: "stale-sha"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.State != StateComment {
		t.Fatalf("stats.State = %q, want COMMENT after creation failure", stats.State)
	}
	if len(f.submitStates) != 0 {
		t.Fatalf("nothing may be submitted when creation failed: %v", f.submitStates)
	}
	if !strings.Contains(f.editedSummary[55], "stale-sha") {
		t.Fatal("failed comments must be reported in the summary")
	}
}

func TestPublishCommitUnresolvableRecordsComment(t *testing.T) {
	// Neither --commit-id nor the PR head resolve: no review is created and
	// the state is recorded as COMMENT.
	f := newFake()
	stats, err := Publish(result(c("a.go", 1, 2, "medium")), f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.State != StateComment {
		t.Fatalf("stats.State = %q, want COMMENT when commit cannot be resolved", stats.State)
	}
	if len(f.createdComments) != 0 || len(f.submitted) != 0 {
		t.Fatalf("no review may be created or submitted: %+v", f.createdComments)
	}
	if stats.Failed != 1 || !strings.Contains(f.editedSummary[55], "could not resolve reviewed commit") {
		t.Fatalf("comments must fail to the summary: %+v", stats)
	}
}

func TestDecideState(t *testing.T) {
	// Every scenario from specs/ocr-forgejo-review-state/spec.md's
	// "Review state decision" requirement plus threshold edge cases.
	tests := []struct {
		name         string
		comments     []review.Comment
		approveBelow string
		posted       bool
		want         string
	}{
		{"clean result approves", nil, "low", false, StateApproved},
		{"zero findings approve with posting success", nil, "low", true, StateApproved},
		{"low-only result approves", []review.Comment{c("a.go", 1, 1, "low"), c("b.go", 1, 2, "low")}, "low", true, StateApproved},
		{"empty threshold fails safe: low blocks", []review.Comment{c("a.go", 1, 1, "low")}, "", true, StateRequestChanges},
		{"finding requests changes", []review.Comment{c("a.go", 1, 1, "medium")}, "low", true, StateRequestChanges},
		{"finding at threshold does not block", []review.Comment{c("a.go", 1, 1, "medium")}, "medium", true, StateApproved},
		{"posted=false with findings comments", []review.Comment{c("a.go", 1, 1, "high")}, "low", false, StateComment},
		{"routed findings still count", []review.Comment{c("a.go", 1, 1, "medium")}, "low", true, StateRequestChanges},
		{"skipped findings still count", []review.Comment{c("a.go", 1, 1, "high")}, "low", true, StateRequestChanges},
		{"malformed threshold fails safe to comment", []review.Comment{c("a.go", 1, 1, "low")}, "banana", true, StateRequestChanges},
		{"malformed threshold with findings blocks", []review.Comment{c("a.go", 1, 1, "low")}, "banana", false, StateComment},
		{"unranked severity fails safe: empty blocks", []review.Comment{c("a.go", 1, 1, "")}, "low", true, StateRequestChanges},
		{"unranked severity fails safe: unknown blocks", []review.Comment{c("a.go", 1, 1, "banana")}, "low", false, StateComment},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decideState(result(tc.comments...), tc.approveBelow, tc.posted)
			if got != tc.want {
				t.Fatalf("decideState = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecideStateFailsSafeOnMalformedThreshold(t *testing.T) {
	// Malformed --approve-below must not approve anything but a comment-free
	// result: any known-severity comment blocks, even `low`.
	r := result(c("a.go", 1, 1, "low"))
	if got := decideState(r, "banana", true); got != StateRequestChanges {
		t.Fatalf("malformed threshold must not approve findings, got %q", got)
	}
	empty := decideState(result(), "banana", true)
	if empty != StateApproved {
		t.Fatalf("comment-free result must still approve, got %q", empty)
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

func TestSpanFromHistoryIgnoresContentWording(t *testing.T) {
	body := "Content mentioning lines 99-98 first.\n\n> File `a.go` lines 3-5\n"
	span := spanFromHistory(poster.ReviewCommentInfo{Body: body, Path: "a.go"})
	if span.Start != 3 || span.End != 5 {
		t.Fatalf("span = %+v, want 3-5", span)
	}
}

var diffFixture = []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,2 @@\n keep()\n-removed()\n+added()\n")

func suggestionResult() *review.Result {
	c := review.Comment{Path: "a.go", StartLine: 2, EndLine: 2, Content: "content a.go", Category: "bug", Severity: "low", ExistingCode: "removed()", SuggestionCode: "better()"}
	return result(c)
}

func TestPublishInlineSuggestionsValidSpan(t *testing.T) {
	f := newFake()
	f.diff = diffFixture
	stats, err := Publish(suggestionResult(), f, Options{RunTag: "t", CommitID: "s", Suggestions: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	body := f.createdComments[0].Body
	if !strings.Contains(body, "**Before:**") || !strings.Contains(body, "**After:**") ||
		!strings.Contains(body, "removed()") || !strings.Contains(body, "better()") {
		t.Fatalf("inline body missing suggestion blocks:\n%s", body)
	}
}

func TestPublishInlineSuggestionsAfterOnly(t *testing.T) {
	f := newFake()
	f.diff = diffFixture
	r := result(review.Comment{Path: "a.go", StartLine: 2, EndLine: 2, Content: "x", SuggestionCode: "better()"})
	if _, err := Publish(r, f, Options{RunTag: "t", CommitID: "s", Suggestions: true}); err != nil {
		t.Fatal(err)
	}
	body := f.createdComments[0].Body
	if strings.Contains(body, "**Before:**") || !strings.Contains(body, "**After:**") || !strings.Contains(body, "better()") {
		t.Fatalf("expected After-only block:\n%s", body)
	}
}

func TestPublishSuggestionsFailOpenWithoutDiff(t *testing.T) {
	f := newFake()
	f.getDiffErr = fmt.Errorf("boom")
	stats, err := Publish(suggestionResult(), f, Options{RunTag: "t", CommitID: "s", Suggestions: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 1 {
		t.Fatalf("fail-open must keep the comment: %+v", stats)
	}
	if !strings.Contains(f.createdComments[0].Body, "**After:**") {
		t.Fatalf("fail-open must include suggestions:\n%s", f.createdComments[0].Body)
	}
}

func TestPublishSuggestionsFailOpenUnparseableDiff(t *testing.T) {
	f := newFake()
	f.diff = []byte("not a diff")
	stats, err := Publish(suggestionResult(), f, Options{RunTag: "t", CommitID: "s", Suggestions: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 1 || !strings.Contains(f.createdComments[0].Body, "**After:**") {
		t.Fatalf("unparseable diff must fail open: %+v", stats)
	}
}

func TestPublishInvalidSpanSuggestionsInFallback(t *testing.T) {
	f := newFake()
	f.diff = diffFixture
	r := result(review.Comment{Path: "a.go", StartLine: 0, EndLine: 99, Content: "x", Category: "bug", Severity: "low", SuggestionCode: "better()"})
	stats, err := Publish(r, f, Options{RunTag: "t", CommitID: "s", Suggestions: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 0 || stats.Failed != 1 {
		t.Fatalf("invalid span must drop comment: %+v", stats)
	}
	final := f.editedSummary[55]
	if !strings.Contains(final, "out of diff") || !strings.Contains(final, "**After:**") {
		t.Fatalf("fallback must keep suggestion blocks:\n%s", final)
	}
}

func TestPublishSuggestionsDisabled(t *testing.T) {
	f := newFake()
	f.diff = diffFixture
	stats, err := Publish(suggestionResult(), f, Options{RunTag: "t", CommitID: "s", Suggestions: false})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if strings.Contains(f.createdComments[0].Body, "**After:**") {
		t.Fatalf("suggestions off must omit blocks inline:\n%s", f.createdComments[0].Body)
	}
	// Summary fallback (no line info) must also omit blocks.
	r := result(review.Comment{Path: "a.go", Content: "no lines", Category: "bug", Severity: "low", SuggestionCode: "better()"})
	f2 := newFake()
	if _, err := Publish(r, f2, Options{RunTag: "t", CommitID: "s", Suggestions: false}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f2.editedSummary[55], "**After:**") || strings.Contains(f2.editedSummary[55], "<details>") {
		t.Fatalf("suggestions off must omit blocks in fallback:\n%s", f2.editedSummary[55])
	}
}

func TestPublishSuggestionsTrueWithoutSuggestionCode(t *testing.T) {
	f := newFake()
	f.diff = diffFixture
	r := result(review.Comment{Path: "a.go", StartLine: 2, EndLine: 2, Content: "x", Category: "bug", Severity: "low"})
	stats, err := Publish(r, f, Options{RunTag: "t", CommitID: "s", Suggestions: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Inline != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	body := f.createdComments[0].Body
	if strings.Contains(body, "**After:**") || strings.Contains(body, "**Before:**") || strings.Contains(body, "<details>") {
		t.Fatalf("comment without suggestion_code must not gain empty blocks:\n%s", body)
	}
}

func TestPublishFailedRunNeverApproves(t *testing.T) {
	f := newFake()
	f.headSHA = "head123"
	f.reviews = []poster.ReviewInfo{{ID: 7, State: "PENDING", Reviewer: "ci-bot"}}
	stats, err := Publish(&review.Result{Status: "failed", Message: "run crashed"}, f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.deletedReviews) != 1 || f.deletedReviews[0] != 7 {
		t.Fatalf("stale pending cleanup must still run, got %v", f.deletedReviews)
	}
	// Exactly one review, submitted as COMMENT — never APPROVED.
	if len(f.submitStates) != 1 || f.submitStates[0] != StateComment {
		t.Fatalf("submit states = %v, want [COMMENT]", f.submitStates)
	}
	if !strings.Contains(f.submitBody, "⚠️") || !strings.Contains(f.submitBody, "run crashed") {
		t.Fatalf("note review must carry the failure note, got %q", f.submitBody)
	}
	if len(f.createdSummary) != 1 || !strings.Contains(f.createdSummary[0], "⚠️") {
		t.Fatalf("summary must show the failure note, got %v", f.createdSummary)
	}
	if strings.Contains(f.createdSummary[0], "✅") {
		t.Fatalf("failed run must not publish the LGTM body, got %q", f.createdSummary[0])
	}
	if stats.State != StateComment || stats.RunStatus != "failed" || stats.Total != 0 || stats.SummaryURL != "comment:55" {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestPublishFailedRunWithFindingsNeverApproves(t *testing.T) {
	f := newFake()
	f.headSHA = "head123"
	r := &review.Result{
		Status:   "failed",
		Message:  "run crashed",
		Comments: []review.Comment{c("a.go", 1, 3, "critical")},
	}
	stats, err := Publish(r, f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.submitStates) != 1 || f.submitStates[0] != StateComment {
		t.Fatalf("submit states = %v, want [COMMENT]", f.submitStates)
	}
	if len(f.createdComments) != 0 {
		t.Fatalf("failed run must not post anchored inline comments, got %v", f.createdComments)
	}
	if !strings.Contains(f.createdSummary[0], "`a.go`") {
		t.Fatalf("findings must land informationally in the failure-note summary, got %q", f.createdSummary[0])
	}
	if stats.Inline != 0 || stats.State != StateComment || stats.RunStatus != "failed" {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestPublishFailedRunKeepsResolutionLedgerOpen(t *testing.T) {
	prior := &format.Ledger{
		Version: format.LedgerVersion,
		Open:    []format.LedgerEntry{{Path: "a.go", StartLine: 10, EndLine: 10, FirstSeen: "run-1", Content: "content a.go"}},
	}
	f := newFake()
	f.issueComments = []poster.IssueCommentInfo{{ID: 31, Body: format.SummaryMarker + "\nold body\n" + format.EncodeLedger(prior)}}
	stats, err := Publish(&review.Result{Status: "failed"}, f, Options{RunTag: "run-2", CommitID: "s", Sticky: true, ResolutionTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Resolved != 0 {
		t.Fatalf("failed run must not resolve open entries, got %d", stats.Resolved)
	}
	body, ok := f.editedSummary[31]
	if !ok {
		t.Fatalf("failure note must upsert the existing sticky summary, created %v", f.createdSummary)
	}
	_, l, found, err := format.StripLedger(body)
	if err != nil || !found {
		t.Fatalf("summary must keep the prior ledger block: found=%v err=%v", found, err)
	}
	if len(l.Open) != 1 || l.Open[0].Path != "a.go" || l.Open[0].FirstSeen != "run-1" {
		t.Fatalf("open entries must remain open unchanged, got %+v", l)
	}
	if len(l.Resolved) != 0 {
		t.Fatalf("failed run must not add resolved entries, got %+v", l.Resolved)
	}
	if !strings.Contains(body, "⚠️") {
		t.Fatalf("summary must show the failure note, got %q", body)
	}
}

func TestPublishFailedRunUpsertsOneSummary(t *testing.T) {
	f := newFake()
	f.headSHA = "head123"
	existing := format.WrapSummaryBody(format.BuildFailureBody("run crashed", nil), "t")
	f.issueComments = []poster.IssueCommentInfo{{ID: 31, Body: existing}}
	if _, err := Publish(&review.Result{Status: "failed", Message: "run crashed"}, f, Options{RunTag: "t", Sticky: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.createdSummary) != 0 {
		t.Fatalf("rerun must upsert the existing summary, created %v", f.createdSummary)
	}
	if body := f.editedSummary[31]; !strings.Contains(body, "run crashed") || !strings.Contains(body, "⚠️") || strings.Contains(body, "✅") {
		t.Fatalf("summary must keep the failure note (never LGTM), got %q", body)
	}
}

// TestPublishReportedFailedRunFixture exercises the exact user-reported
// failed-run JSON shared with internal/review's fixture so the reported
// case stays reproducible end to end.
func TestPublishReportedFailedRunFixture(t *testing.T) {
	r, err := review.Load(filepath.Join("..", "review", "testdata", "failed-run.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	f := newFake()
	f.headSHA = "head123"
	stats, err := Publish(r, f, Options{RunTag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.submitStates) != 1 || f.submitStates[0] != StateComment {
		t.Fatalf("submit states = %v, want [COMMENT]", f.submitStates)
	}
	if !strings.Contains(f.createdSummary[0], "⚠️ **OpenCodeReview**:") {
		t.Fatalf("summary must carry the failure note, got %q", f.createdSummary[0])
	}
	if strings.Contains(f.createdSummary[0], "✅") {
		t.Fatalf("failed run must not publish the LGTM body, got %q", f.createdSummary[0])
	}
	if stats.RunStatus != "failed" || stats.State != StateComment || stats.Total != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestPublishFailedRunDryRunPrintsNoteWithoutWrites(t *testing.T) {
	r := &review.Result{Status: "failed", Message: "run crashed"}
	old := os.Stdout
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wr
	stats, err := Publish(r, poster.NewDryRun(), Options{RunTag: "t"})
	os.Stdout = old
	if err := wr.Close(); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rd); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "--- dry-run summary comment ---") || !strings.Contains(out, "⚠️ **OpenCodeReview**: run crashed") {
		t.Fatalf("dry run must print the failure note, got %q", out)
	}
	if stats.RunStatus != "failed" || stats.State != StateComment {
		t.Fatalf("stats = %+v", stats)
	}
}
