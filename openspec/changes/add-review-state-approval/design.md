# Design

## Context

`internal/publish.Publish` already computes everything the state decision needs: the result JSON (`result.Comments` with severities), routing outcomes, skip/fail counts, and posting success. `internal/poster.ForgejoPoster.SubmitPullReview` hardcodes `forgejo.ReviewStateComment` (poster.go:223). The zero-comment path (publish.go:69-77) returns before any review API call. `--fail-on-severity` lives in `cmd/ocr-forgejo/main.go` and the exit-code logic around it.

## Goals / Non-Goals

**Goals:**

- One decision function: `(result JSON, threshold, posting outcome) -> state`, pure and unit-testable.
- Clean runs produce an `APPROVED` review in addition to the sticky summary.
- State refusal by Forgejo is logged and non-fatal.

**Non-Goals:**

- Any run-to-run memory for the decision (OCR JSON is the source of truth).
- Reusing `--route-severity-below` as the approval threshold; the two flags stay orthogonal (routing = where comments appear, approval = what blocks).
- Retry or fallback submissions when Forgejo refuses a state.

## Decisions

### 1. Decision function in `internal/publish`, not the CLI

`publish` owns the outcome signal, so it computes the state and returns it (add `State string` to `Stats`, e.g. `APPROVED`/`REQUEST_CHANGES`/`COMMENT`). The CLI only wires the threshold flag through `Options`. Alternative: decide in `main.go` from stats — rejected, stats are lossy (routed/skipped severities aren't in `Stats`).

Finding counting: iterate `result.Comments`, compare severity rank at-or-above the threshold using the existing `policy` severity ladder. Routing and incremental skipping never influence this count. Rationale: presentation must not change the verdict.

### 2. `SubmitPullReview` takes the state

Change the `Poster` interface signature to `SubmitPullReview(reviewID int64, state string, body string) error`; `ForgejoPoster` maps the string to `forgejo.ReviewState*`. `DryRunPoster` prints the state. Mock posters in tests update accordingly. Alternative: keep the signature and add a setter — rejected as hidden state.

### 3. Empty-review path on clean runs

The `total == 0` branch keeps the LGTM summary and additionally calls `CreatePullReview(nil, commitID)` + `SubmitPullReview(id, "APPROVED", ...)`. Commit resolution reuses the same fallback as the findings path (`--commit-id` from `manifest.input.resolved_head`, else PR head). If review creation fails (e.g. commit drift), log and still publish the summary — the clean-run summary is the primary contract, the approval is best-effort but logs loudly. Alternative: skip the review when commit can't be resolved — rejected; stale-SHA failure on a clean run is the same situation as today's summary-only run, just noisier in logs.

Low-severity-only runs keep today's flow: the review with inline/summary comments is created and submitted with `APPROVED` instead of `COMMENT` — no second review, just a different event.

### 4. Posting-failure fallback

`findings > 0`:
- Review created and submitted with `REQUEST_CHANGES` → done.
- Submission fails → existing behavior deletes the pending review and marks comments failed; then submit `COMMENT`. If creation itself failed (nothing to submit), no review — state `COMMENT` recorded in stats, failures reported in the summary as today.

Partial failure (some comments posted, submit failed) deletes the pending review wholesale, so `COMMENT` on nothing is honest.

### 5. State refusal handling

The Forgejo SDK surfaces refusal as an HTTP error from `SubmitPullReview`. We cannot distinguish "self-approval refused" from other 4xx without response-body sniffing; treat any submit error as posting failure per Decision 4, logging the error. `APPROVED` refusal therefore logs and the run completes with `COMMENT`-equivalent outcome. Alternative: parse error bodies for a dedicated refusal path — rejected as brittle across Forgejo versions; log-and-continue is the spec.

### 6. Remove `--fail-on-severity`

Delete the flag, `Config.FailOnSeverity`, the exit-code gate, and its tests; the spec's gating clause is REMOVED in the delta. Non-zero exits remain for genuine errors (parse failure, missing flags, network failure of the run).

### 7. Threshold flag parsing

Reuse `policy.Build`'s severity-rank parsing. New config field `ApproveBelow` defaulting to `"low"`; unknown value maps to rank 0 (`low`-only approves nothing above... i.e. fail-safe: only zero-comment runs approve). Note the asymmetry with routing's fail-open: approval is a mergeability signal, so a typo must not silently approve a broken PR.

## Risks / Trade-offs

- [Bot approvals not counted by branch protection, or self-approval refused] → log-and-continue; `COMMENT` fallback keeps comments intact. Documented in spec.
- [Argo users lose the `--fail-on-severity` exit signal] → BREAKING; migration note in the delta. Stats file unchanged, so pipelines can still gate on counts.
- [Approval on a run where every comment was skipped feels wrong to some reviewers] → accepted deliberately: JSON is the source of truth; skipped means "already reported", findings still exist. Spec scenario pins this.
- [Empty review adds one more API write on clean runs] → negligible; review creation + submit, both retried by existing transport.
- [`Poster` interface change ripples to test mocks] → contained to this repo; done as part of the interface task before behavior tasks.

## Migration Plan

1. Land interface + decision + flag in one change (single binary release).
2. Announce `--fail-on-severity` removal in the release notes with the migration from the spec delta.
3. Rollback: revert to previous binary; behavior returns to always-`COMMENT` with no side effects left behind (an `APPROVED` review already submitted is not withdrawn — acceptable, next clean run re-approves anyway).

## Open Questions

None — severity ladder, threshold default (`low`), JSON-as-source-of-truth, and fallback semantics were settled during exploration.
