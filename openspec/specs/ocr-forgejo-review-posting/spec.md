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

On every exit path the CLI SHALL write a dotenv stats file (default `.ocr/ocr-stats.env`) with mutually exclusive counts `OCR_COMMENTS_TOTAL`, `OCR_COMMENTS_INLINE`, `OCR_COMMENTS_SUMMARY`, `OCR_COMMENTS_ROUTED`, `OCR_COMMENTS_SKIPPED`, `OCR_COMMENTS_FAILED`, the count `OCR_FINDINGS_RESOLVED`, the summary URL `OCR_SUMMARY_URL`, and a run-status line `OCR_RUN_STATUS` whose value is `failed` when the result JSON reported `status: "failed"` and `completed` otherwise.

#### Scenario: Stats file always written

- **WHEN** posting fails partway through
- **THEN** the stats file still exists with the counts achieved so far

#### Scenario: Stats file marks a failed run

- **WHEN** the CLI publishes the failure note for a result JSON with `status: "failed"`
- **THEN** the stats file exists with `OCR_RUN_STATUS=failed` and `OCR_COMMENTS_TOTAL=0`

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

When `--resolution-tracking` is enabled, the CLI SHALL maintain a findings ledger inside the sticky summary comment body, delimited by dedicated HTML markers, recording each inline-posted finding as an open entry keyed by file path and line span. On each run the CLI SHALL match the current run's inline candidates against open ledger entries using the same span matching as incremental mode (exact line match for single-line spans; multi-line spans matching with IoU strictly greater than 0.6 on the same path). Inline candidates are the run's findings with usable line information that were posted inline or remained visible on the pull request without successful posting — incrementally skipped overlaps, diff-pre-check drops, review submit failures, and policy-routed summary comments. Current candidates that match an open entry SHALL keep that entry open; current candidates that match no entry SHALL be added as new open entries exactly when they were successfully posted inline. Findings without usable line information never participate in matching and never create entries. When resolution tracking is disabled or the flag is absent, the summary MUST NOT contain the ledger section and existing summary behavior SHALL be unchanged. Ledger entries SHALL only stem from findings that were successfully posted inline; failed, routed, and no-line comments SHALL NOT create entries. When the existing summary cannot be read or its ledger cannot be parsed, tracking SHALL degrade gracefully: the run proceeds and starts a fresh ledger from the current run's findings rather than failing posting.

When `--coverage-diff <path>` is provided, the CLI SHALL parse the file as a unified diff covering exactly the range this run reviewed and SHALL only resolve an unmatched open entry when the diff inventory classifies the entry's path and line span as valid (the code under the finding changed and the reviewer did not re-report it). Entries classified invalid or unknown, or whose span lies outside the coverage diff, SHALL remain open, recording that they were not re-reviewed rather than resolved. When the coverage diff is absent, unreadable, or unparseable, the CLI SHALL treat the run as having no coverage: no unmatched open entry SHALL be resolved (conservative), with a logged warning, and the run MUST NOT fall back to full-coverage resolution semantics. When `--coverage-diff` is not provided at all, the existing full-coverage resolution semantics SHALL be unchanged: unmatched open entries resolve as before, including the clean-run bulk resolution.

#### Scenario: Finding persists across runs

- **WHEN** a finding is posted inline in run 1 and the same span is reported again in run 2 with resolution tracking enabled
- **THEN** run 2's summary ledger shows the finding as a single open entry, not a duplicate

#### Scenario: Fixed finding is marked resolved

- **WHEN** an open ledger entry's finding is absent from the current run's results, no coverage diff is provided, and the run reviewed the full PR range
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

#### Scenario: In-coverage entry resolves on delta run

- **WHEN** a delta run provides a coverage diff, an open ledger entry's path and span classify as valid in that diff, and the run does not re-report the finding
- **THEN** the entry is marked resolved

#### Scenario: Out-of-coverage entry stays open on delta run

- **WHEN** a delta run provides a coverage diff and an open ledger entry's path and span are not covered by any hunk of that diff
- **THEN** the entry remains open in the ledger

#### Scenario: Re-reported entry stays open on delta run

- **WHEN** a delta run re-reports a finding that matches an open ledger entry, regardless of coverage
- **THEN** the entry stays open with its span and content refreshed

#### Scenario: Unparseable coverage diff resolves nothing

- **WHEN** a coverage diff path is provided but the file is unreadable or not a parseable unified diff
- **THEN** no unmatched open entries are resolved this run, the run logs a warning, and re-reported entries still keep their entries open

#### Scenario: No coverage flag keeps full-coverage semantics

- **WHEN** a run with tracking enabled provides no `--coverage-diff` flag and an open entry is not re-reported
- **THEN** the entry resolves exactly as in previous releases

### Requirement: Resolution summary rendering

