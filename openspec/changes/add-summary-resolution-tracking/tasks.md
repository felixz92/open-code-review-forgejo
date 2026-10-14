# Tasks

## 1. Ledger model and serialization (`internal/format`)

- [x] 1.1 Define ledger types (entry: path, span, first-seen, resolved-at, content snippet; ledger: version + open/resolved lists), marker constants `<!-- ocr-ledger -->` / `<!-- /ocr-ledger -->`, and a max-resolved constant (20); verify with `go build ./...`
- [x] 1.2 Implement ledger JSON encode/parse with version field, tolerant of missing fields; unit tests: round-trip, corrupt JSON returns error, missing marker returns not-found; run `go test ./internal/format`
- [x] 1.3 Extract the ledger block from a summary body (returns body-without-ledger, ledger, found); unit tests: body with and without block, malformed block; run `go test ./internal/format`

## 2. Ledger matching and merge (`internal/publish` + `internal/incremental`)

- [x] 2.1 Implement ledger merge: match current posted inline items against open entries via `incremental.SameCommentSpan` (path-scoped), refresh matched spans, add unmatched as new open entries, resolve unmatched open entries with resolved-at stamp, prune resolved beyond cap; unit tests per spec scenario: persists, resolves, adds, line-shift dedup, prune; run `go test ./internal/publish`
- [x] 2.2 Wire merge failure and summary-read failure to fail-open degradation (fresh ledger from current run, logged); unit test simulating unreadable/corrupt prior summary; run `go test ./internal/publish`

## 3. Summary rendering

- [x] 3.1 Render open findings section (summary-fallback style: path, span, content) and resolved section (path, span, resolved marker) from the merged ledger; unit tests for section presence and ordering; run `go test ./internal/format`
- [x] 3.2 Handle the clean-run cases: non-empty open ledger resolves all and reports resolutions (not a bare "looks good to me"); empty ledger keeps existing body; unit tests for both; run `go test ./internal/publish`
- [x] 3.3 Ensure tracking-disabled path strips any legacy ledger block and produces byte-identical classic summary; unit test; run `go test ./internal/publish`

Note: concurrent runs on the same PR race on the summary body; Argo
serializes per-PR runs today, so last-writer-wins is accepted (design
risk note). No locking mechanism is added.

## 4. CLI flag, stats, and posting flow

- [x] 4.1 Add `--resolution-tracking` flag (default false) in `cmd/ocr-forgejo` and thread it into `publish.Options`; verify flag parse via `go build` and a CLI smoke test
- [x] 4.2 Insert ledger read/match/merge into `Publish` after successful review submit (posted inline items only) and render ledger into the final summary before `upsertSummary`; add `Resolved` to `Stats` and `OCR_FINDINGS_RESOLVED` to the dotenv stats file; unit tests: full Publish flow with a fake Poster across two runs (open, then resolved), stats file assertion; run `go test ./internal/publish ./cmd/...`

## 5. Verification

- [x] 5.1 Run `task fmt && task vet && task lint && task test` and fix any findings
- [x] 5.2 Run `--dry-run` twice against a sample result with tracking on and confirm ledger appears in printed summary and second run shows no duplicate entries
- [x] 5.3 Run `serena_get_diagnostics_for_file` on every touched Go file and confirm no errors
