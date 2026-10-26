# Spec Delta: ocr-forgejo-review-posting

## ADDED Requirements

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

## MODIFIED Requirements

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
