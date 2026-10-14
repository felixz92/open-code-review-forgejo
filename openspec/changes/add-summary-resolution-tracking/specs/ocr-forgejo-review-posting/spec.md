# Spec Delta

## ADDED Requirements

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
