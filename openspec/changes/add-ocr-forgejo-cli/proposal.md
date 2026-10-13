# Proposal

## Why

open-code-review (`ocr`) emits review results as JSON but ships no Forgejo glue: its CI examples cover GitLab, Gerrit, and Gitflic only. Teams running reviews from Argo Workflows against Forgejo repos have no way to publish those results onto pull requests. This change adds the missing piece: a Go CLI that reads `ocr review --format json` output and posts it to a Forgejo PR as a review with inline comments plus a sticky summary.

## What Changes

- New Go module `github.com/felixz92/open-code-review-forgejo` with binary `ocr-forgejo` in `cmd/ocr-forgejo`.
- CLI requiring explicit targeting flags (`--url`, `--repo owner/name`, `--pr <index>`) and an input path; auth via the `GIT_TOKEN` env var injected by Argo Workflows. No Forgejo Actions or CI-env auto-detection.
- Single-review posting model: one Forgejo review per run holding all inline comments (created with `CreatePullReview` + `SubmitPullReview`), replacing the GitLab per-discussion model. Stale pending bot reviews are deleted before retrying (idempotency).
- Inline comments anchored at `end_line` via `new_position`; multiline spans stated in the comment body (Forgejo has no line ranges).
- Sticky summary as an issue comment carrying the `<!-- ocr-summary -->` marker, updated in place across runs.
- Incremental mode (opt-in): skip comments overlapping prior bot review comments via line-range IoU.
- Publication policy routing (opt-in): route findings to the summary by severity threshold or category; fail-open on malformed input.
- Diff pre-check: parse the PR's unified diff and classify each comment valid/invalid/unknown before submitting; drop provably out-of-diff comments with a stated reason.
- Retry with backoff on 429/5xx/network errors; `--dry-run` mode; dotenv stats file; `--fail-on-severity` job gating.
- ADR documenting the single-review posting decision at `docs/adr/0001-single-review-posting.md`.

## Capabilities

### New Capabilities
- `ocr-forgejo-review-posting`: reading OCR result JSON and publishing it to a Forgejo pull request as a review with inline comments, a sticky summary, incremental filtering, policy routing, diff pre-check, retry/dry-run, and stats/gating outputs.

### Modified Capabilities

(none - the project has no existing specs)

## Impact

- New code: `cmd/ocr-forgejo`, `internal/{review,format,policy,incremental,diffinv,poster,publish}` packages.
- New dependency: `codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3`.
- New docs: `docs/adr/0001-single-review-posting.md`, README with Argo Workflows usage.
- Downstream: consumes the JSON schema of `ocr review --format json` (`comments[]{path, start_line, end_line, content, suggestion_code, existing_code, category, severity}`, `warnings`, `message`, `manifest.input.resolved_head/resolved_base`).
