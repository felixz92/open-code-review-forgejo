# ocr-forgejo-review-posting Specification

## Purpose

Publish open-code-review (`ocr`) results onto Forgejo pull requests from CI: one review per run with inline comments, a sticky summary comment, incremental filtering against prior bot activity, policy routing, and a diff pre-check, with retry, dry-run, and stats-file behavior for Argo Workflows integration.

## Requirements

### Requirement: CLI targeting and authentication

The CLI SHALL require `--url` (Forgejo base URL), `--repo` (owner/name), and `--pr` (pull request index) as flags, take the review-result JSON path as a positional argument, and authenticate using the `GIT_TOKEN` environment variable. The CLI MUST fail fast with a clear error when a required flag is missing or `GIT_TOKEN` is unset (except in dry-run mode, where the token is not required). The CLI MUST NOT read Forgejo Actions or other CI environment variables for targeting.

#### Scenario: Missing required flag

- **WHEN** the CLI runs without `--pr`
- **THEN** it exits non-zero with an error naming the missing flag and posts nothing

#### Scenario: Missing token outside dry-run

- **WHEN** the CLI runs with all flags but `GIT_TOKEN` is unset and `--dry-run` is not set
- **THEN** it exits non-zero with an error naming `GIT_TOKEN` before making any network call

#### Scenario: Dry-run without token

- **WHEN** the CLI runs with `--dry-run` and `GIT_TOKEN` unset
- **THEN** it proceeds without authenticating and prints instead of posting

### Requirement: Review result parsing

The CLI SHALL parse the JSON emitted by `ocr review --format json`, including `comments[]` (path, start_line, end_line, content, suggestion_code, existing_code, category, severity), `warnings[]`, `message`, and `manifest.input.resolved_head` / `manifest.input.resolved_base`. Missing fields MUST be tolerated as zero values. When the JSON cannot be parsed and a stderr-log path is provided, the CLI SHALL include the log tail in its error output.

#### Scenario: Minimal result with no comments

- **WHEN** the result JSON contains an empty comments array
- **THEN** the CLI posts no review comments and publishes a summary saying the review found no issues

#### Scenario: Malformed JSON with stderr log

- **WHEN** the result file is not valid JSON and the stderr-log file exists
- **THEN** the CLI exits non-zero and its error output includes content from the stderr log

### Requirement: Comment formatting

Every posted inline comment SHALL carry a per-run unique HTML id tag and a `[category · severity]` badge (omitting absent parts), followed by the comment content. When suggestions are enabled (CLI flag `--suggestions`, default `true`) and a comment has `suggestion_code`, the inline body SHALL include suggestion blocks: with both `suggestion_code` and `existing_code`, Before/After fenced code blocks; with `suggestion_code` alone, an After-only fenced code block. Fences SHALL be long enough not to break on embedded backticks. On inline comments, suggestion blocks SHALL be included only when the comment's line span classifies as valid against the diff inventory; when the diff inventory is unavailable or unparseable, gating SHALL fail open and include the suggestion blocks. When suggestions are disabled or the span classifies as invalid (in which case the comment itself is dropped), the inline body SHALL NOT contain suggestion blocks. Comments rendered outside inline position (summary fallback) SHALL render the file path, line span, an optional reason, and collapsible suggestion detail blocks, subject to the same `--suggestions` flag but without diff-validity gating.

#### Scenario: Badge with category and severity

- **WHEN** a comment has category `bug` and severity `critical`
- **THEN** the posted body starts with the id tag line followed by `[bug · critical]` and then the content

#### Scenario: Suggestion fences

- **WHEN** a comment has suggestion_code and existing_code whose content contains triple backticks
- **THEN** the posted Before/After blocks use a longer fence so the content renders intact

#### Scenario: Inline body includes suggestion blocks

- **WHEN** suggestions are enabled and an inline candidate has both suggestion_code and existing_code and its span classifies as valid against the diff inventory
- **THEN** the posted inline body includes Before and After fenced code blocks after the comment content

