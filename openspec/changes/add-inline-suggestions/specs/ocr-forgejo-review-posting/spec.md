# Spec Delta

## MODIFIED Requirements

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
