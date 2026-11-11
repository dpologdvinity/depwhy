# AGENTS.md

Instructions for coding agents (Claude Code, Codex, and others) working in this repo.
`CLAUDE.md` only imports this file.

## What this is

depwhy is a single-binary Go CLI that reads lockfiles (npm, pnpm, Yarn, Bun, uv, Poetry,
Cargo, go.mod) and explains why a package is in the dependency tree, what removing a
direct dependency would free, and which packages are duplicated. It is offline and
read-only by default.

- Behavior contract: [docs/SPEC.md](docs/SPEC.md). The spec wins over code, plans and
  ideas; a behavior change updates the spec in the same commit or PR.
- Code layout and data flow: [ARCHITECTURE.md](ARCHITECTURE.md).
- Frozen `--json` contract: [docs/json-schema.md](docs/json-schema.md).
- Human contributor guide: [CONTRIBUTING.md](CONTRIBUTING.md).

## Setup

Go 1.26+ (see `go.mod`). Optional: `golangci-lint` v2, `lefthook`, `gitleaks`, `typos`.
Run `lefthook install` once to enable the pre-commit hooks (gitleaks, gofmt,
golangci-lint, typos). No environment variables are required; `NO_COLOR` and
`XDG_CACHE_HOME` are honored if set.

## Commands

These are the CI gates; all must pass before a commit.

```sh
gofmt -l .                # must print nothing
go vet ./...
golangci-lint run ./...
go test -race ./...       # includes golden end-to-end tests and a speed guard
go mod tidy               # go.mod/go.sum must not change
cd npm && npm test        # only when touching the npm launcher in npm/
```

Coverage must stay at or above 90% total:
`go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | tail -1`

Try a build against a fixture: `go run . -C testdata/projects/npm d`.

## Conventions

- TDD: write the failing test first; a bug fix starts with a regression test.
- Golden files in `testdata/golden` are the spec in executable form. Regenerate only
  for an intentional output change (`go test ./internal/cli -update`), then review
  `git diff testdata/golden` against the spec. Never regenerate to make a test pass.
- Output must be deterministic: sort map keys before building nodes, edges or output.
- `internal/graph` imports only the standard library. Format knowledge stays in
  `internal/lockfile`, formatting in `internal/render`.
- Lockfile content is untrusted input: escape it before printing to a terminal.
- Dependencies are limited to pflag, a YAML library and a TOML library.
- No lint ignores, skipped tests or suppressed errors. Fix the cause.

## Commits and releases

Conventional Commits, small and atomic: `type(scope): description`. Scopes in use:
`cli`, `graph`, `lockfile`, `render`. Branch from `master`. No AI attribution trailers.

Releases are cut by pushing a `v*` tag; `release.yml` reruns CI, then GoReleaser
publishes binaries, Homebrew, apt and npm. See "Releases" in CONTRIBUTING.md.

## Agent workflow

How this repo was built with agents, and the config that is checked in:

- **Spec, then plan, then tasks.** [docs/SPEC.md](docs/SPEC.md) was written first;
  [docs/PLAN.md](docs/PLAN.md) broke each release (v0.1–v0.5) into ordered tasks,
  each ending in a passing check and one commit. Each release on
  [docs/ROADMAP.md](docs/ROADMAP.md) repeats the cycle: extend the spec, write the
  tasks into the plan, implement.
- **Claude Code** (`.claude/settings.json`): enables the `gopls-lsp` plugin for Go
  code intelligence, `commit-commands` for commit/PR flows, and `ponytail` (smallest
  complete change, from the `DietrichGebert/ponytail` marketplace). Personal
  settings go in `.claude/settings.local.json` (gitignored).
- **Codex** (`.codex/config.toml`): enables the same `ponytail` plugin.
- **Shared skill** (`.agents/skills/surgical-patch`, symlinked into `.claude/skills/`
  so both agents load one copy): fix bugs at the narrowest responsible layer, with
  regression proof.
- **Safety net:** lefthook runs gitleaks on every staged change, so an agent cannot
  commit a secret unnoticed.