When resolution tracking is enabled, the sticky summary SHALL render open findings and resolved findings as distinct sections after the existing count reporting: open findings rendered like summary fallback items (path, span, content), resolved findings rendered with their path, span, and a resolved indication. The resolved section SHALL list the findings resolved during this run; resolutions retained from earlier runs persist in the ledger state but are not re-rendered. Resolved entries SHALL be retained in the ledger up to a bounded count (default 20 most recent); older resolved entries SHALL be pruned. A run with no current findings and a non-empty open ledger SHALL resolve all entries permitted by the coverage rules above and SHALL NOT present the body as "looks good to me" without noting the resolutions; a run with no current findings and an empty ledger SHALL keep the existing "looks good to me" body. The run stats SHALL include a `OCR_FINDINGS_RESOLVED` variable reporting how many open entries this run resolved, and the summary SHALL state this count when it is greater than zero.

#### Scenario: Summary shows open and resolved sections

- **WHEN** a run with tracking enabled ends with 2 open entries and 1 entry resolved this run
- **THEN** the summary renders the 2 open findings and the 1 resolved finding in separate sections and states that 1 finding was resolved

#### Scenario: All findings resolved on clean run

- **WHEN** a run with tracking enabled and a non-empty open ledger yields zero findings and no coverage diff restricts resolution
- **THEN** all open entries are marked resolved in the ledger and the summary reports them as resolved instead of a bare no-issues body

#### Scenario: Clean delta run keeps out-of-coverage entries open

- **WHEN** a clean delta run with a coverage diff yields zero findings but open entries exist that the coverage diff does not cover
- **THEN** those entries remain open in the ledger, the summary does not use the bare no-issues body, and no resolution is claimed for them

#### Scenario: Clean run with empty ledger

- **WHEN** a run with tracking enabled and an empty ledger yields zero findings
- **THEN** the summary shows the existing no-issues body with no resolved section

#### Scenario: Resolved entries bounded

- **WHEN** the ledger already holds the maximum retained resolved entries and another entry is resolved
- **THEN** the oldest resolved entry is pruned from the ledger

#### Scenario: Resolved count in stats file

- **WHEN** a run with tracking enabled resolves 3 open entries
- **THEN** the stats file contains `OCR_FINDINGS_RESOLVED=3` (and `OCR_FINDINGS_RESOLVED=0` when none are resolved)

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

### Requirement: Last-reviewed marker

The CLI SHALL embed an HTML marker `<!-- ocr-last-reviewed:<sha> -->` in the sticky summary body on success paths only, where `<sha>` is the reviewed head SHA from `manifest.input.resolved_head`. The marker SHALL be written when the run submits an approval or publishes a no-issues summary, and MUST NOT be written on any failed-run path (including failed-run publication). When the resolved head SHA is absent or empty, the summary SHALL be published without the marker. The marker SHALL be preserved by ledger re-encoding and summary updates on later runs as long as the body sections that carry it are not regenerated from a path that omits it.

#### Scenario: Marker written on approval

- **WHEN** a successful run with findings submits a review and updates the sticky summary with a resolved head SHA present in the result manifest
- **THEN** the summary body contains `<!-- ocr-last-reviewed:<sha> -->` with that SHA

#### Scenario: Marker absent on failed run

- **WHEN** the result JSON reports `status: "failed"` and the failure-note summary is published
- **THEN** the summary body contains no last-reviewed marker

#### Scenario: Marker omitted without resolved head

- **WHEN** a successful run's result JSON has no `manifest.input.resolved_head` value
- **THEN** the summary body contains no last-reviewed marker, and all other summary behavior is unchanged

### Requirement: Last-reviewed query mode

The CLI SHALL support a `--print-last-reviewed` mode: it locates the sticky summary comment (the comment carrying the summary marker, newest match first, paginating the issue-comment listing as needed), extracts the SHA from the last-reviewed marker, and prints it to stdout. The mode SHALL authenticate with the existing `GIT_TOKEN` environment variable and use the existing `--url`, `--repo`, and `--pr` flags. When no sticky summary exists, the summary carries no last-reviewed marker, or the Forgejo API fails, the CLI SHALL print nothing (empty output) and exit 0 — a query failure MUST NOT surface as an error, so callers can fall back to a full review.

The CLI SHALL reject `--coverage-diff` without `--resolution-tracking` with a fail-fast flag-validation error, because coverage-aware approval safety depends on the ledger; a bare coverage diff without tracking would let a clean delta run approve while only the delta was reviewed.

#### Scenario: Marker found

- **WHEN** the sticky summary contains `<!-- ocr-last-reviewed:abc123 -->` and the CLI runs with `--print-last-reviewed`
- **THEN** the CLI prints `abc123` to stdout and exits 0

#### Scenario: No marker

- **WHEN** no sticky summary comment exists on the PR, or the sticky summary has no last-reviewed marker
- **THEN** the CLI prints nothing and exits 0

#### Scenario: API failure degrades to empty output

- **WHEN** the Forgejo issue-comment listing fails during `--print-last-reviewed`
- **THEN** the CLI prints nothing, exits 0, and logs the failure to stderr

#### Scenario: Query mode does not post

- **WHEN** the CLI runs with `--print-last-reviewed`
- **THEN** it creates no reviews, comments, or summary updates on the PR

#### Scenario: Coverage diff without tracking is rejected

- **WHEN** the CLI is invoked with `--coverage-diff` but without `--resolution-tracking`
- **THEN** the CLI fails fast with a flag-validation error before any Forgejo call
