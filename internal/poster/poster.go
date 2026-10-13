// Package poster abstracts the Forgejo transport used to publish reviews,
// with retry/backoff and a dry-run implementation.
package poster

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	forgejo "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"
)

const (
	maxRetries = 3
	// retryBaseDelay is the base for exponential backoff.
	retryBaseDelay = 2 * time.Second
	// maxBackoff caps a single backoff delay before jitter.
	maxBackoff = 60 * time.Second
)

var errInvalidConfig = errors.New("poster: forgejo URL, owner, repo and PR index are required")

// ReviewCommentInput is one inline review comment to create.
type ReviewCommentInput struct {
	Path    string
	Body    string
	NewLine int64
}

// ReviewInfo summarizes a pull review.
type ReviewInfo struct {
	ID       int64
	State    string
	Reviewer string
}

// ReviewCommentInfo summarizes an existing review comment.
type ReviewCommentInfo struct {
	Body    string
	Path    string
	NewLine int64
}

// IssueCommentInfo summarizes an issue (PR) comment.
type IssueCommentInfo struct {
	ID   int64
	Body string
}

// PullRequestInfo summarizes a pull request.
type PullRequestInfo struct {
	HeadSHA string
	HeadRef string
}

// Poster is the transport used to publish an OCR review.
type Poster interface {
	CreatePullReview(comments []ReviewCommentInput, commitID string) (int64, error)
	SubmitPullReview(reviewID int64, body string) error
	DeletePullReview(reviewID int64) error
	ListPullReviews() ([]ReviewInfo, error)
	ListPullReviewComments(reviewID int64) ([]ReviewCommentInfo, error)
	ListIssueComments() ([]IssueCommentInfo, error)
	CreateIssueComment(body string) (int64, error)
	EditIssueComment(commentID int64, body string) error
	GetDiff() ([]byte, error)
	GetPullRequest() (*PullRequestInfo, error)
	// WhoAmI returns the authenticated user's login.
	WhoAmI() (string, error)
}

// ForgejoPoster implements Poster over the forgejo SDK with retry/backoff.
type ForgejoPoster struct {
	client *forgejo.Client
	owner  string
	repo   string
	pr     int64
	sleep  func(time.Duration)
}

// NewForgejo builds a ForgejoPoster. sleep is injectable for tests; nil uses
// time.Sleep.
func NewForgejo(url, owner, repo string, prIndex int64, token string, sleep func(time.Duration)) (*ForgejoPoster, error) {
	if url == "" || owner == "" || repo == "" || prIndex <= 0 {
		return nil, errInvalidConfig
	}
	client, err := forgejo.NewClient(url,
		forgejo.SetToken(token),
		forgejo.SetContext(context.Background()),
		forgejo.SetForgejoVersion("1.22.0"),
	)
	if err != nil {
		return nil, fmt.Errorf("poster: create client: %w", err)
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	return &ForgejoPoster{client: client, owner: owner, repo: repo, pr: prIndex, sleep: sleep}, nil
}

// retryResult carries the last response so headers can be inspected.
type retryResult struct {
	resp *forgejo.Response
}

// do runs fn with retry on 429/5xx/network errors. fn may inspect the last
// response through the passed retryResult pointer.
func (p *ForgejoPoster) do(operation string, rr *retryResult, fn func() (*forgejo.Response, error)) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		resp, err := fn()
		if rr != nil {
			rr.resp = resp
		}
		if err == nil {
			return nil
		}
		lastErr = err
		status, header := statusOf(resp)
		retryAfter := retryAfterOf(header)
		if !retryable(status, err) || attempt >= maxRetries {
			return fmt.Errorf("poster: %s: %w", operation, lastErr)
		}
		p.sleep(backoffDelay(attempt, retryAfter))
	}
}

func statusOf(resp *forgejo.Response) (int, http.Header) {
	if resp == nil || resp.Response == nil {
		return 0, nil
	}
	return resp.StatusCode, resp.Header
}

func retryAfterOf(header http.Header) time.Duration {
	if header == nil {
		return 0
	}
	if v := header.Get("Retry-After"); v != "" {
		var secs float64
		if _, err := fmt.Sscanf(v, "%f", &secs); err == nil && secs >= 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 0
}

func retryable(status int, err error) bool {
	if err != nil && status == 0 {
		// Network-level failure (no HTTP status available).
		return true
	}
	return status == http.StatusTooManyRequests || status >= 500
}

// backoffDelay computes the next retry delay: Retry-After when present,
// otherwise exponential backoff, capped at maxBackoff, then widened by ±25%
// jitter.
func backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	base := retryAfter
	if base <= 0 {
		base = retryBaseDelay << attempt
	}
	if base > maxBackoff {
		base = maxBackoff
	}
	jitter := 0.75 + rand.Float64()*0.5
	return time.Duration(float64(base) * jitter)
}

