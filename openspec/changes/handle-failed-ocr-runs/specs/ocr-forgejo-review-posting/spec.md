# Spec Delta: ocr-forgejo-review-posting (handle-failed-ocr-runs)

## MODIFIED Requirements

### Requirement: Review result parsing

The CLI SHALL parse the JSON emitted by `ocr review --format json`, including `comments[]` (path, start_line, end_line, content, suggestion_code, existing_code, category, severity), `warnings[]`, `message`, `status`, and `manifest.input.resolved_head` / `manifest.input.resolved_base`. Missing fields MUST be tolerated as zero values. When the JSON cannot be parsed and a stderr-log path is provided, the CLI SHALL include the log tail in its error output. The `status` field SHALL be treated as a failed-run marker matching the exact string `failed`; an absent `status` or any other value MUST keep the existing behavior, so older result files without the field remain compatible.

#### Scenario: Minimal result with no comments

- **WHEN** the result JSON contains an empty comments array
- **THEN** the CLI posts no review comments and publishes a summary saying the review found no issues

#### Scenario: Malformed JSON with stderr log

- **WHEN** the result file is not valid JSON and the stderr-log file exists
- **THEN** the CLI exits non-zero and its error output includes content from the stderr log

#### Scenario: Result without status behaves as completed run

- **WHEN** the result JSON contains no `status` field (or a value other than `failed`) and an empty comments array
- **THEN** the run behaves exactly as before this requirement gained the field: empty approval review plus the LGTM summary

### Requirement: Stats output

On every exit path the CLI SHALL write a dotenv stats file (default `.ocr/ocr-stats.env`) with mutually exclusive counts `OCR_COMMENTS_TOTAL`, `OCR_COMMENTS_INLINE`, `OCR_COMMENTS_SUMMARY`, `OCR_COMMENTS_ROUTED`, `OCR_COMMENTS_SKIPPED`, `OCR_COMMENTS_FAILED`, the count `OCR_FINDINGS_RESOLVED`, the summary URL `OCR_SUMMARY_URL`, and a run-status line `OCR_RUN_STATUS` whose value is `failed` when the result JSON reported `status: "failed"` and `completed` otherwise.

#### Scenario: Stats file always written

- **WHEN** posting fails partway through
- **THEN** the stats file still exists with the counts achieved so far

#### Scenario: Stats file marks a failed run

- **WHEN** the CLI publishes the failure note for a result JSON with `status: "failed"`
- **THEN** the stats file exists with `OCR_RUN_STATUS=failed` and `OCR_COMMENTS_TOTAL=0`

## ADDED Requirements

### Requirement: Failed run publication

When the result JSON reports `status: "failed"`, the CLI SHALL NOT submit an approval under any circumstances and SHALL NOT create an empty approval review. The run SHALL publish a sticky summary carrying a failure note that names the failure and includes the `message` field and any `warnings[]` when present, and SHALL submit exactly one Forgejo review with event `COMMENT` carrying the same failure note in the review body. Resolution tracking SHALL leave the resolution ledger untouched for that run: no entries resolved, no entries added. Any findings present in a failed result are informational only: they land in the failure-note summary and are never posted as anchored inline comments, and the submitted review state is `COMMENT`.

#### Scenario: Failed run with no comments publishes failure note

- **WHEN** the result JSON has `status: "failed"`, no comments, and no manifest
- **THEN** no approval review is submitted, the sticky summary shows the failure note instead of the LGTM body, and one review with event `COMMENT` is submitted carrying the failure note

#### Scenario: Failed run keeps its failure note across reruns

- **WHEN** the CLI runs on the same PR with a failed result twice
- **THEN** the failure note is upserted into the existing sticky summary (one summary comment total) and each run submits its own `COMMENT` note review

#### Scenario: Failed run does not resolve tracked findings

- **WHEN** resolution tracking is enabled and the result JSON has `status: "failed"`
- **THEN** previously open ledger entries remain open and the failure-note summary reports no resolutions

#### Scenario: Failed run with leftover findings never approves

- **WHEN** the result JSON has `status: "failed"` and findings above or below any approval threshold
- **THEN** the run does not submit `APPROVED`; findings land in the failure-note summary and the review is submitted as `COMMENT`

#### Scenario: Dry-run of failed result

- **WHEN** the CLI runs with `--dry-run` against a result JSON with `status: "failed"`
- **THEN** the failure note is printed and no API write occurs

### Requirement: Failed run exit code

When the result JSON reports `status: "failed"`, the CLI SHALL exit non-zero after publishing the failure note and writing the stats file, so an Argo Workflow step surfaces the failure instead of silently passing. This mirrors the existing behavior for unparseable result files.

#### Scenario: Failed run exits non-zero

- **WHEN** the CLI publishes the failure note for a result JSON with `status: "failed"`
- **THEN** the process exits non-zero with the reason logged to stderr
