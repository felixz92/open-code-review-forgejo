# Proposal

## Why

Inline review comments currently drop `suggestion_code` entirely: only comments rendered in the summary fallback show Before/After blocks, and the implementation already deviates from the existing spec requirement that mandates Before/After blocks on inline bodies. Since the comments are consumed by another agent, surfacing the suggested fix inline is the highest-value content the reviewer can act on.

## What Changes

- Inline comment bodies SHALL include suggestion blocks (Before/After fenced code) when the comment carries `suggestion_code`.
- Suggestion blocks on inline comments SHALL be gated on the diff pre-check: they are included only when the comment's line span classifies as valid against the PR diff inventory.
- When the diff inventory is unavailable or unparseable, gating fails open: suggestions are included (matching the existing fail-open behavior of the diff pre-check for comments themselves).
- A new `--suggestions` CLI flag (default `true`) controls whether suggestion blocks are rendered at all; when `false`, inline and summary-fallback bodies omit them.
- `existing_code` becomes optional for suggestion rendering: with both fields, Before/After blocks are rendered; with only `suggestion_code`, an After-only block is rendered.
- This brings the implementation into compliance with the existing "Comment formatting" requirement, which is amended to specify the gating and flag behavior.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `ocr-forgejo-review-posting`: The "Comment formatting" requirement changes to specify suggestion block rendering on inline comments, diff-validity gating, fail-open behavior when the diff is unavailable, and the `--suggestions` flag (default true).

## Impact

- `internal/format` (`FormatComment`, `SuggestionBlocks`): render suggestion blocks on inline bodies; relax the both-fields requirement.
- `internal/publish` (`Publish`, `Options`): build/append suggestion content gated on `diffinv.Classify` results; new `Suggestions` option.
- `cmd/ocr-forgejo`: new `--suggestions` flag, default true.
- Tests in `internal/format` and `internal/publish`; no changes to the `ocr` JSON contract, Forgejo posting model, or stats file.
