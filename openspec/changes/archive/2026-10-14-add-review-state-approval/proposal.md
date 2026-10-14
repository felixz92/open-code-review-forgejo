# Proposal

## Why

The bot currently submits every review with event `COMMENT`, so a clean review run never changes PR mergeability status, and a run with real findings leaves the PR status neutral even though the bot's verdict is effectively "changes requested". PR status should reflect the bot's verdict, derived entirely from the OCR result JSON plus the posting outcome — no run-to-run state.

## What Changes

- Reviews are submitted with a computed review state instead of always `COMMENT`:
  - JSON contains no findings at-or-above the approval threshold → submit `APPROVED`.
  - Findings exist and posting succeeded (inline or summary) → submit `REQUEST_CHANGES`.
  - Findings exist but posting failed → fall back to `COMMENT` (log the failure).
- New CLI flag `--approve-below <severity>` (default `low`, at-or-below semantics matching `--route-severity-below`): the highest severity that does not block approval.
- Findings are counted from the OCR JSON regardless of routing or incremental skipping — routing and skipping are presentation, not verdicts.
- Zero-finding runs additionally create and submit an empty review (today they only post the sticky LGTM summary, never touching the review API).
- When Forgejo refuses the state (self-approval, branch protection), log and continue — the run does not fail.
- **BREAKING**: `--fail-on-severity` is removed; the severity-gating exit code goes away. `REQUEST_CHANGES` is the new severity signal.

## Capabilities

### New Capabilities

- `ocr-forgejo-review-state`: computed review-state decision — approval threshold flag, the APPROVED/REQUEST_CHANGES/COMMENT decision table, empty-review approval on clean runs, and log-and-continue on Forgejo refusal.

### Modified Capabilities

- `ocr-forgejo-review-posting`: "Single-review posting" requirement changes from fixed `COMMENT` event to the computed state (kept as requirement — still one review per run, good summary). "Stats output and severity gating" requirement loses the `--fail-on-severity` gating clause; stats-file clause unchanged.

## Impact

- `internal/publish` (state decision, empty-review path on zero findings), `internal/poster` (`SubmitPullReview` takes the state instead of hardcoding `ReviewStateComment`; dry-run prints it), `cmd/ocr-forgejo` (new `--approve-below` flag, `--fail-on-severity` removed), spec `ocr-forgejo-review-posting`.
- Argo users relying on the non-zero exit from `--fail-on-severity` must switch to reading the PR review state or the stats file.
- Forgejo branch protection may not count bot approvals; behavior is log-and-continue.
