# Design

## Context

The sticky summary (`<!-- ocr-summary -->` marker) is already created once and updated in place every run (`findSummaryComment`/`upsertSummary` in `internal/publish/publish.go`), making it the only durable, bot-writable state on the PR. Incremental mode already has span matching (`internal/incremental`: exact single-line match, IoU > 0.6 for multi-line, path-scoped) and history loading is keyed on the `format.BotMarker` in comment bodies. Forgejo has no resolve-conversation API (see proposal.md — Why), so the summary body is where cross-run resolution state must live.

## Goals / Non-Goals

**Goals:**
- Persistent, machine-parseable findings ledger inside the summary body
- Open/resolved progression visible to human reviewers
- Graceful degradation when prior state is unreadable
- Zero behavior change when the flag is off

**Non-Goals:**
- Resolving Forgejo conversations via API (deferred until Forgejo ships resolve/unresolve endpoints; a future change will add the probe + Poster methods and can then reconcile ledger state with real conversation resolution)
- Tracking failed, routed, or no-line comments
- Per-comment replies, reactions, or edits to past reviews
- Persisting ledger state anywhere other than the summary comment

## Decisions

### D1: Ledger lives in the summary body behind its own HTML marker

The ledger is a separate delimited block inside the sticky summary, e.g. wrapped in `<!-- ocr-ledger -->` ... `<!-- /ocr-ledger -->`, containing a JSON payload (path, start/end line, first-seen run, resolved run, comment content snippet). The human-readable open/resolved sections are rendered *outside* the marker from the same data.

- *Why JSON inside HTML comment*: invisible in rendered Markdown, trivially parseable, versionable (add a `version` field), and confined to a comment we already own and rewrite atomically on every run. The existing `<!-- ocr-summary -->` marker pattern is precedent.
- *Alternative*: scrape our own rendered Markdown — brittle against formatting tweaks; rejected.
- *Alternative*: second hidden issue comment for state — two comments must stay consistent across failed edits; rejected.

### D2: Matching reuses incremental span semantics

Ledger matching uses `incremental.Span` / `SameCommentSpan` with the same default threshold (0.6), scoped by path. On a match, the entry's recorded span is refreshed to the current finding's span so slow line drift does not accumulate into a mismatch.

- *Why*: one matching semantics to test and reason about; the two features answer the same question ("is this the same finding as before?").
- *Alternative*: content-hash keys — a reformatted line would orphan the entry; rejected.

### D3: Only successfully posted inline findings enter the ledger

An entry is created only after the review submit succeeds, from the posted set. Failed/routed/no-line comments are transient reporting, not findings with a lifecycle to track.

- *Why*: keeps the ledger a promise ("this was flagged inline and is still open"), avoids resolving entries for comments that never landed.
- Note: this means ledger population happens after `SubmitPullReview`, using the same `inline` slice that produced the posted comments.

### D4: Flag-gated, default off

`--resolution-tracking` (default false). When off, no marker, no ledger, no rendering changes — bit-identical summary bodies to today.

- *Why*: existing Argo consumers parse the summary/stats; opt-in avoids any surprise. Promoting to default can be a later, separate decision.

### D5: Bounded ledger, resolution recorded per entry

Resolved entries keep a resolved-at run tag and date, capped at the 20 most recent (constant, not configurable for now). Open entries are unbounded — a pathological PR with hundreds of unfixed findings renders a long summary, which is honest reporting.

- *Why cap*: the summary is rewritten wholesale each run; without a cap it grows forever on long-lived PRs. 20 covers typical review horizons.
- *Rendering scope*: the summary's resolved section lists the findings resolved during the current run only; older resolutions stay in the ledger state (visible in the hidden JSON block) without re-rendering, so the visible resolution story stays tied to recent progression.
- *Alternative*: prune by age/runs — more knobs, no added value yet.

### D6: Failure semantics — fail open, never block posting

If listing issue comments fails or the ledger JSON is corrupt/truncated (e.g. a partial edit), log a degradation, discard old state, and seed a fresh ledger from this run. This mirrors the existing incremental and diff-pre-check failure posture. Parsing lives in `internal/format` next to the markers it defines; `internal/publish` orchestrates read → match → merge → render around the existing anchor/upsert flow.

### D7: Stats extension, partition untouched

`Stats` gains a `Resolved` count (cross-run, deliberately outside the mutually-exclusive total partition) exposed as `OCR_FINDINGS_RESOLVED`. The existing `OCR_COMMENTS_*` contract is unchanged.

## Risks / Trade-offs

- [Concurrent runs on the same PR race on the summary body] → Argo runs per-PR are serialized today; last-writer-wins is accepted. Document in tasks; no locking mechanism.
- [Ledger drift when a human deletes or edits the summary] → parse failure or missing marker degrades to fresh ledger; findings re-enter next run.
- [Span drift beyond IoU threshold duplicates an entry] → same residual risk as incremental mode; span refresh (D2) minimizes accumulation; accepted.
- [Summary grows with open findings] → open entries uncapped by design (honest reporting); resolved entries capped.
- [Thread 2 future integration] → when Forgejo ships resolve endpoints, ledger entries can gain a `resolved_by` field to distinguish auto-detected vs API-resolved; JSON versioning (D1) leaves room.

## Migration Plan

Opt-in flag means no migration. Rollback = stop passing the flag; the last summary with a ledger remains but is inert (markers ignored by the non-tracking renderer... note: the non-tracking path must leave an existing ledger block intact or strip it — strip it, so the summary returns to the classic format; recorded in tasks).

## Open Questions

None.
