# Tasks

## 1. Last-reviewed marker

- [ ] 1.1 Add `LastReviewedMarkerFmt` constant to `internal/format/format.go` and a helper that renders the marker from a resolved head SHA, returning empty string for an empty SHA (TDD: write failing format tests first for present/absent SHA, then implement; `go test ./internal/format/...`)
- [ ] 1.2 Write the marker into the success summary bodies in `internal/publish/publish.go` (approve path and no-issues path) using `result.Manifest.Input.ResolvedHead`, and ensure `publishFailed` never emits it (TDD: tests asserting marker present after success, absent after `publishFailed`, absent when `ResolvedHead` empty, and preserved across a ledger re-encode; `go test ./internal/publish/...`)
- [ ] 1.3 Run `serena_get_diagnostics_for_file` on `internal/format/format.go` and `internal/publish/publish.go`; fix any reported errors

## 2. Query mode

- [ ] 2.1 Add the `--print-last-reviewed` flag to `cmd/ocr-forgejo/main.go` and implement the query path: locate the sticky summary comment (reuse `findSummaryComment` lookup logic), extract the marker SHA, print it to stdout, exit 0; print nothing and exit 0 on missing summary, missing marker, or API failure with a stderr warning (TDD: failing tests first for found, absent, API-error, and pagination cases; `go test ./...`)
- [ ] 2.2 Verify query mode performs no writes: add a test asserting no review/comment API mutations on a fake poster when `--print-last-reviewed` is set (`go test ./...`)
- [ ] 2.3 Run `serena_get_diagnostics_for_file` on touched files; fix any reported errors

## 3. Coverage-aware ledger resolution

- [ ] 3.1 Add the `--coverage-diff <path>` flag, plumb it into publish options (absent ⇒ nil coverage), and reject `--coverage-diff` without `--resolution-tracking` in `Config.validate` (mirroring the existing sticky-summary validation) (TDD: failing flag-parsing and validation tests first; `go test ./cmd/...`)
- [ ] 3.2 Implement coverage gating in `mergeLedger` and `trackedNoIssuesBody`: an unmatched open entry resolves only when `internal/diffinv` classifies its path+span as valid against the coverage diff; invalid/unknown/out-of-diff entries stay open; unreadable or unparseable diff means no coverage (nothing resolves, warning logged, no silent full-coverage fallback); nil coverage keeps current semantics (TDD: failing tests first for in-diff resolves, out-of-diff stays open, in-diff re-reported stays open with span refresh, no-flag resolves as before, unparseable diff resolves nothing; `go test ./internal/publish/...`)
- [ ] 3.3 Run `serena_get_diagnostics_for_file` on `internal/publish/ledger.go` and touched files; fix any reported errors

## 4. Approval downgrade

- [ ] 4.1 Extend `decideState` in `internal/publish/publish.go`: with resolution tracking on, zero findings plus surviving open entries ⇒ `COMMENT` with a summary note "delta clean, N findings remain open"; zero survivors ⇒ APPROVED as before (TDD: failing tests first for downgrade and zero-survivor approval; `go test ./internal/publish/...`)
- [ ] 4.2 Render the remaining-open note in the summary body and verify it appears only on downgraded runs (TDD: summary body test; `go test ./internal/publish/...`)
- [ ] 4.3 Run `serena_get_diagnostics_for_file` on `internal/publish/publish.go`; fix any reported errors

## 5. Integration verification

- [ ] 5.1 Run `task fmt`, `task vet`, `task lint`, and `task test` and confirm all pass
- [ ] 5.2 Manually verify backward compatibility: run the binary with the old flag set only (no `--coverage-diff`, no `--print-last-reviewed`) against a fixture result file and confirm output matches pre-change behavior (dry-run or fixture-based test)