#### Scenario: Suggestion without existing code

- **WHEN** suggestions are enabled and an inline candidate has suggestion_code but no existing_code and its span classifies as valid
- **THEN** the posted inline body includes an After-only fenced code block

#### Scenario: Invalid span drops suggestions with the comment

- **WHEN** an inline candidate's span classifies as invalid against a complete diff inventory
- **THEN** the comment is dropped from inline posting entirely and lands in the summary with the out-of-diff reason, including its suggestion blocks in the fallback rendering

#### Scenario: Diff unavailable fails open

- **WHEN** the diff inventory is unavailable or unparseable and suggestions are enabled
- **THEN** inline comments that survive posting include their suggestion blocks

#### Scenario: Suggestions disabled by flag

- **WHEN** `--suggestions=false` is set
- **THEN** no posted body (inline or summary fallback) contains suggestion blocks, for any comment regardless of its fields

#### Scenario: Flag default

- **WHEN** the CLI runs without `--suggestions`
- **THEN** suggestion rendering behaves as if `--suggestions=true` were set

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

### Requirement: Stats output

On every exit path the CLI SHALL write a dotenv stats file (default `.ocr/ocr-stats.env`) with mutually exclusive counts `OCR_COMMENTS_TOTAL`, `OCR_COMMENTS_INLINE`, `OCR_COMMENTS_SUMMARY`, `OCR_COMMENTS_ROUTED`, `OCR_COMMENTS_SKIPPED`, `OCR_COMMENTS_FAILED`, and `OCR_SUMMARY_URL`.

#### Scenario: Stats file always written

- **WHEN** posting fails partway through
- **THEN** the stats file still exists with the counts achieved so far

### Requirement: Partition into inline, no-line, and routed

The CLI SHALL partition comments into: inline candidates (path and a valid end_line >= 1 present), no-line (missing path or end_line, routed to summary with reason "No line information provided"), and policy-routed (matching the publication policy, routed to summary with a reason). Counts across the three groups plus skipped and failed MUST be mutually exclusive and sum to the total comment count.

#### Scenario: Comment without line info

- **WHEN** a comment has content but no end_line
- **THEN** it is not posted inline and appears in the summary under "no line information"

### Requirement: Deterministic ordering

Inline comments SHALL be posted in ascending order of (path, start_line, end_line, original index) so identical reruns produce identical post sequences.

#### Scenario: Stable rerun order

- **WHEN** the same result is posted twice
- **THEN** both runs emit comments in the same order

### Requirement: Publication policy routing

When `--route-severity-below` is set to a known severity, comments at-or-below that severity SHALL route to the summary; when `--route-categories` lists known categories, matching comments SHALL route to the summary. Unknown or malformed policy values MUST fail open (no routing). Routed comments SHALL appear in the summary with their routing reason.

#### Scenario: Severity routing

- **WHEN** `--route-severity-below high` is set and a comment has severity `medium`
- **THEN** the comment appears in the summary with reason "Routed to summary (severity medium)" and is not posted inline

#### Scenario: Malformed policy fails open

- **WHEN** `--route-severity-below banana` is set
- **THEN** no comments are routed by policy and all anchorable comments are posted inline

### Requirement: Sticky summary comment

The CLI SHALL publish a summary as an issue comment on the PR carrying the `<!-- ocr-summary -->` marker, created once and updated in place on subsequent runs via the marker lookup (newest matching comment first). The summary SHALL report mutually exclusive counts (total, inline, summary, routed, skipped, failed), then render no-line, routed, and failed comments with reasons, then warnings. Before inline posting begins, the summary SHALL exist with a "posting..." body. A summary-only run (no comments) SHALL publish a "looks good to me" body.

#### Scenario: Second run updates in place

- **WHEN** the CLI runs twice against the same PR
- **THEN** only one summary comment exists after both runs and its body reflects the second run

#### Scenario: Failed comments land in summary

