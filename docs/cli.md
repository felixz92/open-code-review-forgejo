# CLI reference: `ocr-forgejo`

`ocr-forgejo` reads open-code-review (`ocr review --format json`) results and
publishes them to a Forgejo pull request as one review with inline comments
plus a sticky summary comment. This page is the authoritative CLI reference;
the [README](../README.md) only shows a quick-start example.

## Synopsis

```
ocr-forgejo [flags] [input.json]
```

- The positional `input.json` is the review result JSON path; it defaults to
  `.ocr/ocr-result.json`. Exactly one positional argument is allowed.
- Flags may appear before or after the positional argument. Boolean flags may
  be written as `--flag` or `--flag=true` anywhere; other flags consume the
  next non-flag token as their value.
- `--` stops flag parsing; everything after it is treated as the positional
  argument.
- `--help` (or `-h`) prints the flag list.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | (required) | Forgejo base URL, e.g. `https://forge.example.com` |
| `--repo` | (required) | Repository as `owner/name` |
| `--pr` | (required) | Pull request index |
| `--token` | `$GIT_TOKEN` | API token (required unless `--dry-run`) |
| `--input` | `.ocr/ocr-result.json` | Review result JSON path |
| `--stderr-log` | `.ocr/ocr-stderr.log` | OCR stderr log, included in errors on parse failure |
| `--stats-file` | `.ocr/ocr-stats.env` | dotenv output for posting stats |
| `--dry-run` | `false` | Print review comments and summary instead of posting |
| `--sticky-summary` | `true` | Update the summary comment in place across runs |
| `--incremental` | `false` | Skip findings overlapping prior bot review comments |
| `--incremental-overlap-threshold` | `0.6` | IoU threshold for the overlap test (strictly greater) |
| `--route-severity-below` | `""` | Route findings at-or-below this severity to the summary |
| `--route-categories` | `""` | Comma-separated categories to route to the summary |
| `--approve-below` | `low` | Findings above this severity block approval (request changes); the named severity and below approve |
| `--run-tag` | `<pr>-<head sha>` | Per-run identity for idempotency tags |
| `--suggestions` | `true` | Include Before/After suggestion blocks in comment bodies |
| `--resolution-tracking` | `false` | Maintain an open/resolved findings ledger in the sticky summary across runs |

### Validation

The following constraints are checked before anything is posted (violations
exit with code `2`):

- `--url`, `--repo`, and `--pr` are required.
- `--repo` must be `owner/name` (both parts non-empty).
- `--resolution-tracking` requires the sticky summary
  (`--resolution-tracking` without `--sticky-summary` is an error): the
  cross-run ledger lives in the sticky summary body.
- `--incremental-overlap-threshold` must be in `(0, 1)` when `--incremental`
  or `--resolution-tracking` is enabled.

### Token

- `--token` falls back to the `GIT_TOKEN` environment variable when unset.
- A token is only required when posting: `--dry-run` runs without a token are
  allowed.
- A missing token on a posting run exits with code `2`.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Posting failure (Forgejo client setup, input load, or publish error); a zero-filled or partially filled stats file is written |
| `2` | Usage/config error (flag parsing, validation, missing token) |

## Stats file

Always written, zero-filled on early exit (parse/validation errors), before
`run` starts. Keys:

```
OCR_COMMENTS_TOTAL=1
OCR_COMMENTS_INLINE=1
OCR_COMMENTS_SUMMARY=0
OCR_COMMENTS_ROUTED=0
OCR_COMMENTS_SKIPPED=0
OCR_COMMENTS_FAILED=0
OCR_FINDINGS_RESOLVED=0
OCR_SUMMARY_URL=comment:1
```

`OCR_SUMMARY_URL` is the URL (or `comment:<id>` placeholder) of the sticky
summary comment; empty when no summary was posted.

## Severity and category reference

- **Severities** (least to most severe): `low`, `medium`, `high`, `critical`.
  Findings with an unknown severity are treated as blocking for approval
  (fail-safe).
- **Categories**: `bug`, `security`, `performance`, `maintainability`, `test`,
  `style`, `documentation`, `other`.
- `--route-severity-below` / `--route-categories` only recognize these known
  values; unknown values deactivate routing instead of failing the run.
- `--approve-below` names the highest severity that does not block approval;
  findings above it (and unknown severities) force `REQUEST_CHANGES`. An
  empty value defaults to `low`; an unknown value (e.g. a typo) fails safe by
  making every known-severity finding blocking.

## Notes on selected flags

### `--incremental`

Recommended for the standard PR review pipeline (every push to a PR): it
skips findings that overlap comments the bot already posted, so repeated runs
don't pile up duplicate inline comments. Overlap matching is exact line
equality for single-line spans and IoU strictly greater than
`--incremental-overlap-threshold` for multi-line spans. If prior bot activity
can't be read, the filter silently disables for that run and everything is
posted. Leave it off for one-off or differently-configured re-reviews, where
overlap is not a reliable signal that a finding is a duplicate.

### `--resolution-tracking`

Maintains an open/resolved findings ledger inside the sticky summary across
runs. Requires `--sticky-summary` (see [Validation](#validation)); the IoU
threshold is validated for tracking runs too. Resolved counts are reported in
the stats file as `OCR_FINDINGS_RESOLVED`.

### `--run-tag`

Defaults to `<pr index>-<first 8 chars of the reviewed head SHA>` (from the
input manifest; falls back to `unknown` when the head SHA is not recorded),
so reruns of the same revision share one identity and idempotency tags stay
stable across retries.
