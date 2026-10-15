# Tasks

## 1. Review parsing

- [x] 1.1 Add `Status string \`json:"status"\`` to `review.Result`; extend `internal/review/review_test.go` to cover a failed-run fixture (`status: "failed"`, absent `message`/`manifest`/`comments`) and confirm an absent `status` yields a zero value. Verify: `go test ./internal/review/`.

## 2. Failure-note body

- [x] 2.1 Add `BuildFailureBody` (message + optional warnings) alongside `BuildNoIssuesBody` in `internal/format`; unit-test rendering with and without `message`/`warnings` covering the exact `⚠️` prefix. Verify: `go test ./internal/format/`.

## 3. Failed-run publication

- [x] 3.1 Add `RunStatus` to `publish.Stats` and emit `OCR_RUN_STATUS=` in `WriteStatsFile` (`failed`/`completed`); unit-test the dotenv line for both values. Verify: `go test ./internal/publish/ -run TestWriteStatsFile`.
- [x] 3.2 Add the early `result.Status == "failed"` branch in `publish.Publish`: `cleanupStalePending`, submit one empty review with `COMMENT` carrying `BuildFailureBody`, `upsertSummary` with the failure-note body, no ledger merge, findings treated as information in the note, `stats.RunStatus = "failed"`. Cover with poster-fake tests: failed+no-comments (no APPROVED, note review COMMENT), failed+findings (still never APPROVED), resolution-tracking untouched, repeated failed run upserts one summary. Verify: `go test ./internal/publish/`.
- [x] 3.3 Cover the `--dry-run` failed-result path (note printed, no API writes). Verify: `go test ./internal/publish/ -run DryRun` plus an end-to-end `bin/ocr-forgejo --dry-run` run against a failed-result fixture.

## 4. CLI wiring

- [x] 4.1 In `cmd/ocr-forgejo/main.go:run`, return 1 after a failed run (stats written, reason logged) while keeping exit 0 for completed runs; extend `cmd/ocr-forgejo` tests for both paths. Verify: `go test ./cmd/ocr-forgejo/`.

## 5. Verification

- [x] 5.1 Full gate: `task fmt`, `task vet`, `task lint`, `task test` all pass; diagnostics clean on touched Go files. Verify: commands exit zero.
- [x] 5.2 Add the user-reported failed-run JSON (`"status": "failed"`, `comments: null`, no manifest/message) as a shared test fixture exercised by both `review.Load` and `publish.Publish` so the exact reported case stays reproducible. Verify: `go test ./internal/review/ ./internal/publish/` passes with the fixture present.
