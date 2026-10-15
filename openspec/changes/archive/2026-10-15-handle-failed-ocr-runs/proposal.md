# Proposal

## Why

A failed `ocr review` run can emit valid JSON with `"status": "failed"` and no comments/manifest. ocr-forgejo parses this fine but drops `status` on the floor, so the run takes the "no findings" path: it submits an empty APPROVED review, publishes a "Looks good to me" sticky summary, resolves every open ledger entry under `--resolution-tracking`, and exits 0. A review run that crashed gets to vouch for code it never reviewed and can unblock auto-merge.

## What Changes

- Parse the `status` field of the `ocr review --format json` result into `review.Result` (additive, optional; absent or any value other than `failed` keeps today's behavior — OCR's success vocabulary is deliberately not guessed at).
- When `status == "failed"`:
  - The run must NEVER submit an approval: no empty approval review, no APPROVED state, even when findings exist (state falls back to `COMMENT`).
  - `--resolution-tracking` must not resolve or add ledger entries.
  - The sticky summary becomes/shows a failure note (`⚠️` or similar) including `message` and `warnings` when present, instead of the LGTM body.
  - The run's one Forgejo review carries the failure note and is submitted with `COMMENT` (visible timeline note for the failed run), skipping inline comment anchoring.
  - The CLI exits non-zero (mirroring the already-codified malformed-JSON path) and the dotenv stats file gains an `OCR_RUN_STATUS=failed` line (other runs write `OCR_RUN_STATUS=completed`).
- `--dry-run` prints the failure note instead of posting.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `ocr-forgejo-review-posting`: the review-result parsing requirement gains the optional `status` field; a new requirement governs failed-run publishing (failure-note summary + `COMMENT` note review), and the stats-output requirement gains `OCR_RUN_STATUS`.
- `ocr-forgejo-review-state`: the review-state decision gains a failed-run override (never `APPROVED`, `COMMENT` instead) plus a non-zero exit requirement for failed runs.

## Impact

- `internal/review`: `Result` struct + tests (new `Status` field).
- `internal/publish`: `Publish` early branch for failed status; never-approve override; stats status plumbing; tests.
- `internal/format`: failure-note body builder (new small function) + tests.
- `cmd/ocr-forgejo`: exit code and stats-file behavior for failed runs; flag tests.
- `openspec/specs/ocr-forgejo-review-posting`, `openspec/specs/ocr-forgejo-review-state`: delta updates.
- Upstream (open-code-review): non-goal — surfacing *why* OCR failed (e.g. `tool_calls.failure_details`) stays upstream's job via `message`/stderr; ocr-forgejo only classifies).
