# Tasks

## 1. Module setup

- [ ] 1.1 Run `go mod init github.com/felixz92/open-code-review-forgejo` and add `codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3` dependency; verify `go mod tidy` succeeds and the SDK resolves (review/issue/PR types present in the v3 package)
- [ ] 1.2 Scaffold `cmd/ocr-forgejo/main.go` with the flag set (`--url`, `--repo`, `--pr`, input positional, `--input`, `--stats-file`, `--dry-run`, `--sticky-summary`, `--incremental`, `--incremental-overlap-threshold`, `--route-severity-below`, `--route-categories`, `--fail-on-severity`, `--stderr-log`, `--run-tag`) and a config struct; verify `go build ./...` compiles
- [ ] 1.3 Write `docs/adr/0001-single-review-posting.md` (context: GitLab per-discussion vs Forgejo review primitive; decision: one review per run; consequences: atomic submit, delete-stale-pending idempotency, no per-finding threads, multiline anchors at end_line); verify the file exists and states the decision

## 2. Result parsing

- [ ] 2.1 Implement `internal/review`: OCR result structs (`comments[]` with path/start_line/end_line/content/suggestion_code/existing_code/category/severity, `warnings[]`, `message`, `manifest.input.resolved_head/resolved_base`) plus a tolerant loader (missing fields -> zero values) and stderr-log tail on parse failure; verify unit tests cover minimal result, missing fields, and malformed JSON with stderr log

## 3. Pure formatting logic

- [ ] 3.1 Implement `internal/format`: badge builder (`[category · severity]`, omit absent parts), safe fence length, Before/After fenced blocks, collapsible fallback formatting (path, span, reason), warning rendering, summary body builders (counts, pre-review "posting..." body, LGTM body); verify unit tests cover badge variants, backtick-embedded fences, fallback rendering, and count formatting
- [ ] 3.2 Implement `internal/policy`: `build_policy` (severity-below threshold, category list, fail-open on malformed values) and `route_comment` (severity at-or-below or category match, reason strings); verify unit tests cover severity routing, category routing, unknown metadata never matching, and malformed policy fail-open
- [ ] 3.3 Implement `internal/incremental` pure parts: `comment_span` (single/multi-line), IoU-based `same_comment_span` (strictly greater than threshold; single-line exact match), overlap predicate; verify unit tests cover the IoU boundary (equal-to-threshold is not a match), multiline-vs-single mismatch, and non-overlapping spans
- [ ] 3.4 Implement `internal/diffinv`: unified-diff hunk parsing into per-file new-line ranges (hunk-header declared vs observed length completeness check), classification tri-state valid/invalid/unknown; verify unit tests cover in-range, out-of-range, unknown-file, truncated (incomplete) inventory, and empty diff cases

## 4. Transport

- [ ] 4.1 Implement `internal/poster`: `Poster` interface (CreatePullReview, SubmitPullReview, DeletePullReview, ListPullReviews, ListIssueComments, CreateIssueComment, EditIssueComment, GetPullRequestDiff or ListPullRequestFiles, GetPullRequest) with retry/backoff+jitter on 429/5xx/network errors (default 3 attempts, no retry on other 4xx) and a `DryRunPoster` that prints review comments and summary; verify unit tests with a fake HTTP transport cover retry on 500/429, no retry on 400, backoff cap, and that dry-run performs no writes
- [ ] 4.2 Verify SDK v3 surface at implementation: confirm `CreatePullReviewOptions`/`CreatePullReviewComment`/`SubmitPullReview` field names in v3 and whether an empty review body with comments is accepted; record findings and adjust the poster accordingly (one-line summary in review body if required)

## 5. Publishing orchestration

- [ ] 5.1 Implement `internal/publish`: partition (inline / no-line / policy-routed), deterministic sort (path, start_line, end_line, original index), incremental filtering via `ListPullReviews`+`ListPullReviewComments` bot-marker detection, pending-review cleanup before creation, diff pre-check drop, create+submit review with CommitID, sticky summary anchor/finalize via issue comments (marker + per-run tag, newest-first lookup), stats accumulation; verify integration test against a fake `Poster` covers full posting, second-run summary update-in-place, no-line/routed/failed comments in summary, out-of-diff drop, incremental skip, and stale pending review deletion
- [ ] 5.2 Implement stats dotenv writer (always written, zero-filled on early exit) and `--fail-on-severity` gating with exit codes in `cmd/ocr-forgejo/main.go`; verify unit tests cover partial-failure stats and severity-gate exit code

## 6. CLI wiring and docs

- [ ] 6.1 Wire `cmd/ocr-forgejo/main.go` end-to-end: flag/config parsing, `GIT_TOKEN` requirement (skipped in dry-run), fail-fast on missing flags, run-tag derivation (PR index + head SHA, `--run-tag` override); verify `go build ./...` and a `--dry-run` run against a fixture `.ocr/ocr-result.json` prints review comments and summary with no network calls
- [ ] 6.2 Write README with usage, flag reference, and an Argo Workflows example step (`GIT_TOKEN` env, required flags, stats file consumption); verify documented commands run as written (`--help`, `--dry-run`)

## 7. Integration verification

- [ ] 7.1 Run `go vet ./...` and `go test ./...`; verify all green
- [ ] 7.2 Optional manual smoke against a real Forgejo instance with a scratch PR: run twice (sticky summary updates in place, no duplicate reviews), run with an out-of-diff line (dropped with reason), run with `--incremental` after adding an overlapping finding (skip counted); record results
