# ocr-forgejo

A Go CLI that publishes [open-code-review](https://github.com/alibaba/open-code-review)
(`ocr review --format json`) results onto a Forgejo pull request as a review with
inline comments plus a sticky summary. Designed to run from Argo Workflows.

## How it works

- One **review per run**: all anchorable comments are batched into a single
  Forgejo review (pending) and submitted with the computed review state:
  `APPROVED` on clean runs, `REQUEST_CHANGES` when blocking findings post,
  `COMMENT` when posting failed (see
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
- Writes a **stats dotenv file**. Clean runs additionally submit an `APPROVED`
  review so the PR review status reflects the verdict.
- **Fail-safe verdict**: unknown approval thresholds and severities that are
  not on the known ladder (`low`/`medium`/`high`/`critical`) always block
  approval.

## Usage

```
ocr-forgejo [flags] [input.json]
```

See the [CLI reference](docs/cli.md) for the full flag list, validation
rules, exit codes, stats file keys, and severity/category semantics.

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
