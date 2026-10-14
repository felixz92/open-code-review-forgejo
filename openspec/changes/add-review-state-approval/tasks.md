# Tasks

## 1. Poster interface and threshold config

- [ ] 1.1 Change `Poster.SubmitPullReview` to accept a state parameter, update `ForgejoPoster` (map string to `forgejo.ReviewState*`), `DryRunPoster` (print state), and all test mocks; verify with `go build ./...` and `go vet ./...`
- [ ] 1.2 Add `ApproveBelow` to publish `Options` and CLI config with default `low`, parsing severity via `policy`'s severity ladder with fail-safe (unknown → fail-safe threshold) semantics; add unit test for default, valid, and malformed values, verify with `go test ./internal/policy/... ./internal/publish/...`

## 2. Review state decision

- [ ] 2.1 Implement the pure decision in `internal/publish` (findings = JSON comments at-or-above threshold, ignoring routing and skips; outcome → APPROVED / REQUEST_CHANGES / COMMENT) and expose it on `Stats.State`; table-driven unit tests covering every scenario in `specs/ocr-forgejo-review-state/spec.md` (clean, low-only, finding, routed, skipped, posting failure), verify with `go test ./internal/publish/...`
- [ ] 2.2 Wire the computed state into the findings-path submit call; on submit failure after pending-review deletion, log and record `COMMENT` as the state; on creation failure record `COMMENT`; verify via publish tests asserting submitted state in mock poster, `go test ./internal/publish/...`

## 3. Empty-review approval on clean runs

- [ ] 3.1 In the `total == 0` branch, resolve the commit (`--commit-id` fallback to PR head), create and submit an empty review with `APPROVED`, keeping the LGTM summary; log-and-continue when review creation or submission fails; add publish tests for success and failure paths, verify with `go test ./internal/publish/...`

## 4. CLI flag changes

- [ ] 4.1 Add `--approve-below` flag (default `low`) to `cmd/ocr-forgejo/main.go` and remove `--fail-on-severity`, `Config.FailOnSeverity`, and the exit-code gate; update CLI tests, verify with `go test ./cmd/...`
- [ ] 4.2 Verify `--dry-run` prints the computed state for clean, low-only, and finding inputs, verify with a dry-run test or manual run against a fixture

## 5. Verification

- [ ] 5.1 Run `task fmt`, `task vet`, `task lint`, and `task test`; all pass
- [ ] 5.2 Run `openspec validate --change add-review-state-approval --strict`; passes
