# Spec Delta

## MODIFIED Requirements

### Requirement: Single-review posting

The CLI SHALL post all anchorable comments as ONE Forgejo review per run: create the review with all inline comments, then submit it with the computed review state (see the `ocr-forgejo-review-state` capability: `APPROVED` when the result JSON has no findings at-or-above the approval threshold, `REQUEST_CHANGES` when findings post successfully, `COMMENT` when posting failed). Each inline comment SHALL be anchored at its `end_line` via the new-file line position; multiline spans SHALL be stated in the comment body. The review creation SHALL carry the commit SHA that OCR actually reviewed (from `manifest.input.resolved_head`, falling back to the PR's current head SHA) so anchors bind to the reviewed revision.

#### Scenario: All comments anchorable

- **WHEN** three comments all have a path and end_line
- **THEN** exactly one review is created containing three inline comments anchored at their respective end_line values

#### Scenario: Multiline span

- **WHEN** a comment spans start_line 10 to end_line 14
- **THEN** the comment is anchored at line 14 and its body states the 10-14 span

#### Scenario: Commit drift

- **WHEN** the PR head has moved since OCR ran and review creation carries the stale reviewed SHA
- **THEN** review creation fails against the stale SHA, comments are classified as unpostable with a stated reason, and the summary reports them

#### Scenario: Findings request changes

- **WHEN** anchorable comments above the approval threshold post successfully
- **THEN** the review is submitted with event `REQUEST_CHANGES` rather than `COMMENT`

## ADDED Requirements

### Requirement: Stats output

On every exit path the CLI SHALL write a dotenv stats file (default `.ocr/ocr-stats.env`) with mutually exclusive counts `OCR_COMMENTS_TOTAL`, `OCR_COMMENTS_INLINE`, `OCR_COMMENTS_SUMMARY`, `OCR_COMMENTS_ROUTED`, `OCR_COMMENTS_SKIPPED`, `OCR_COMMENTS_FAILED`, and `OCR_SUMMARY_URL`.

#### Scenario: Stats file always written

- **WHEN** posting fails partway through
- **THEN** the stats file still exists with the counts achieved so far

## REMOVED Requirements

### Requirement: Stats output and severity gating

**Reason**: `--fail-on-severity` is removed; the severity gate is superseded by the computed review state (`REQUEST_CHANGES`), which is the new signal for findings in CI. The stats-file portion of the requirement is retained unchanged as the `Stats output` requirement above.

**Migration**: Argo Workflows users who relied on the non-zero exit from `--fail-on-severity` should branch on the PR review state (or a Forgejo status check counting reviews) instead, or on the stats-file counts.
