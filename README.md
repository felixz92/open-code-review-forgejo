# ocr-forgejo

A Go CLI that publishes [open-code-review](https://github.com/alibaba/open-code-review)
(`ocr review --format json`) results onto a Forgejo pull request as a review with
inline comments plus a sticky summary. Designed to run from Argo Workflows.

## How it works

- One **review per run**: all anchorable comments are batched into a single
  Forgejo review (pending) and submitted with `COMMENT` (see
  [ADR 0001](docs/adr/0001-single-review-posting.md)).
- Comments anchor at their `end_line`; multiline spans are stated in the body.
- A **sticky summary** issue comment (`<!-- ocr-summary -->` marker) is created
  once and updated in place across runs.
- **Incremental mode** (opt-in) skips findings overlapping prior bot review
  comments (IoU over line spans).
- **Publication policy** (opt-in) routes low-severity or selected-category
  findings to the summary instead of posting them inline.
- A **diff pre-check** classifies each comment against the PR diff before
  submitting; provably out-of-diff comments land in the summary with a reason.
- Stale **pending** reviews of the bot are deleted before posting, so
  interrupted runs never accumulate.
- Writes a **stats dotenv file** and can gate the job on severity.

## Usage

```
ocr-forgejo [flags] [input.json]
```

`input.json` defaults to `.ocr/ocr-result.json`. Flags must precede the
positional argument.

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
| `--fail-on-severity` | `""` | Exit non-zero when any finding is at-or-above this severity |
| `--run-tag` | `<pr>-<head sha>` | Per-run identity for idempotency tags |

Exit codes: `0` success, `1` posting failure, `2` usage/config error, `3`
severity gate tripped.

### Stats file

Always written (zero-filled on early exit):

```
OCR_COMMENTS_TOTAL=1
OCR_COMMENTS_INLINE=1
OCR_COMMENTS_SUMMARY=0
OCR_COMMENTS_ROUTED=0
OCR_COMMENTS_SKIPPED=0
OCR_COMMENTS_FAILED=0
OCR_SUMMARY_URL=comment:1
```

## Argo Workflows example

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: ocr-review-
spec:
  entrypoint: review
  templates:
    - name: review
      steps:
        - - name: review
            template: ocr
        - - name: post
            template: post
    - name: ocr
      container:
        image: ghcr.io/alibaba/open-code-review:latest
        command: [sh, -c]
        args: ["ocr review --format json > .ocr/ocr-result.json 2> .ocr/ocr-stderr.log"]
    - name: post
      container:
        image: ghcr.io/felixz92/ocr-forgejo:latest
        command: [ocr-forgejo]
        args:
          - --url
          - https://forge.example.com
          - --repo
          - my-org/my-repo
          - --pr
          - "42"
          - --incremental
          - --fail-on-severity
          - critical
        env:
          - name: GIT_TOKEN
            valueFrom:
              secretKeyRef:
                name: forgejo-token
                key: token
        workingDir: /work
        volumeMounts:
          - name: work
            mountPath: /work
  volumes:
    - name: work
      emptyDir: {}
```

The token should belong to a dedicated bot user; it needs write access to the
repository's pull requests.

## Development

```
go build ./...
go test ./...
go vet ./...
```

## License

Apache-2.0 (matching the upstream open-code-review example).
