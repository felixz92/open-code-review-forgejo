# Design

## Context

`ocr` (alibaba/open-code-review) emits review results as JSON but has no Forgejo publishing glue; the GitLab example (`examples/gitlab_ci/post_review.py`, ~1650 lines) is the reference behavior. The target runtime is Argo Workflows, which injects `GIT_TOKEN`/`GIT_USER`. Forgejo exposes the Gitea-compatible API, consumed via `codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3`. See proposal.md - Why for motivation.

## Goals / Non-Goals

**Goals:**
- Single static Go binary, standard library + forgejo-sdk only.
- Pure logic (formatting, policy, incremental, diff parsing) separated from transport so everything is unit-testable without network or wall-clock sleeps (the Python script's Poster pattern).
- Full behavioral port of the GitLab script minus GitLab-specific mechanics.

**Non-Goals:**
- Forgejo Actions support or CI-env auto-detection.
- GitLab/GitHub/Gerrit targets.
- Rate-limit wait-until-reset and read-pacing machinery (Forgejo has no rate limits by default).
- Multiline line-range anchoring (platform limit; anchor at end_line).

## Decisions

### D1: One review per run (not per-comment)
`CreatePullReview` with all inline comments + `SubmitPullReview(event=COMMENT)`.
- Why: Forgejo's review primitive makes a batch atomic; retry is trivially idempotent (delete the stale pending review, recreate). Per-comment reviews would produce N notifications and N pending reviews and buy nothing since Forgejo has no per-discussion positions anyway.
- Alternative considered: literal GitLab port (one review per comment) - rejected as above.
- Recorded as ADR: `docs/adr/0001-single-review-posting.md`.

### D2: Line anchoring
`CreatePullReviewComment{Path, Body, NewLineNum: end_line}`. Multiline spans (start 10, end 14) anchor at `end_line` and state the span in the body. Same as the GitLab single-line fallback.

### D3: Commit binding
`CreatePullReviewOptions.CommitID` = `manifest.input.resolved_head`, falling back to the PR head SHA from `GetPullRequest`. This replaces the GitLab `/versions` SHA-matching logic: a force-push after OCR ran fails review creation against the stale SHA, and the failed comments land in the summary with a stated reason.

### D4: Idempotency
- Before creating: `ListPullReviews`, delete prior **pending** reviews authored by the bot (delete-stale-pending cleanup).
- A failed submit leaves a pending review; it is deleted and the batch retried whole.
- Per-comment `<!-- ocr-<runtag>-<rand> -->` id tags kept for incremental history detection.
- Run tag: derived from PR index + head SHA (no CI env available); `--run-tag` optional override.

### D5: Sticky summary
PR == issue: `ListIssueComments` / `CreateIssueComment` / `EditIssueComment` with `SUMMARY_MARKER = "<!-- ocr-summary -->"` plus per-run `<!-- ocr-summary-run:TAG -->`. Anchor ("posting...") body before inline posting, final body after - port of the Python anchor/finalize phases. Review body stays empty/one-liner so the summary is the only summary surface.

### D6: Diff pre-check instead of post-hoc 400 fallback
GitLab's script classifies 400 errors after a failed discussion post. Forgejo (Gitea) rejects invalid `new_position` values with 422 on review submission - a failed submit loses the whole batch. So the diff-inventory classification runs **before** submitting: fetch `GetPullRequestDiff` (raw unified diff), parse per-file new-line ranges, classify valid/invalid/unknown, drop provably-out-of-diff comments. `ListPullRequestFiles` is the fallback if the binary diff/whitespace options complicate parsing.

### D7: Suggestions
GitLab's `suggestion:-N+0` blocks render an Apply button Forgejo does not support. Port as plain fenced Before/After blocks (safe fence length).

### D8: Transport
Drop wait-until-reset and read-pacing (Forgejo has no rate limits by default). Keep simple retry with exponential backoff + jitter on 429/5xx/network errors (default 3 attempts). `Poster` interface over the SDK with a `DryRunPoster` for `--dry-run`.

## Package layout

```
cmd/ocr-forgejo/main.go        flag parsing, wiring, exit codes
internal/review/               OCR result JSON structs + loader (tolerant)
internal/format/               badges, comment bodies, fallback formatting,
                               fences, warnings, summary builders  (pure)
internal/policy/               build_policy / route_comment (pure)
internal/incremental/          comment_span, IoU overlap, history loading
internal/diffinv/              unified-diff hunk parsing + classification (pure)
internal/poster/               Poster interface + ForgejoPoster (SDK v3) + DryRunPoster
internal/publish/              orchestration: partition, sort, filter, pre-check,
                               post, summary, stats
docs/adr/0001-single-review-posting.md
```

## Risks / Trade-offs

- [SDK v3 API surface assumed identical in shape to v2 (review types, issue comments, PR diff)] → verify at implementation time against the v3 docs before writing `internal/poster`.
- [Empty-body review submit with only comments may be rejected] → if Forgejo requires a body, put a one-line summary in the review body.
- [`GetPullRequestDiff` returns a raw unified diff (bytes)] → sufficient for `diffinv`; fall back to `ListPullRequestFiles` if binary/whitespace options complicate parsing.
- [No CI env on Argo by default] → run tag derived from PR index + head SHA; `--run-tag` override available.

## Open Questions

(none - all decisions resolved during exploration)