// CreatePullReview creates a pending review with the given inline comments.
func (p *ForgejoPoster) CreatePullReview(comments []ReviewCommentInput, commitID string) (int64, error) {
	opts := forgejo.CreatePullReviewOptions{
		State:    forgejo.ReviewStatePending,
		Body:     "🔍 OpenCodeReview: automated review in progress",
		CommitID: commitID,
	}
	for _, c := range comments {
		opts.Comments = append(opts.Comments, forgejo.CreatePullReviewComment{
			Path:       c.Path,
			Body:       c.Body,
			NewLineNum: c.NewLine,
		})
	}
	var reviewID int64
	rr := &retryResult{}
	err := p.do("create review", rr, func() (*forgejo.Response, error) {
		review, resp, err := p.client.CreatePullReview(p.owner, p.repo, p.pr, opts)
		if err == nil {
			reviewID = review.ID
		}
		return resp, err
	})
	return reviewID, err
}

// SubmitPullReview submits a pending review with the given event and body.
func (p *ForgejoPoster) SubmitPullReview(reviewID int64, body string) error {
	opts := forgejo.SubmitPullReviewOptions{State: forgejo.ReviewStateComment, Body: body}
	return p.do("submit review", nil, func() (*forgejo.Response, error) {
		_, resp, err := p.client.SubmitPullReview(p.owner, p.repo, p.pr, reviewID, opts)
		return resp, err
	})
}

// DeletePullReview deletes an unsubmitted review.
func (p *ForgejoPoster) DeletePullReview(reviewID int64) error {
	return p.do("delete review", nil, func() (*forgejo.Response, error) {
		return p.client.DeletePullReview(p.owner, p.repo, p.pr, reviewID)
	})
}

// ListPullReviews returns all reviews on the PR.
func (p *ForgejoPoster) ListPullReviews() ([]ReviewInfo, error) {
	var out []ReviewInfo
	rr := &retryResult{}
	err := p.do("list reviews", rr, func() (*forgejo.Response, error) {
		reviews, resp, err := p.client.ListPullReviews(p.owner, p.repo, p.pr, forgejo.ListPullReviewsOptions{
			ListOptions: forgejo.ListOptions{Page: 1, PageSize: 50},
		})
		if err == nil {
			out = out[:0]
			for _, r := range reviews {
				info := ReviewInfo{ID: r.ID, State: string(r.State)}
				if r.Reviewer != nil {
					info.Reviewer = r.Reviewer.UserName
				}
				out = append(out, info)
			}
		}
		return resp, err
	})
	return out, err
}

// ListPullReviewComments returns the comments of one review.
func (p *ForgejoPoster) ListPullReviewComments(reviewID int64) ([]ReviewCommentInfo, error) {
	var out []ReviewCommentInfo
	rr := &retryResult{}
	err := p.do("list review comments", rr, func() (*forgejo.Response, error) {
		comments, resp, err := p.client.ListPullReviewComments(p.owner, p.repo, p.pr, reviewID)
		if err == nil {
			out = out[:0]
			for _, c := range comments {
				out = append(out, ReviewCommentInfo{Body: c.Body, Path: c.Path, NewLine: int64(c.LineNum)})
			}
		}
		return resp, err
	})
	return out, err
}

// ListIssueComments returns all comments on the PR (PRs are issues).
func (p *ForgejoPoster) ListIssueComments() ([]IssueCommentInfo, error) {
	var out []IssueCommentInfo
	rr := &retryResult{}
	err := p.do("list issue comments", rr, func() (*forgejo.Response, error) {
		comments, resp, err := p.client.ListIssueComments(p.owner, p.repo, p.pr, forgejo.ListIssueCommentOptions{
			ListOptions: forgejo.ListOptions{Page: 1, PageSize: 50},
		})
		if err == nil {
			out = out[:0]
			for _, c := range comments {
				out = append(out, IssueCommentInfo{ID: c.ID, Body: c.Body})
			}
		}
		return resp, err
	})
	return out, err
}

