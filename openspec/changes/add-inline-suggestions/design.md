# Design

## Context

`internal/format.FormatComment` builds inline bodies without suggestions; `SuggestionBlocks` renders Before/After only in `FormatCommentFallback`. `internal/publish` already runs a diff pre-check that classifies every inline candidate via `diffinv.Classify` and drops comments with invalid spans (`publish.go:146-154`). Comments anchor at `end_line`; the span statement is appended by `FormatComment` when start != end. See proposal.md for motivation and the spec delta for required behavior.

## Goals / Non-Goals

- Goals: suggestions visible on inline comments; gated on diff validity; controlled by `--suggestions` (default true); spec compliance for the existing formatting requirement.
- Non-Goals: no Forgejo native `suggestion` blocks or Apply buttons (consumer is an agent, not a human); no change to the `ocr` JSON contract; no policy routing integration; no changes to stats counts.

## Decisions

- **D1: Gate via existing diff pre-check, not a second classification.** The pre-check loop already calls `inv.Classify(path, start, end)` per inline item. Suggestion inclusion reuses that classification: valid (or unknown, i.e. fail-open) -> include, invalid -> the comment is dropped entirely anyway. No extra diff parsing. Alternative: a separate gating pass in `format` — rejected because format would need diff knowledge, breaking its pure-render role.
- **D2: Fail-open when the diff is unavailable or unparseable.** Matches the pre-check's existing behavior (unknown keeps the comment). Since the consumer is an agent, a stale suggestion is a minor quality issue, not a destructive action. Alternative fail-closed (strip suggestions) silently removes the most useful content exactly when confidence is lowest.
- **D3: Body assembly moves after the pre-check.** Currently `body` is rendered before classification (`publish.go:94`). Restructure: keep `FormatComment` for the non-suggestion part (id, badge, content, span) at partition time, and append the suggestion suffix in the pre-check loop when `Suggestions` is on and classification is not Invalid. Alternative: render everything after the pre-check — rejected because the body is used by earlier stages (incremental logging) conceptually and the append keeps the diff minimal.
- **D4: `existing_code` becomes optional.** `SuggestionBlocks` currently requires both fields. Relax: both fields -> Before/After; `suggestion_code` only -> After-only block (existing_code without suggestion_code still renders nothing). Recorded as an assumption from exploration (user left it open); it strictly adds value for an agent reader.
- **D5: Flag plumbing.** `--suggestions` bool flag, default true -> `publish.Options.Suggestions`. When false, `FormatComment`/`FormatCommentFallback` receive a rendering mode that omits suggestion blocks; the flag governs both inline and fallback rendering so behavior is uniform.
- **D6: Format API.** Extend `format` with a small options/flag parameter (e.g. `FormatComment(c, id, start, end, withSuggestions bool)` or an options struct) rather than a global; `publish` passes the resolved flag. Keeps rendering pure and testable.

## Risks / Trade-offs

- [Stale suggestion applied by the consuming agent] -> gated on diff-valid span; agent still sees reviewed content context; fail-open only when no better information exists.
- [Body text changes break incremental matching] -> matching keys on the per-run HTML id marker, not body text; no impact.
- [Markdown size growth of review payload] -> suggestions are already carried in fallback rendering; fence length logic is unchanged; no practical limit concern at current review sizes.
- [Assumption: After-only rendering when existing_code is missing] -> revisiting means reverting to both-required; isolated to one function and its tests.

## Migration Plan

Single binary release; no data migration. Rollback: `--suggestions=false` restores prior bodies.

## Open Questions

(none — fail-open, flag default, and existing_code-optional are resolved per exploration and recorded above; D4 remains a revisitable assumption.)
