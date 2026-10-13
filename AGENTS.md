# AGENTS.md

Instructions for AI coding agents working in this repository. These rules are
binding: the two "Mandatory" sections below are hard requirements, not
suggestions.

## Repo Overview

Standalone Go mock servers for the TPL deployment ecosystem: SCM (GitHub
Enterprise / Bitbucket Server), Harbor registry, and Kubernetes/Tekton API
doubles for local development and testing.

Three **nested Go modules** (`scm/`, `harbor/`, `k8s/`), each with its own
`go.mod`. There is **no root `go.mod`** — run Go commands from inside the
module directory, or use the Taskfile targets which handle this for you.

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

Shell, Read, Grep, and Glob remain fine for non-code files (Taskfile.yml,
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

Use the Taskfile — it iterates over all three modules for you:

| Task | What it does |
|------|--------------|
| `task check` | fmt-check + vet + lint + test across all modules |
| `task test` | `go test ./...` in all modules |
| `task lint` | `golangci-lint run ./...` in all modules |
| `task vet` | `go vet ./...` in all modules |
| `task fmt` | gofmt all modules (`task fmt-check` verifies only) |
| `task tidy` | `go mod tidy` in all modules |
| `task run:scm` | Run SCM mock (ports 8081 GitHub, 8082 Bitbucket) |
| `task run:harbor` | Run Harbor mock (ports 8083 nonprod, 8084 prod) |
| `task run:k8s` | Run K8s/Tekton mock (port 16443, downloads envtest assets first) |
| `task envtest` | `setup-envtest use 1.35.0 --bin-dir ./bin` for the k8s mock |

Per-module alternative: `cd <module> && go test ./...`. Never run bare
`go build`/`go test` at the repo root — it fails without a root `go.mod`.

## Layout & Conventions

| Module | Path | Import path |
|--------|------|-------------|
| SCM | `scm/` | `forgejo.zippelf.de/felix/tpl-go-mocks/scm` |
| Harbor | `harbor/` | `forgejo.zippelf.de/felix/tpl-go-mocks/harbor` |
| K8s | `k8s/` | `forgejo.zippelf.de/felix/tpl-go-mocks/k8s` |

- SCM module entrypoints: `scm/cmd/mockscm`; Harbor: `harbor/cmd/mockharbor`;
  K8s: `k8s/cmd/mockk8s`.
- The k8s mock downloads Tekton CRDs from the network at startup; its envtest
  binaries default to `<repo>/bin/k8s/1.35.0-<os>-<arch>`.
- Dependencies are deliberately minimal: Harbor is stdlib-only at runtime, SCM
  uses `onsi/gomega` test helpers compiled into the package. Do not add
  heavyweight dependencies without a strong reason.

## Verification Rule

Before declaring any code change done:

1. `task check` must pass (format check, vet, lint, tests across all modules).
2. `serena_get_diagnostics_for_file` on every touched Go file must report no
   errors.
3. Never claim tests pass without having run them in this session.
