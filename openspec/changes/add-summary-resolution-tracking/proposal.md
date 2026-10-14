# Proposal: Summary-based resolution tracking

## Why

Fixed findings are never marked as resolved: inline review comments stay on the PR (rendered "outdated" only by Forgejo), and each run's sticky summary replaces the previous one, so nobody can tell which findings are still open and which were fixed. Forgejo lacks a resolve-conversation API endpoint, so the sticky summary is the only viable place to carry resolution state across runs.

## What Changes

- The sticky summary comment gains a machine-readable findings ledger (an HTML-marker-delimited section) that persists unresolved findings across runs, keyed by path + line span.
- On each run, current OCR findings are matched against the ledger (same span matching as incremental mode: exact line for single-line, IoU > 0.6 for multi-line). Matched entries stay open; current findings not in the ledger are added; ledger entries not matched by any current finding are marked resolved with the run/date of resolution.
- The summary renders two finding sections: open findings (unresolved) and recently resolved findings, so a reviewer sees progression without reading old reviews.
- A no-findings run resolves all open ledger entries instead of reporting a bare "looks good to me" when the ledger is non-empty.
- Opt-in via a new `--resolution-tracking` CLI flag (default off) so existing consumers of the summary format are unaffected.
- Cross-run resolution state lives entirely in the summary comment body; no new server-side state, no review-comment mutation.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `ocr-forgejo-review-posting`: add a requirement for resolution tracking in the sticky summary — ledger persistence, matching, resolution marking, rendering of open/resolved sections, flag gating, and stats output of the resolved count.

## Impact

- `internal/publish` (publish.go): parse/merge/render the ledger around the existing summary upsert; new resolved count in `Stats`.
- `internal/format`: ledger markers, ledger serialization/parsing, summary sections for open/resolved findings.
- `internal/incremental`: reuse `Span`/`SameCommentSpan` matching for ledger matching (no behavior change to existing incremental requirement).
- `cmd/ocr-forgejo`: new `--resolution-tracking` flag; new stats-file variable `OCR_FINDINGS_RESOLVED`.
- No new dependencies; no API additions; existing summary format unchanged when the flag is off.
