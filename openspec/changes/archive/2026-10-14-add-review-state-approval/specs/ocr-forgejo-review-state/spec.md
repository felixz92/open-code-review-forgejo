# Spec Delta

## Purpose

Derive the Forgejo review state (APPROVED, REQUEST_CHANGES, COMMENT) from the OCR result JSON and the posting outcome, so a clean run approves the PR and a run with findings requests changes.

## ADDED Requirements

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
- no findings → submit `APPROVED`
- findings present and posting succeeded (inline review submitted or summary published) → submit `REQUEST_CHANGES`
- findings present but posting failed → submit `COMMENT` instead, logging the posting failure

#### Scenario: Clean result approves

- **WHEN** the result JSON has no comments
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
