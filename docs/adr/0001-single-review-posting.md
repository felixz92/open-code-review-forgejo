# ADR 0001: Single Review Posting

## Status

Accepted

## Context

open-code-review (`ocr`) results must be published to Forgejo pull requests.
The reference implementation for GitLab (`post_review.py`) posts one positional
discussion per comment, then a sticky summary note. Forgejo's API (Gitea-compatible)
has no per-discussion positional model; it exposes a *pull review* primitive:

- `POST /repos/{owner}/{repo}/pulls/{index}/reviews` accepts a batch of review
  comments (`path`, `body`, `new_position`) plus a commit ID, created as a
  pending review and then submitted with an event (`COMMENT`, `APPROVED`, ...).
- `DELETE /reviews/{id}` removes a review that has not been submitted.

Two posting models were considered:

1. **Literal GitLab port** - one review per comment. Produces N notifications
   and N pending reviews, forfeits the atomicity of the review primitive, and
   buys nothing because Forgejo has no per-discussion line-range anchoring
   anyway.
2. **One review per run** - all anchorable comments posted as a single review,
   submitted with `COMMENT`.

## Decision

Post **one review per run**. All anchorable comments are batched into a single
`CreatePullReview` (pending) and submitted with `SubmitPullReview(event=COMMENT)`.
Unanchorable, policy-routed, and failed comments go to the sticky summary issue
comment, not the review.

## Consequences

- **Atomic submit**: a run produces exactly one review and one notification on
  the PR.
- **Idempotent retries**: a failed submit leaves a pending review, which is
  deleted and the batch recreated wholesale; no per-comment reconciliation is
  needed (unlike GitLab's reconcile-before-retry). Stale pending reviews from
  interrupted runs are deleted before creating a new one.
- **No per-finding threads**: findings are not individually resolvable or
  replyable as their own discussion threads; replies happen on the review.
- **Multiline anchoring**: Forgejo review comments accept a single line
  position (`new_position`). Multiline spans anchor at `end_line` and state
  the span in the comment body.
- **Submit body**: `SubmitPullReview` requires a non-empty body for
  non-APPROVED events; the review body carries a one-line summary while the
  full summary lives in the sticky issue comment.
