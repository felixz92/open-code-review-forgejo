# Proposal

## Why

When a PR head is pushed again (e.g. after fixing previous review comments), CI
currently runs a full `ocr review` over `origin/<base>..refs/pr-head` every
time. On fix-up pushes, ocr-forgejo's resolution ledger wrongly marks untouched
findings as "resolved" and a clean delta run would wrongly approve the PR,
because every run today implicitly re-reviews the whole PR.

## What Changes

- **Last-reviewed marker**: the sticky summary success paths (approve and
  no-issues) record the reviewed head SHA (`manifest.input.resolved_head`) in a
  new `<!-- ocr-last-reviewed:<sha> -->` HTML marker. `publishFailed` never
  writes it, and it is omitted when the SHA is unknown, so only successful runs
  count as "reviewed".
- **Query mode**: new `--print-last-reviewed` flag. The CLI locates the sticky
  summary comment, extracts the marker SHA, prints it to stdout, and exits 0.
  An absent marker or API failure prints nothing and exits 0 (caller falls back
  to a full review).
- **Range-aware ledger resolution**: new `--coverage-diff <path>` flag taking a
  unified diff of the exact range the run reviewed. An open ledger entry may
  only be resolved when the diff inventory classifies its path+span as valid
  (code under the finding changed AND the reviewer did not re-report it).
  Invalid/Unknown/out-of-diff entries stay open. Unreadable diffs mean "no
  coverage": nothing resolves (conservative). Without the flag, current
  full-coverage semantics are unchanged.
- **Approval downgrade**: when resolution tracking is enabled and open entries
  survive the merge after a clean delta run, the review state is downgraded
  from APPROVED to COMMENT with a note in the summary ("delta clean, N findings
  remain open").

Non-goal (this repo): the task-ci.yaml workflow wiring (marker read-back,
`--from` computation, `--background` prompt, coverage-diff production) lives in
the homelab repo and is tracked separately.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `ocr-forgejo-review-posting`: adds the last-reviewed marker to success-path
  summary bodies, a `--print-last-reviewed` query mode, and coverage-diff–aware
  resolution in the findings ledger (open entries resolve only when the
  coverage diff proves the code under them changed).
- `ocr-forgejo-review-state`: adds the approval downgrade rule — a clean run
  must not APPROVE while open ledger entries survive the merge.

## Impact

- `internal/format/format.go`: new `LastReviewedMarkerFmt` constant.
- `internal/publish/publish.go`: marker write on success paths, `decideState`
  downgrade, query-mode implementation, new flag plumbing.
- `internal/publish/ledger.go`: `mergeLedger` / `trackedNoIssuesBody` respect
  coverage classification.
- `internal/diffinv`: reused (no changes expected).
- `cmd/ocr-forgejo/main.go`: new flags.
- Existing behavior without the new flags is unchanged (backward compatible).