// CreateIssueComment creates a comment on the PR and returns its ID.
func (p *ForgejoPoster) CreateIssueComment(body string) (int64, error) {
	var id int64
	rr := &retryResult{}
	err := p.do("create issue comment", rr, func() (*forgejo.Response, error) {
		comment, resp, err := p.client.CreateIssueComment(p.owner, p.repo, p.pr, forgejo.CreateIssueCommentOption{Body: body})
		if err == nil {
			id = comment.ID
		}
		return resp, err
	})
	return id, err
}

// EditIssueComment updates the body of an existing PR comment.
func (p *ForgejoPoster) EditIssueComment(commentID int64, body string) error {
	return p.do("edit issue comment", nil, func() (*forgejo.Response, error) {
		_, resp, err := p.client.EditIssueComment(p.owner, p.repo, commentID, forgejo.EditIssueCommentOption{Body: body})
		return resp, err
	})
}

// GetDiff returns the PR's unified diff.
func (p *ForgejoPoster) GetDiff() ([]byte, error) {
	var diff []byte
	rr := &retryResult{}
	err := p.do("get diff", rr, func() (*forgejo.Response, error) {
		var resp *forgejo.Response
		var err error
		diff, resp, err = p.client.GetPullRequestDiff(p.owner, p.repo, p.pr, forgejo.PullRequestDiffOptions{})
		return resp, err
	})
	return diff, err
}

// GetPullRequest returns PR metadata needed for commit binding and run tags.
func (p *ForgejoPoster) GetPullRequest() (*PullRequestInfo, error) {
	var info *PullRequestInfo
	rr := &retryResult{}
	err := p.do("get pull request", rr, func() (*forgejo.Response, error) {
		pr, resp, err := p.client.GetPullRequest(p.owner, p.repo, p.pr)
		if err == nil && pr != nil && pr.Head != nil {
			info = &PullRequestInfo{HeadSHA: pr.Head.Sha, HeadRef: pr.Head.Ref}
		}
		return resp, err
	})
	return info, err
}

// WhoAmI returns the authenticated user's login.
func (p *ForgejoPoster) WhoAmI() (string, error) {
	var login string
	rr := &retryResult{}
	err := p.do("whoami", rr, func() (*forgejo.Response, error) {
		user, resp, err := p.client.GetMyUserInfo()
		if err == nil && user != nil {
			login = user.UserName
		}
		return resp, err
	})
	return login, err
}

// DryRunPoster prints instead of touching the network.
type DryRunPoster struct{}

// NewDryRun returns a DryRunPoster.
func NewDryRun() *DryRunPoster { return &DryRunPoster{} }

// CreatePullReview prints what would be posted.
func (DryRunPoster) CreatePullReview(comments []ReviewCommentInput, commitID string) (int64, error) {
	for _, c := range comments {
		fmt.Printf("--- dry-run review comment [%s:%d] ---\n%s\n\n", c.Path, c.NewLine, c.Body)
	}
	return 1, nil
}

// SubmitPullReview prints the review body.
func (DryRunPoster) SubmitPullReview(_ int64, body string) error {
	fmt.Printf("--- dry-run review submit ---\n%s\n", body)
	return nil
}

// DeletePullReview prints the pending review that would be deleted.
func (DryRunPoster) DeletePullReview(reviewID int64) error {
	fmt.Printf("--- dry-run delete pending review %d ---\n", reviewID)
	return nil
}

// ListPullReviews returns nothing.
func (DryRunPoster) ListPullReviews() ([]ReviewInfo, error) { return nil, nil }

// ListPullReviewComments returns nothing.
func (DryRunPoster) ListPullReviewComments(int64) ([]ReviewCommentInfo, error) { return nil, nil }

// ListIssueComments returns nothing.
func (DryRunPoster) ListIssueComments() ([]IssueCommentInfo, error) { return nil, nil }

// CreateIssueComment prints the summary that would be posted.
func (DryRunPoster) CreateIssueComment(body string) (int64, error) {
	fmt.Printf("--- dry-run summary comment ---\n%s\n", body)
	return 1, nil
}

// EditIssueComment prints the summary that would be updated.
func (DryRunPoster) EditIssueComment(commentID int64, body string) error {
	fmt.Printf("--- dry-run summary update [%d] ---\n%s\n", commentID, body)
	return nil
}

// GetDiff returns an empty diff.
func (DryRunPoster) GetDiff() ([]byte, error) { return nil, nil }

// GetPullRequest returns unknown metadata.
func (DryRunPoster) GetPullRequest() (*PullRequestInfo, error) {
	return &PullRequestInfo{}, nil
}

// WhoAmI returns a placeholder identity.
func (DryRunPoster) WhoAmI() (string, error) { return "dry-run-bot", nil }