- **WHEN** an inline comment cannot be posted
- **THEN** the final summary includes it with the failure reason and counts it as failed

### Requirement: Incremental mode

When `--incremental` is enabled, the CLI SHALL list prior review comments authored by the bot on the PR and skip inline posting of any comment whose line span overlaps a prior bot comment on the same path (multi-line comments compared by IoU strictly greater than a threshold, default 0.6; single-line by exact line match). Skipped comments SHALL be counted and the summary SHALL note the skips. If prior activity cannot be read, incremental filtering MUST be disabled for the run rather than blocking posting.

#### Scenario: Overlapping comment skipped

- **WHEN** incremental mode is on and a new comment overlaps a prior bot comment on the same file beyond the threshold
- **THEN** the comment is skipped, counted as skipped, and not posted inline

#### Scenario: IoU at threshold is not a match

- **WHEN** two multi-line spans have IoU exactly equal to the threshold
- **THEN** they are not considered the same comment and the new comment is posted

#### Scenario: History read failure disables filtering

- **WHEN** prior review comments cannot be listed
- **THEN** the run proceeds without incremental filtering and logs the degradation

### Requirement: Diff pre-check

The CLI SHALL fetch the PR diff, parse it into per-file new-line ranges, and classify each inline candidate as valid, invalid, or unknown before submitting the review. Comments provably outside the diff (invalid: unknown file, or line range outside all hunks of a complete inventory) SHALL be dropped from inline posting with reason "out of diff" and land in the summary. An incomplete or unavailable inventory MUST classify as unknown and keep the comment.

#### Scenario: Out-of-diff comment dropped before submit

- **WHEN** a comment's end_line falls outside every hunk of the target file and the diff inventory is complete
- **THEN** the comment is not included in the review and appears in the summary as "out of diff"

#### Scenario: Truncated inventory keeps comment

- **WHEN** the diff cannot be fully parsed
- **THEN** classification is unknown and the comment is still attempted inline

### Requirement: Transport retry and dry-run

The CLI SHALL retry failed Forgejo API calls on HTTP 429, 5xx, and network errors with exponential backoff and jitter (default 3 attempts), and SHALL NOT retry on other client errors (4xx). With `--dry-run`, the CLI SHALL perform no network calls and print the review comments and summary it would post.

#### Scenario: Transient 500 retried

- **WHEN** review submission returns 500 twice then succeeds
- **THEN** the run completes successfully with the third attempt

#### Scenario: Dry-run posts nothing

- **WHEN** the CLI runs with `--dry-run` against a valid result
- **THEN** the review comments and summary are printed to stdout and no API write occurs

### Requirement: Pending review cleanup

Before creating a review, the CLI SHALL delete any prior pending (unsubmitted) reviews authored by the authenticated bot user on the target PR, so a previously interrupted run cannot accumulate stale pending reviews.

#### Scenario: Stale pending review from interrupted run

- **WHEN** a previous run crashed after creating a pending review and the CLI runs again
- **THEN** the stale pending review is deleted before the new review is created

### Requirement: Summary resolution tracking

When `--resolution-tracking` is enabled, the CLI SHALL maintain a findings ledger inside the sticky summary comment body, delimited by dedicated HTML markers, recording each inline-posted finding as an open entry keyed by file path and line span. On each run the CLI SHALL match the current run's inline candidates against open ledger entries using the same span matching as incremental mode (exact line match for single-line spans; multi-line spans matching with IoU strictly greater than 0.6 on the same path). Inline candidates are the run's findings with usable line information that were posted inline or remained visible on the pull request without successful posting — incrementally skipped overlaps, diff-pre-check drops, review submit failures, and policy-routed summary comments. Current candidates that match an open entry SHALL keep that entry open; current candidates that match no entry SHALL be added as new open entries exactly when they were successfully posted inline; open entries matched by no current candidate SHALL be marked resolved, recording the resolution. Findings without usable line information never participate in matching and never create entries. When resolution tracking is disabled or the flag is absent, the summary MUST NOT contain the ledger section and existing summary behavior SHALL be unchanged. Ledger entries SHALL only stem from findings that were successfully posted inline; failed, routed, and no-line comments SHALL NOT create entries. When the existing summary cannot be read or its ledger cannot be parsed, tracking SHALL degrade gracefully: the run proceeds and starts a fresh ledger from the current run's findings rather than failing posting.

