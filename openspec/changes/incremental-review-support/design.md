# Design

## Context

ocr-forgejo posts one Forgejo review per run plus a sticky summary (marker
`<!-- ocr-summary -->`, with a per-run tag variant in `format.PerRunMarkerFmt`,
`internal/format/format.go`). Resolution tracking keeps a findings ledger in
that summary. Today every CI run reviews the full PR range, so the ledger's
"not re-reported ⇒ resolved" rule and `decideState`'s "zero findings ⇒
APPROVED" rule are sound. Once the workflow starts running ocr over a delta
range (planned in the task-ci/homelab side), those rules break: untouched
findings would be claimed as resolved and a clean delta run would approve the
PR. See proposal.md for the full motivation.

## Goals / Non-Goals

**Goals:**

- Record the reviewed head SHA on successful runs so callers can compute a
  delta range next time.
- Provide a read-only query mode for that SHA, safe to call from advisory CI.
- Make ledger resolution coverage-aware so delta runs only resolve findings
  whose code provably changed.
- Never approve while open findings remain, even on a clean delta run.
- Zero behavior change when the new flags are absent.

**Non-Goals:**

- Workflow-side wiring in task-ci.yaml (homelab repo, separate change).
- ocr `--resume` sessions, budget tuning, `--incremental` overlap semantics.
- Ledger format changes beyond the coverage rule.
- Teaching the query mode about per-run (non-sticky) summaries.

## Decisions

**D1 — Marker as a formatted constant in `internal/format`.**
`const LastReviewedMarkerFmt = "<!-- ocr-last-reviewed:%s -->"`, value =
`result.Manifest.Input.ResolvedHead`. Written in the success paths of
`internal/publish/publish.go` (approve body and no-issues body), never in
`publishFailed`; omitted when `ResolvedHead` is empty. Alternative considered:
a separate comment — rejected, the sticky summary already exists and updating
it is atomic with the ledger.

**D2 — Query mode reuses the summary lookup, not new parsing infrastructure.**
`--print-last-reviewed` walks `p.ListIssueComments()` (the poster interface
already supports pagination for sticky lookup via `findSummaryComment`,
`publish.go:547`), scans for the summary marker, then extracts the
last-reviewed marker with a regex. No posting, dry-run style. Failure ⇒ empty
stdout + exit 0 by design (D5 of the proposal): the review step is advisory
and a query failure must not break it. Alternative: workflow-side curl+grep —
rejected per user decision; keeps the marker format a private contract of this
binary.

**D3 — Coverage via `internal/diffinv` and a file path, not an inline diff.**
`--coverage-diff <path>` points at a unified diff written by the workflow of
the exact range reviewed (`git diff <merge-base> refs/pr-head`). Parse with
`diffinv.New` and classify each unmatched open entry with `diffinv.Classify`
on the entry's `Class` span semantics (same Range semantics as the existing
diff pre-check). Valid ⇒ resolve; Invalid/Unknown/out-of-diff ⇒ keep open.
Unreadable/unparseable file ⇒ "no coverage": nothing resolves, warn on stderr.
Absent flag ⇒ nil coverage ⇒ full-coverage semantics, byte-identical behavior.
Known conservative edge: a fix that purely deletes the offending lines can
classify Invalid (hunk new-line ranges shrink past the span) — the entry stays
open until a full review or re-report. Accepted and documented; never
wrongly resolves.

**D4 — Approval downgrade in `decideState`.**
`decideState` gains the surviving-open count: with resolution tracking on,
`countFindings == 0` and `openSurvivors > 0` ⇒ COMMENT plus a summary note
"delta clean, N findings remain open"; `openSurvivors == 0` ⇒ APPROVED as
before. Computed from the merged ledger before the state is submitted.

**D5 — Empty-output query mode exits 0.**
Mirrors advisory-step requirements; Argo `continueOn` semantics stay untouched.

## Risks / Trade-offs

- [Purely-deleted lines classify Invalid so their entries linger] →
  Documented conservative edge; a later full review or re-report clears them.
- [Marker drift if the summary body is regenerated on a path that omits it] →
  Marker write is centralized in the summary body construction for success
  paths; tests pin presence after success and absence after `publishFailed`.
- [Query mode pagination cost on PRs with many comments] → Reuse the same
  pagination approach as sticky lookup (newest-first scan, stop at first
  summary-marker match).
- [Coverage diff from a force-push covers rewritten files, resolving entries
  in moved code] → Explicit merge-base diff is a superset of new changes; only
  in-diff, non-re-reported entries resolve, which is the intended semantics.

## Migration Plan

1. Ship the new flags and marker (this change); behavior with old workflows is
   unchanged because flags default to absent.
2. Tag/release the binary, rebuild the reviewer image, and bump the workflow
   default (homelab repo change).
3. Rollback: revert the workflow to full-range invocation (no
   `--coverage-diff`); the binary's old behavior is fully preserved by the
   absent-flag semantics.

## Open Questions

None — the marker format, query-mode failure semantics, coverage rule, and
downgrade rule were confirmed with the user.
