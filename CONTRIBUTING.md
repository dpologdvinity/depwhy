# Contributing to depwhy

Thanks for helping. This guide covers the local workflow and what a pull
request needs. For how the code is organized, read [ARCHITECTURE.md](ARCHITECTURE.md);
for the exact behavior contract, read [docs/SPEC.md](docs/SPEC.md). Where they
disagree with an idea, the spec wins, and changing behavior means changing the
spec in the same pull request.

## Setup

You need Go 1.26 or newer. Optional tools: `golangci-lint` v2 for linting and
`goreleaser` for checking release builds.

```sh
git clone https://github.com/dpologdvinity/depwhy.git
cd depwhy
go build ./...
go test ./...
```

Try your build against a fixture project:

```sh
go run . -C testdata/projects/npm d
go run . -C testdata/projects/cargo --dupes
```

## The checks CI runs

Run these before you push; CI fails on any of them.

```sh
gofmt -l .                  # must print nothing; fix with: gofmt -w .
go vet ./...
golangci-lint run ./...     # config in .golangci.yml
go test -race ./...         # all tests, including the speed guard
go mod tidy                 # go.mod and go.sum must not change
```

Total statement coverage must stay at or above 90%. Check it with:

```sh
go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | tail -1
```

## Workflow

1. Branch from `master`: `git switch -c feat/short-description`.
2. Write the test first and watch it fail for the right reason, then make it pass.
   For a bug, the failing test is a regression test for it.
3. Keep commits small, with one logical change each.
4. Open a pull request into `master`. Describe what changed and why, and mention
   any spec section it touches.

### Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `type(scope): description`.

| Type | Use for |
|---|---|
| `feat` | new behavior |
| `fix` | a bug fix |
| `refactor` | restructuring with no behavior change |
| `perf` | a speed improvement |
| `test` | tests only |
| `docs` | documentation only |
| `ci` | workflows and release config |
| `chore` | everything else (dependencies, tooling) |

Scopes in use: `cli`, `graph`, `lockfile`, `render`. Example:
`fix(graph): stop the kept walk at workspace roots`.

Never commit secrets, `.env` files, or agent/editor tooling directories. If a
tool you use creates local files, add them to `.git/info/exclude`.

## Code conventions

- Every package, exported identifier, and function has a doc comment that starts with its name. Comment *why* when the code does something non-obvious; do not restate what it does.
- Output must be deterministic. Go randomizes map iteration, so sort keys before using them to build nodes, edges, or output.
- `internal/graph` imports only the standard library. Lockfile format knowledge stays in `internal/lockfile`; output formatting stays in `internal/render`.
- Parsers return warnings for the odd-but-survivable (an unresolved optional dependency, an ambiguous match) and an error only when the file cannot be understood.
- Do not suppress errors, skip tests, or add lint ignores to get green. Fix the cause.
- The only dependencies are pflag, a YAML library, and a TOML library. Adding one needs a reason a few lines of standard-library code cannot meet.

## Tests

| Where | What |
|---|---|
| `internal/graph` | Algorithms on hand-built graphs: diamonds, cycles, direct and dev-only targets, `--prod`, the path cap. |
| `internal/lockfile` | One file per format, using `testdata/projects` and `testdata/edge`. |
| `internal/render` | Exact expected text and decoded JSON structure. |
| `internal/cli` | End-to-end golden tests against `testdata/golden`, plus error-path tests. |

### Golden files

Golden files hold the exact stdout, stderr, and exit code for each CLI case. They
are the spec in executable form. After an intentional output change:

```sh
go test ./internal/cli -update
git diff testdata/golden
```

Read every changed golden against the spec before committing. Never regenerate
goldens just to make a failing test pass.

### Fixtures

All four fixture projects under `testdata/projects` encode the same logical graph
so their outputs can be compared. The `uv` and `cargo` lockfiles are stored as
`uv.lock.fixture` and `Cargo.lock.fixture`; the golden test copies them to a
temporary directory under their real names. Keep that convention when adding
fixtures for formats whose file names end in `.lock`.

## Adding a lockfile format

See "Adding a lockfile format" in [ARCHITECTURE.md](ARCHITECTURE.md). In short: a
parser in `internal/lockfile`, an entry in `supported` in `load.go`, a fixture,
parser tests, golden cases, and README and spec updates.

## Releases

Maintainers cut releases by pushing a version tag:

```sh
git tag -a v0.2.0 -m "depwhy v0.2.0"
git push origin v0.2.0
```

The release workflow first runs the whole CI pipeline (`ci.yml`), then publishes
binaries, Linux packages and the Homebrew cask with GoReleaser, republishes the apt
repository, and publishes the npm package. Tags with a suffix such as `v0.2.0-rc.1`
are published as pre-releases and do not update Homebrew, apt or npm. A missing
`APT_GPG_PRIVATE_KEY`, `NPM_TOKEN` or `HOMEBREW_TAP_GITHUB_TOKEN` skips that channel
with a warning instead of failing the release.

Repository secrets the release uses: `HOMEBREW_TAP_GITHUB_TOKEN` (Homebrew) and
`APT_GPG_PRIVATE_KEY` (signs the apt repository) and `NPM_TOKEN` (publishes the
npm launcher package in `npm/`; its tests run with `cd npm && npm test`).

One-time repository setup the apt site depends on: Settings → Pages → Source:
*GitHub Actions*, and the `github-pages` environment must allow deployments from
`master` and `v*` tags. To check the packaging scripts locally you need `dpkg-dev`, `apt-utils`, `gpg` and Docker; see
`scripts/build-apt-repo.sh` and `scripts/test-apt-repo.sh`.

## Reporting problems

Use the issue templates. For a wrong result, include the command, the output you
got and expected, and the smallest piece of lockfile that reproduces it (remove
private package names).
