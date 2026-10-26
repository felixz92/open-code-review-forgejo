# ocr-forgejo-review-state Specification

## Purpose

Derive the Forgejo review state (APPROVED, REQUEST_CHANGES, COMMENT) from the OCR result JSON and the posting outcome, so a clean run approves the PR and a run with findings requests changes.

## Requirements

### Requirement: Approval threshold flag

The CLI SHALL accept `--approve-below <severity>` with at-or-below semantics matching `--route-severity-below`, defaulting to `low`: the named severity and everything below it does not block approval. Unknown or malformed values MUST fail safe as if the threshold were the lowest severity (only runs with zero findings approve). Severities SHALL be the known ladder (`low`, `medium`, `high`, `critical`).

#### Scenario: Default threshold

- **WHEN** the CLI runs without `--approve-below`
- **THEN** findings at severity `low` do not block approval and `medium` and above do

#### Scenario: Malformed threshold fails safe

- **WHEN** `--approve-below banana` is set
- **THEN** only runs whose result JSON contains no comments can produce `APPROVED`

### Requirement: Review state decision

The CLI SHALL derive the submitted review state from the result JSON and the posting outcome, with no run-to-run state:
- findings = comments in the result JSON at-or-above the approval threshold, counted regardless of policy routing or incremental skipping; comments whose severity is not on the known ladder (including a missing severity field) SHALL always count as findings
- no findings and no open ledger entries surviving the merge → submit `APPROVED`
- findings present and posting succeeded (inline review submitted or summary published) → submit `REQUEST_CHANGES`
- findings present but posting failed → submit `COMMENT` instead, logging the posting failure
- result JSON reports `status: "failed"` → the state SHALL always be `COMMENT`, overriding every rule above, and no empty approval review SHALL be created (see the `ocr-forgejo-review-posting` capability: failed-run publication)

When resolution tracking is enabled and the run yields zero findings but open ledger entries survive the resolution merge (for example on a delta run where the coverage diff does not cover those entries), the CLI SHALL NOT submit `APPROVED`: the state SHALL be downgraded to `COMMENT`, and the sticky summary SHALL note that the run was clean but findings remain open, including the surviving open-entry count. When the surviving open-entry count is zero, the normal approval rules above apply unchanged.

#### Scenario: Clean result approves

- **WHEN** the result JSON has no comments and no `status` field (or a value other than `failed`)
- **THEN** the run submits an `APPROVED` review

#### Scenario: Low-severity-only result approves

- **WHEN** all comments in the result JSON are at-or-below the approval threshold
- **THEN** the run submits an `APPROVED` review even though inline/summary comments exist

#### Scenario: Finding requests changes

- **WHEN** the result JSON has a comment above the approval threshold and it posts successfully
- **THEN** the run submits the review with `REQUEST_CHANGES`

#### Scenario: Routed findings still count

- **WHEN** a comment above the threshold is routed to the summary by publication policy
- **THEN** it still counts as a finding and the run submits `REQUEST_CHANGES`

#### Scenario: Skipped findings still count

- **WHEN** incremental mode skips every comment above the threshold
- **THEN** the run still submits `REQUEST_CHANGES`

#### Scenario: Posting failure falls back to comment

- **WHEN** a finding exists but the review cannot be created or submitted
- **THEN** the run logs the failure and submits `COMMENT` (or no review when nothing could be created)

#### Scenario: Unranked comment severity fails safe

- **WHEN** a comment in the result JSON carries a severity that is not on the known ladder, for example because the field is missing entirely
- **THEN** the comment counts as a finding and the run does not submit `APPROVED`

#### Scenario: Failed run never approves

- **WHEN** the result JSON reports `status: "failed"` regardless of findings (even findings entirely below the approval threshold)
- **THEN** the run submits `COMMENT` (or no review when the note review could not be created) and never `APPROVED`

#### Scenario: Open entries downgrade approval

- **WHEN** a clean delta run yields zero findings with resolution tracking enabled, and open ledger entries survive because the coverage diff does not cover them
- **THEN** the CLI submits `COMMENT` instead of `APPROVED`, and the summary notes the remaining open findings count

#### Scenario: Zero surviving entries approves normally

- **WHEN** a clean run yields zero findings and the resolution merge leaves no open entries
- **THEN** the CLI submits `APPROVED` as in previous releases

### Requirement: Empty review on clean run

When the result JSON has no findings, the CLI SHALL additionally create and submit an empty Forgejo review carrying the computed state (in addition to the sticky "no issues" summary), so the PR review status changes on clean runs.

#### Scenario: Clean run posts empty approval review

- **WHEN** the result JSON has no comments
- **THEN** exactly one review with no inline comments is submitted with `APPROVED` and the LGTM summary is still published

### Requirement: Forgejo state refusal is non-fatal

When Forgejo refuses the submitted state (for example self-approval or branch-protection restrictions), the CLI SHALL log the refusal and continue: the summary and any already-posted comments remain, and the run does not exit non-zero because of the refusal alone.

#### Scenario: Self-approval refused

- **WHEN** Forgejo rejects the `APPROVED` submission because the bot authored the PR
- **THEN** the CLI logs the refusal, completes the run, and exits zero

### Requirement: Failed run exits non-zero

When the result JSON reports `status: "failed"`, the CLI SHALL treat the run as failed: publish the failure note (see the `ocr-forgejo-review-posting` capability), write the stats file with `OCR_RUN_STATUS=failed`, and exit non-zero.

#### Scenario: Failed run reports failure to the workflow

- **WHEN** the CLI completes publishing the failure note for a result JSON with `status: "failed"`
- **THEN** the process exits non-zero and the stats file contains `OCR_RUN_STATUS=failed`
