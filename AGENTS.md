# AGENTS.md

Instructions for AI coding agents working in this repository. These rules are
binding: the two "Mandatory" sections below are hard requirements, not
suggestions.

## Repo Overview

`ocr-forgejo`: a Go CLI that reads `ocr review --format json` output from
open-code-review (`ocr`) and publishes it to a Forgejo pull request as a
review with inline comments plus a sticky summary. Built for use from
Argo Workflows.

This is a **single Go module at the repo root** (`go.mod`), with the binary
entrypoint in `cmd/ocr-forgejo`. Implementation packages live under
`internal/` (`review`, `format`, `policy`, `incremental`, `diffinv`,
`poster`, `publish`). OpenSpec workflow artifacts live under `openspec/`.

## Mandatory: Serena MCP for all code changes

All reading, navigation, and modification of source code MUST go through the
Serena MCP tools. This is required for reliable symbol-level edits in Go.

Before any coding task:

1. Call `serena_activate_project` (project: this repo) if not already active.
2. Call `serena_initial_instructions` once per session before the first edit.

Rules for code work:

- **Navigation**: use `serena_get_symbols_overview`, `serena_find_symbol`,
  `serena_find_referencing_symbols`, `serena_find_declaration`, and
  `serena_find_implementations` instead of raw file reads/greps to understand
  code.
- **Edits**: use `serena_replace_symbol_body`, `serena_insert_before_symbol`,
  `serena_insert_after_symbol`, `serena_safe_delete_symbol`, and
  `serena_replace_content`. Do **not** rewrite whole Go files with a raw file
  write tool.
- **New files**: prefer `serena_create_text_file`.
- **After edits**: run `serena_get_diagnostics_for_file` on touched Go files
  to catch LSP-reported issues early.

Shell, Read, Grep, and Glob remain fine for non-code files (Taskfile.yaml,
YAML, markdown), running builds/tests/linters, and git operations.

## Mandatory: superpowers skills

Load the matching skill via the skill tool **before** starting the activity.
Skipping a required skill is a rule violation. Required mappings:

| Situation | Required skill |
|-----------|----------------|
| Any creative work: new feature, component, behavior change | `brainstorming` |
| Implementing any feature or bugfix | `test-driven-development` |
| Any bug, test failure, or unexpected behavior | `systematic-debugging` |
| Multi-step task with a spec/requirements | `writing-plans` |
| Executing an implementation plan inline | `executing-plans` |
| Executing a plan with subagents | `subagent-driven-development` |
| 2+ independent tasks | `dispatching-parallel-agents` |
| Feature work needing workspace isolation | `using-git-worktrees` |
| Before claiming work is complete/done/passing | `verification-before-completion` |
| Completed major feature, before merge | `requesting-code-review` |
| Handling incoming review feedback | `receiving-code-review` |
| Finishing/integrating a development branch | `finishing-a-development-branch` |

The OpenSpec workflow skills (`openspec-propose`, `openspec-apply-change`,
`openspec-verify-change`, `openspec-archive-change`, `openspec-sync-specs`,
`openspec-continue-change`, `openspec-explore`, `openspec-onboard`) are
mandatory whenever the task touches the OpenSpec workflow (see `openspec/`).

## Commands

Go commands run from the repo root (single module — no per-module juggling
needed). Prefer the Taskfile targets:

| Task | What it does |
|------|--------------|
| `task build` | `go build` the binary (`bin/ocr-forgejo` per `Taskfile.yaml`) |
| `task test` | `go test ./...` with coverage profile |
| `task test-coverage` | Tests plus HTML coverage report |
| `task lint` | `golangci-lint run` |
| `task vet` | `go vet ./...` |
| `task fmt` | `go fmt ./...` |
| `task deps` | `go mod tidy` |
| `task clean` | Remove binary and coverage artifacts |

There is **no `task check` aggregate** in this repo; run `task fmt`, `task
vet`, `task lint`, and `task test` individually (or `go test ./...` etc.
directly).

## Layout & Conventions

| What | Path |
|------|------|
| Binary entrypoint | `cmd/ocr-forgejo` |
| Implementation packages | `internal/review`, `internal/format`, `internal/policy`, `internal/incremental`, `internal/diffinv`, `internal/poster`, `internal/publish` |
| Module path | `github.com/felixz92/open-code-review-forgejo` |
| ADRs | `docs/adr/` |
| OpenSpec change | `openspec/changes/add-ocr-forgejo-cli/` |

- Forgejo SDK: `codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3`. Keep
  dependencies minimal; do not add heavyweight dependencies without a strong
  reason.
- Auth is via the `GIT_TOKEN` env var (injected by Argo Workflows); targeting
  is always via explicit flags (`--url`, `--repo owner/name`, `--pr <index>`).
  No CI-env auto-detection.
- Posting model: one Forgejo review per run (single-review posting, see
  `docs/adr/0001-single-review-posting.md`), sticky summary issue comment
  with the `<!-- ocr-summary -->` marker.
- Consumed input format is the `ocr review --format json` schema — do not
  change parsing in a way that breaks that contract.

## Verification Rule

Before declaring any code change done:

1. `task fmt`, `task vet`, `task lint`, and `task test` must all pass.
2. `serena_get_diagnostics_for_file` on every touched Go file must report no
   errors.
3. Never claim tests pass without having run them in this session.
