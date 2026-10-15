# Design

## Context

The failed-run JSON is parseable by today's `review.Result` (`internal/review/review.go`), which simply has no field for `status`. Control flow: `cmd/ocr-forgejo/main.go:run` loads the result, then `publish.Publish` (\`internal/publish/publish.go:117\`) routes `stats.Total == 0` into the no-findings branch that submits the empty approval review and LGTM sticky summary; with resolution tracking it also resolves all ledger entries. Exit-code parity already exists: invalid JSON → `return 1` with stats written (`cmd/ocr-forgejo/main.go:203`).

## Goals / Non-Goals

**Goals:**

- Classify a run as failed from `status: "failed"` without breaking the consumed OCR contract (additive field, absent-field tolerance).
- Guarantee a failed run never vouches: no approval, no ledger mutation.
- Leave durable, workflow-visible evidence: failure-note review (`COMMENT`), failed sticky summary, `OCR_RUN_STATUS=failed` in the dotenv stats file, non-zero exit.

**Non-Goals:**

- Diagnosing why OCR failed (LLM tool errors, repo-owner confusion). That belongs upstream: OCR should put failure reasons into `message`/stderr; the poster only classifies and relays.
- Interpreting any other `status` values or building a status vocabulary registry.
- Formatting `summary`/`tool_calls` diagnostics from the failed JSON into the note (not part of the established `Result` contract).

## Decisions

1. **Exact-string gate on `failed`.** `review.Result` gains `Status string \`json:"status"\``. Only the exact value `"failed"` triggers failure handling; absent, empty, or any other string keeps today's behavior. Rationale: success fixtures never emit `status`, and OCR's success vocabulary is unknown — treating unknown non-empty values as failure could spuriously block legitimate runs. Alternative considered (treat unknown non-empty as failure, fail-safe) rejected as over-eager.
2. **Failure note reuses the single-review model.** One review per run (ADR 0001): the failed run creates an empty-input review anchored like today and submits it with `COMMENTS`/`COMMENT` event carrying `format`'s failure-note body. `cleanupStalePending` still runs before creation for idempotency. Alternative (summary-only, no review) rejected: the user-visible "note to the review" in the PR timeline is the point.
3. **Failure note lives in `internal/format`** next to `BuildNoIssuesBody` (e.g. `BuildFailureBody(message, warnings)`), rendering `⚠️ **OpenCodeReview**: <message> [warnings]`, keeping body rendering out of publish.
4. **Override placed as an early branch in `Publish`.** Before any existing path: if `result.Status == "failed"`, run cleanup, submit the note review (`decideState` bypassed — comment state regardless of findings), build the failure-note body, `upsertSummary` (sticky-safe, same as LGTM), skip ledger merge entirely, and mark a new stats field `RunStatus`.
5. **`RunStatus` travels through `publish.Stats`** as an enum string (`"failed"`/`"completed"`); `WriteStatsFile` adds `OCR_RUN_STATUS=`. No new flag: failed → exit 1 is unconditional (simplest honest default; a flag can be added if Argo templates need pass-through behavior later).
6. **Dry-run covered by the same branch** — the dry-run poster prints instead of posting, so the note is exercised without network.

## Risks / Trade-offs

- [OCR's success vocabulary changes (e.g. it starts emitting `status: "succeeded"` with a typo we'd misread)] → Mitigation: exact-string match on `failed` only; unknown values are never failed, so a vocabulary change degrades to today's behavior, never to a false failure.
- [Non-zero exit perceived as workflow breakage by existing Argo templates] → Mitigation: a failed OCR run is a real error; the stats file and summary give operators the reason. Templates already treat `return 1` (malformed JSON) as reasonable.
- [Failed run with leftover findings loses inline anchoring] → Accepted: findings from a manifest-less/failed run cannot be trusted to anchor; they remain visible in the summary body.
- [Duplicate note reviews on manual reruns of the same failed result] → Matches existing rerun semantics (one review per run); sticky summary upserts, notes accumulate by design like any review run.

## Open Questions

(None — vocabulary decisions above deliberately avoid depending on upstream answers.)