#### Scenario: Finding persists across runs

- **WHEN** a finding is posted inline in run 1 and the same span is reported again in run 2 with resolution tracking enabled
- **THEN** run 2's summary ledger shows the finding as a single open entry, not a duplicate

#### Scenario: Fixed finding is marked resolved

- **WHEN** an open ledger entry's finding is absent from the current run's results
- **THEN** the entry moves to the resolved state in the ledger and the summary renders it under resolved findings with the resolution marker

#### Scenario: New finding is added to ledger

- **WHEN** the current run posts an inline finding that matches no open ledger entry
- **THEN** a new open entry for that finding appears in the ledger

#### Scenario: Line-shifted finding stays the same entry

- **WHEN** a multi-line finding's span shifts within the IoU threshold between runs
- **THEN** the entry remains open and is not duplicated, and its recorded span is updated to the current run's span

#### Scenario: Tracking disabled leaves format unchanged

- **WHEN** `--resolution-tracking` is not set
- **THEN** the summary contains no ledger section and its body matches the pre-existing format

#### Scenario: Unreadable summary degrades gracefully

- **WHEN** the prior summary comment cannot be listed or its ledger section cannot be parsed
- **THEN** posting proceeds, the run logs the degradation, and the new summary starts a fresh ledger from the current run's findings

#### Scenario: Only posted findings tracked

- **WHEN** a run has failed, routed, or no-line comments
- **THEN** none of those comments appear as open ledger entries

### Requirement: Resolution summary rendering

When resolution tracking is enabled, the sticky summary SHALL render open findings and resolved findings as distinct sections after the existing count reporting: open findings rendered like summary fallback items (path, span, content), resolved findings rendered with their path, span, and a resolved indication. The resolved section SHALL list the findings resolved during this run; resolutions retained from earlier runs persist in the ledger state but are not re-rendered. Resolved entries SHALL be retained in the ledger up to a bounded count (default 20 most recent); older resolved entries SHALL be pruned. A run with no current findings and a non-empty open ledger SHALL resolve all open entries and SHALL NOT present the body as "looks good to me" without noting the resolutions; a run with no current findings and an empty ledger SHALL keep the existing "looks good to me" body. The run stats SHALL include a `OCR_FINDINGS_RESOLVED` variable reporting how many open entries this run resolved, and the summary SHALL state this count when it is greater than zero.

#### Scenario: Summary shows open and resolved sections

- **WHEN** a run with tracking enabled ends with 2 open entries and 1 entry resolved this run
- **THEN** the summary renders the 2 open findings and the 1 resolved finding in separate sections and states that 1 finding was resolved

#### Scenario: All findings resolved on clean run

- **WHEN** a run with tracking enabled and a non-empty open ledger yields zero findings
- **THEN** all open entries are marked resolved in the ledger and the summary reports them as resolved instead of a bare no-issues body

#### Scenario: Clean run with empty ledger

- **WHEN** a run with tracking enabled and an empty ledger yields zero findings
- **THEN** the summary shows the existing no-issues body with no resolved section

#### Scenario: Resolved entries bounded

- **WHEN** the ledger already holds the maximum retained resolved entries and another entry is resolved
- **THEN** the oldest resolved entry is pruned from the ledger

#### Scenario: Resolved count in stats file

- **WHEN** a run with tracking enabled resolves 3 open entries
- **THEN** the stats file contains `OCR_FINDINGS_RESOLVED=3` (and `OCR_FINDINGS_RESOLVED=0` when none are resolved)
