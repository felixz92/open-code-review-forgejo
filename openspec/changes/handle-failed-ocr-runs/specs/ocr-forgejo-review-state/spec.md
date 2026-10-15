# Spec Delta: ocr-forgejo-review-state (handle-failed-ocr-runs)

## MODIFIED Requirements

### Requirement: Review state decision

The CLI SHALL derive the submitted review state from the result JSON and the posting outcome, with no run-to-run state:
- findings = comments in the result JSON at-or-above the approval threshold, counted regardless of policy routing or incremental skipping; comments whose severity is not on the known ladder (including a missing severity field) SHALL always count as findings
- no findings → submit `APPROVED`
- findings present and posting succeeded (inline review submitted or summary published) → submit `REQUEST_CHANGES`
- findings present but posting failed → submit `COMMENT` instead, logging the posting failure
- result JSON reports `status: "failed"` → the state SHALL always be `COMMENT`, overriding every rule above, and no empty approval review SHALL be created (see the `ocr-forgejo-review-posting` capability: failed-run publication)

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

## ADDED Requirements

### Requirement: Failed run exits non-zero

When the result JSON reports `status: "failed"`, the CLI SHALL treat the run as failed: publish the failure note (see the `ocr-forgejo-review-posting` capability), write the stats file with `OCR_RUN_STATUS=failed`, and exit non-zero.

#### Scenario: Failed run reports failure to the workflow

- **WHEN** the CLI completes publishing the failure note for a result JSON with `status: "failed"`
- **THEN** the process exits non-zero and the stats file contains `OCR_RUN_STATUS=failed`
