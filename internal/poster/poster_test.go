package poster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// newTestForgejo builds a ForgejoPoster against a test server with injected
// sleep so backoff does not slow tests.
func newTestForgejo(t *testing.T, handler http.HandlerFunc) (*ForgejoPoster, *sleepRecorder) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	rec := &sleepRecorder{}
	p, err := NewForgejo(context.Background(), srv.URL, "owner", "repo", 42, "tok", rec.sleep)
	if err != nil {
		t.Fatal(err)
	}
	return p, rec
}

type sleepRecorder struct {
	calls []time.Duration
}

func (s *sleepRecorder) sleep(d time.Duration) {
	s.calls = append(s.calls, d)
}

func TestRetryOn500ThenSuccess(t *testing.T) {
	attempts := 0
	p, rec := newTestForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, `{"head": {"sha": "abc123", "ref": "main"}}`)
	})
	if _, err := p.GetPullRequest(); err != nil {
		t.Fatalf("expected success on third attempt, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("expected 2 backoff sleeps, got %v", rec.calls)
	}
}

func TestNoRetryOn400(t *testing.T) {
	attempts := 0
	p, rec := newTestForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"message": "bad"}`)
	})
	_, err := p.GetPullRequest()
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if attempts != 1 {
		t.Fatalf("400 must not be retried, attempts = %d", attempts)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("no sleeps expected on 400, got %v", rec.calls)
	}
}

func TestRetryOn429(t *testing.T) {
	attempts := 0
	p, rec := newTestForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := p.GetPullRequest(); err == nil {
		t.Fatal("expected error when every attempt 429s")
	}
	if attempts != maxRetries+1 {
		t.Fatalf("attempts = %d, want %d", attempts, maxRetries+1)
	}
	if len(rec.calls) != maxRetries {
		t.Fatalf("expected %d sleeps, got %v", maxRetries, rec.calls)
	}
}

func TestRetryOnNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // connection refused
	rec := &sleepRecorder{}
	p, err := NewForgejo(context.Background(), srv.URL, "owner", "repo", 42, "tok", rec.sleep)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetPullRequest(); err == nil {
		t.Fatal("expected network error")
	}
	if len(rec.calls) != maxRetries {
		t.Fatalf("network errors must be retried, sleeps = %v", rec.calls)
	}
}

func TestBackoffCap(t *testing.T) {
	got := backoffDelay(10, time.Minute)
	if got <= 0 || got > time.Duration(float64(maxBackoff)*1.25) { // cap applied before ±25% jitter widening
		t.Fatalf("delay %v should be at most the capped base widened by jitter", got)
	}
}

func TestDryRunPostsNothing(t *testing.T) {
	p := NewDryRun()
	if _, err := p.CreatePullReview([]ReviewCommentInput{{Path: "a.go", Body: "b", NewLine: 5}}, "sha"); err != nil {
		t.Fatal(err)
	}
	if err := p.SubmitPullReview(1, "APPROVED", "body"); err != nil {
		t.Fatal(err)
	}
	if err := p.DeletePullReview(1); err != nil {
		t.Fatal(err)
	}
	reviews, err := p.ListPullReviews()
	if err != nil || len(reviews) != 0 {
		t.Fatalf("dry-run lists nothing: %v %v", reviews, err)
	}
	if comments, err := p.ListPullReviewComments(1); err != nil || len(comments) != 0 {
		t.Fatalf("dry-run lists no comments: %v %v", comments, err)
	}
	if comments, err := p.ListIssueComments(); err != nil || len(comments) != 0 {
		t.Fatalf("dry-run lists no issue comments: %v %v", comments, err)
	}
	if _, err := p.GetDiff(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetPullRequest(); err != nil {
		t.Fatal(err)
	}
}

func TestForgejoIssueComments(t *testing.T) {
	var id int64 = 7
	p, _ := newTestForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/owner/repo/issues/42/comments":
			_, _ = fmt.Fprintf(w, `{"id": %d, "body": "x"}`, id)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/owner/repo/issues/comments/7":
			_, _ = fmt.Fprint(w, `{"id": 7, "body": "y"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/owner/repo/issues/42/comments":
			_, _ = fmt.Fprint(w, `[{"id": 7, "body": "y"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cid, err := p.CreateIssueComment("x")
	if err != nil || cid != id {
		t.Fatalf("CreateIssueComment = %d, %v", cid, err)
	}
	if err := p.EditIssueComment(7, "y"); err != nil {
		t.Fatal(err)
	}
	comments, err := p.ListIssueComments()
	if err != nil || len(comments) != 1 || comments[0].ID != 7 || comments[0].Body != "y" {
		t.Fatalf("ListIssueComments = %+v, %v", comments, err)
	}
}

func TestForgejoReviewFlow(t *testing.T) {
	p, _ := newTestForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/owner/repo/pulls/42/reviews":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"body":"`) || strings.Contains(string(body), `"body":""`) {
				t.Errorf("PENDING creation requires a non-empty body, got %s", body)
			}
			_, _ = fmt.Fprint(w, `{"id": 5, "state": "PENDING"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/owner/repo/pulls/42/reviews/5":
			sbody, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(sbody), `"event":"REQUEST_CHANGES"`) {
				t.Errorf("submit must carry the requested event, got %s", sbody)
			}
			_, _ = fmt.Fprint(w, `{"id": 5, "state": "COMMENT"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/owner/repo/pulls/42/reviews/5":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/owner/repo/pulls/42/reviews":
			_, _ = fmt.Fprint(w, `[{"id": 5, "state": "PENDING", "user": {"login": "bot"}}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/owner/repo/pulls/42/reviews/5/comments":
			_, _ = fmt.Fprint(w, `[{"id": 1, "body": "<!-- ocr-1-a -->", "path": "a.go", "position": 5}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	rid, err := p.CreatePullReview([]ReviewCommentInput{{Path: "a.go", Body: "b", NewLine: 5}}, "sha")
	if err != nil || rid != 5 {
		t.Fatalf("CreatePullReview = %d, %v", rid, err)
	}
	if err := p.SubmitPullReview(5, "REQUEST_CHANGES", "body"); err != nil {
		t.Fatal(err)
	}
	if err := p.DeletePullReview(5); err != nil {
		t.Fatal(err)
	}
	reviews, err := p.ListPullReviews()
	if err != nil || len(reviews) != 1 || reviews[0].State != "PENDING" || reviews[0].Reviewer != "bot" {
		t.Fatalf("ListPullReviews = %+v, %v", reviews, err)
	}
	comments, err := p.ListPullReviewComments(5)
	if err != nil || len(comments) != 1 || comments[0].Path != "a.go" || comments[0].NewLine != 5 {
		t.Fatalf("ListPullReviewComments = %+v, %v", comments, err)
	}
}

func TestForgejoGetDiffAndPR(t *testing.T) {
	p, _ := newTestForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/owner/repo/pulls/42.diff":
			_, _ = fmt.Fprint(w, "diff --git a/a.go b/a.go")
		case "/api/v1/repos/owner/repo/pulls/42":
			_, _ = fmt.Fprint(w, `{"head": {"sha": "abc123", "ref": "feature"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	diff, err := p.GetDiff()
	if err != nil || string(diff) != "diff --git a/a.go b/a.go" {
		t.Fatalf("GetDiff = %q, %v", diff, err)
	}
	pr, err := p.GetPullRequest()
	if err != nil || pr.HeadSHA != "abc123" {
		t.Fatalf("GetPullRequest = %+v, %v", pr, err)
	}
}

func TestNewForgejoRequiresURL(t *testing.T) {
	if _, err := NewForgejo(context.Background(), "", "o", "r", 1, "t", nil); !errors.Is(err, errInvalidConfig) {
		t.Fatalf("expected errInvalidConfig, got %v", err)
	}
}

func TestDryRunSubmitPrintsState(t *testing.T) {
	p := NewDryRun()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	if err := p.SubmitPullReview(1, "APPROVED", "body"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(r)
	if !strings.Contains(string(out), "APPROVED") {
		t.Fatalf("dry-run submit must print the state, got %q", out)
	}
}
