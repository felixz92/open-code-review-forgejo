# Tasks

## 1. Format layer: suggestion rendering

- [x] 1.1 Relax `SuggestionBlocks` in `internal/format/format.go` to render Before/After when both `suggestion_code` and `existing_code` are present, an After-only block when only `suggestion_code` is present, and nothing otherwise; update `TestSuggestionBlocks`/`TestSuggestionBlocksAbsent` and add an After-only case; verify with `go test ./internal/format/`
- [x] 1.2 Add a suggestion-aware variant or flag parameter to `FormatComment` (design D6) and to `FormatCommentFallback` so callers can omit suggestion blocks; update existing format tests and add cases for with/without suggestions; verify with `go test ./internal/format/`

## 2. Publish layer: gating and flag plumbing

- [x] 2.1 Add `Suggestions bool` to `publish.Options` and thread the resolved flag into comment rendering (inline bodies and summary fallback bodies); verify with `go test ./internal/publish/`
- [x] 2.2 Append the suggestion suffix to inline bodies in the diff pre-check loop when `Suggestions` is on and classification is not Invalid, covering: valid span (suggestions included), diff unavailable/unparseable (fail-open, included), and invalid span (comment dropped, fallback keeps blocks); add publish tests for all three; verify with `go test ./internal/publish/`

## 3. CLI flag

- [x] 3.1 Add `--suggestions` bool flag (default true) in `cmd/ocr-forgejo` and map it to `publish.Options.Suggestions`; verify with `go build ./...` and an existing flag-parsing test or a new one asserting the default

## 4. Integration verification

- [x] 4.1 Run `task fmt`, `task vet`, `task lint`, and `task test` and confirm all pass; confirm a dry-run output with suggestions on shows Before/After blocks on inline comments and `--suggestions=false` shows none
