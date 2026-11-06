# depwhy

[![ci](https://github.com/dpologdvinity/depwhy/actions/workflows/ci.yml/badge.svg)](https://github.com/dpologdvinity/depwhy/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/dpologdvinity/depwhy)](https://github.com/dpologdvinity/depwhy/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/dpologdvinity/depwhy)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/dpologdvinity/depwhy)](https://goreportcard.com/report/github.com/dpologdvinity/depwhy)
[![license: MIT](https://img.shields.io/github/license/dpologdvinity/depwhy)](LICENSE)

![depwhy demo](docs/demo.gif)

**Why is this package in my dependency tree?** `depwhy` reads your lockfile and
tells you, offline by default, with one interface for npm, pnpm, Yarn, Bun, Python (uv, Poetry), Cargo, and Go.

```console
$ depwhy d
d@1.0.0  npm · package-lock.json
├── a@1.0.0
│   └── app (root, prod)
└── b@1.0.0
    └── app (root, dev)
pulled in by 2 direct dependencies: a (prod), b (dev)

$ depwhy --remove a
removing a@1.0.0 (direct, prod) from app  npm · package-lock.json
would remove 2 packages:
  a@1.0.0
  c@1.0.0
still needed by others (1):
  d@1.0.0  via b@1.0.0

$ depwhy --dupes
npm · package-lock.json
c  2 versions
  1.0.0  via a (prod)
  2.0.0  via b (dev)
```

- **Why**: every chain from your direct dependencies to a package.
- **Remove**: what would leave the tree if you removed a direct dependency, and what would stay and why. It is a dry run; nothing is modified.
- **Dupes**: packages installed at more than one version, and which direct dependencies pull in each.
- **Heaviest**: direct dependencies ranked by exclusive transitive on-disk size freed.
- **Polyglot**: if a directory holds lockfiles for several ecosystems, all of them are loaded and searched.
- **Monorepos**: `-r` discovers nested projects, respects `.gitignore`, and groups results by lockfile path. `--dupes --across` finds version conflicts between projects.
- **Offline by default, read-only always**: no network unless you pass `--online`, no package manager needed, no lockfile or manifest changed.
- **Health info (opt-in)**: with `--online`, packages show known security advisories and deprecated or yanked status.
- **Scriptable**: `--json` gives a stable, documented output.

### Compared with the package managers' own commands

Each package manager can answer "why" for its own lockfile. depwhy asks the same
question everywhere, and adds removal impact:

| | depwhy | `npm ls` | `pnpm why` | `uv tree --invert` | `cargo tree -i` |
|---|---|---|---|---|---|
| Ecosystems | npm, pnpm, Yarn, Bun, uv, Poetry, Cargo, Go | npm | pnpm | uv | Cargo |
| Works from the lockfile alone | yes | with `--package-lock-only` | yes | yes | no: downloads crate manifests |
| Removal impact (`--remove`) | yes | no | no | no | no |
| Duplicate versions (`--dupes`) | yes, also across projects | no | no | no | `cargo tree -d` |
| JSON output | yes | yes | yes | yes | no |
| Needs the package manager installed | no | yes | yes | yes | yes |

The package managers' own commands know details depwhy leaves out, such as Cargo
features and build edges and Python platform markers (see [Limitations](#limitations)).

## Install

With Go 1.26 or newer:

```sh
go install github.com/dpologdvinity/depwhy@latest
```

With Homebrew on macOS or Linux:

```sh
brew install dpologdvinity/tap/depwhy
```

On Debian, Ubuntu and derivatives (amd64 and arm64), through the signed apt repository:

```sh
sudo install -d -m 0755 /usr/share/keyrings
curl -fsSL https://dpologdvinity.github.io/depwhy/key.gpg | sudo tee /usr/share/keyrings/depwhy.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/depwhy.gpg] https://dpologdvinity.github.io/depwhy/apt ./" | sudo tee /etc/apt/sources.list.d/depwhy.list
sudo apt update && sudo apt install depwhy
```

With npm (any platform; downloads the matching native binary):

```sh
npm install -g @kaitlynbassford/depwhy   # installs the `depwhy` command
npx @kaitlynbassford/depwhy lodash       # or run it once
```

With [Scoop](https://scoop.sh) on Windows (amd64 and arm64):

```powershell
scoop bucket add dpologdvinity https://github.com/dpologdvinity/scoop-bucket
scoop install depwhy
```

The npm package is scoped because npm rejected the plain name `depwhy` as too similar to an existing package.

The Homebrew cask removes macOS's quarantine flag from the binary after install, because
the binary is not Apple-notarized.

Or download a binary, a `.deb`, `.rpm` or `.apk` package for Linux, a binary for macOS or
Windows (amd64 and arm64) from the
[releases page](https://github.com/dpologdvinity/depwhy/releases).

Check the install with `depwhy --version`.

## Usage

```
depwhy [flags] <package>[@version]      explain why <package> is installed
depwhy [flags] --remove <package>       impact of removing a direct dependency
depwhy [flags] --dupes                  packages installed at more than one version
depwhy [flags] --heaviest [N]           rank direct dependencies by exclusive size
depwhy completion bash|zsh|fish         print a shell completion script
```

Run it in a project directory, or point it at one with `-C`:

```sh
depwhy lodash                     # why is lodash installed?
depwhy lodash@4.17.21             # only that version
depwhy @babel/core                # scoped npm names work
depwhy --remove express           # what would removing express free?
depwhy --dupes --prod             # duplicates, ignoring dev dependencies
depwhy -r react                   # search every project in a monorepo
depwhy -r --dupes --across        # version conflicts across projects
depwhy --heaviest                 # top 10 heaviest direct dependencies
depwhy -C ../other-project react  # look somewhere else
depwhy --json lodash | jq .       # machine-readable
```

Names are matched the way the ecosystem does: exactly for npm, PEP 503
normalization for Python (`Foo_Bar` matches `foo-bar`), and `_`/`-` equivalence
for Cargo. A Go version matches with or without its `v`. If nothing matches,
depwhy suggests close names, or lists the installed versions when only the version
was wrong.

### Flags

| Flag | Meaning |
|---|---|
| `--remove` | Removal-impact mode. The argument is a direct dependency. |
| `--dupes` | List packages installed at more than one version. |
| `--across` | With `--dupes`, aggregate versions across all loaded lockfiles. |
| `--heaviest [N]` | Rank the N (default 10) direct dependencies that free the most disk space; `--heaviest 5` or `--heaviest=5`. Not combinable with `--online`. |
| `--venv` | Path to Python virtualenv (default `.venv` next to the lockfile). |
| `-C`, `--dir` | Directory to look for lockfiles in (default `.`). |
| `-r`, `--recursive` | Search subdirectories, respecting `.gitignore`; cannot be combined with `-l`. |
| `-l`, `--lockfile` | Use these lockfile paths instead of auto-detection (repeatable). |
| `-e`, `--ecosystem` | Restrict to `npm`, `python`, `cargo`, or `go`. |
| `--prod` | Ignore dev dependencies, and everything reachable only through them. |
| `-d`, `--depth` | Max levels printed in the why tree (0 = unlimited). Why mode only. |
| `--all` | Print all paths without the 10-path cap. Why mode only. |
| `--json` | Emit JSON. |
| `--go-graph` | For `go.mod`, add transitive edges from `go mod graph` (needs `go` on PATH). Runs offline; see the note below. |
| `--cargo-metadata` | For `Cargo.lock`, use `cargo metadata` for dev/build edges and workspace members (needs `cargo` on PATH). Runs offline; see the note below. |
| `--online` | Look up security advisories (OSV) and deprecated/yanked status. This is the only flag that uses the network. |
| `--refresh` | With `--online`, ignore cached answers. |
| `--no-color` | Disable ANSI color. Color is also off when output is not a terminal or `NO_COLOR` is set. |
| `-V`, `--version` | Print the version. |
| `-h`, `--help` | Print usage. |

`--go-graph` and `--cargo-metadata` are the only flags that run another program.
depwhy runs `go mod graph` or `cargo metadata` in the project's directory with a
60-second timeout, kept offline and read-only (`GOTOOLCHAIN=local`,
`GOPROXY=off`, `-mod=readonly`; `cargo --locked --offline`), so nothing is
downloaded and no `go.mod`, `go.sum` or `Cargo.lock` is changed. If the tool is
missing or fails, for example because a module or crate is not in the local
cache, depwhy warns and reads the file alone. **Use them only on projects you
trust**: go and cargo read the project's own configuration. depwhy stops a
`rust-toolchain.toml` or the `rustc`, wrapper and `rustflags` settings of
`.cargo/config.toml` from choosing what runs (by setting `RUSTUP_TOOLCHAIN` to
your `rustup default`, `RUSTC=rustc`, and empty `RUSTC_WRAPPER` and
`CARGO_ENCODED_RUSTFLAGS`; with rustup but no default toolchain it skips
`cargo metadata`), but cannot vet everything else cargo reads.

### Reading the output

- `(root, prod)` and `(root, dev)` mark your own project and say how it depends on the first package in the chain.
- `(*)` means that package was already expanded earlier in the tree, so its parents are not repeated.
- `(cycle)` marks a circular dependency.
- `…` means the tree was cut by `--depth`.
- `… N more paths (use --all)` means the 10-path cap hid N routes.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Success: the package was found, or `--dupes` ran. |
| `1` | The package was not found in any loaded lockfile. |
| `2` | Usage error, no lockfile found, or a lockfile could not be read or parsed. |

Errors and warnings go to stderr as `depwhy: ...` and `depwhy: warning: ...`.
Warnings never change the exit code.

### Shell completion

Tab completes flags, flag values (directories, lockfiles, ecosystems), and package
names read from the lockfiles that the command line so far selects, so
`depwhy -C ../api lo<Tab>` offers the names in `../api`'s lockfile.

```sh
# bash (3.2 or newer): add to ~/.bashrc
source <(depwhy completion bash)

# zsh: add to ~/.zshrc (after compinit)
source <(depwhy completion zsh)

# fish
depwhy completion fish > ~/.config/fish/completions/depwhy.fish
```

Completion never uses the network and never runs other programs, even when
`--online`, `--go-graph` or `--cargo-metadata` is on the command line. Package
names are treated as untrusted text: they are offered as completions, never
evaluated by the shell.

## Online health information

By default depwhy never touches the network. With `--online` it also looks up the
packages that appear in the output:

```console
$ depwhy --online request
request@2.88.2  npm · package-lock.json  ⚠ GHSA-p8p7-x288-28g6  [deprecated: request has been deprecated, see https://github.com/request/request/issues/3142]
└── vuln (root, prod)
direct dependency of vuln (prod)
```

- `⚠ GHSA-…` lists known advisories from [OSV](https://osv.dev) (GitHub, PyPI, RustSec and others).
- `[deprecated: …]` is an npm deprecation message. `[yanked]` means the version was yanked from PyPI or crates.io.
- Only packages shown in the output are looked up, and answers are cached for 24 hours in `$XDG_CACHE_HOME/depwhy` (or your OS cache directory). `--refresh` skips the cache.
- Lookups never fail a run. If a service is unreachable you get a warning on stderr, the result is printed without that information, and the exit code is unchanged.
- Private packages and registries are not supported; a package a public registry does not know is shown without information.
- In `--json` mode packages gain `advisories`, `deprecated` and `yanked` fields. `advisories` is `[]` for a package that was checked and is clean, and `null` when it could not be checked, so a script never treats an outage as "clean". Without `--online` those keys are absent.

Data sent: the names and versions of the packages shown, to `api.osv.dev` and to the
public registry for each package's ecosystem. Nothing else leaves your machine.
That includes the names of any private or internal packages that appear in the
output, so avoid `--online` on projects whose package names are confidential.

## Supported lockfiles

| File | Versions | Ecosystem |
|---|---|---|
| `pnpm-lock.yaml` | lockfileVersion 9.x | npm |
| `yarn.lock` | Berry (v2+) and Classic (v1) | npm |
| `bun.lock` | Bun 1.2+ text lockfile (JSONC) | npm |
| `package-lock.json` | lockfileVersion 1, 2, and 3 | npm |
| `uv.lock` | version 1 | python |
| `poetry.lock` | Poetry 1.x and 2.x | python |
| `Cargo.lock` | versions 3 and 4 (plus `--cargo-metadata`) | cargo |
| `go.mod` | all versions (plus `--go-graph`) | go |

Detection priority per directory:
- npm: `pnpm-lock.yaml` > `yarn.lock` > `bun.lock` > `package-lock.json`
- python: `uv.lock` > `poetry.lock`
- cargo: `Cargo.lock`
- go: `go.mod`

If multiple lockfiles are present for an ecosystem, the higher-priority file is used and depwhy warns.
By default, only the given directory is checked. Add `-r` to find nested projects.
Each directory applies the same priority independently. Recursive discovery skips
`.git`, `node_modules`, `.venv`, `venv`, `target`, `dist`, `build`, `vendor`,
`__pycache__`, `.tox` and `testdata` (test fixtures, which the Go tool also
skips), honors nested `.gitignore` files, and does not follow directory symlinks. Parent/global Git ignores and the Git index are not consulted.
Output identifies each lockfile by its path relative to `-C`, including in JSON.
Installed sizes use each discovered project's own install directory.

Find version conflicts across all discovered projects, or select files explicitly:

```sh
depwhy -C path/to/repo -r --dupes --across --prod
depwhy --dupes --across -l web/package-lock.json -l admin/package-lock.json
depwhy -r --dupes --across --json
```

`--across` groups packages within each ecosystem and includes conflicts within
individual files. It does not enable recursion itself. Its JSON adds
`"across": true`; each package version lists sources with `lockfile`, `version`,
`roots` and `direct_deps`. See [the full schema](docs/json-schema.md#across-results).

## Limitations

- **Cargo**: `Cargo.lock` records no dev or build edges, so every edge is shown as `prod`. Path dependencies outside a workspace also lack a source and appear as roots. Use `--cargo-metadata` when cargo is available on PATH for precise dependency discrimination and workspace resolution.
- **Go**: `go.mod` distinguishes direct dependencies (`prod`) from `// indirect` requirements. Pass `--go-graph` when go is available on PATH to refine transitive edges via `go mod graph`; only the versions minimal version selection picks are kept. A `replace` keeps the dependency's module path; only a same-path version pin changes the version shown.
- **Poetry**: direct dependencies are classified from `pyproject.toml`: the `main` group and `[project].dependencies` are `prod`, every other group (dev, test, lint, docs, ...) and every PEP 735 `[dependency-groups]` group is `dev`, and extras are `optional`. Without `pyproject.toml`, depwhy falls back to the lockfile's `groups` (Poetry 2.x) or `category` (before 1.5); Poetry 1.5-1.8 lockfiles record neither, so every package then counts as `prod`.
- **Python markers**: environment markers in `uv.lock` and `poetry.lock` are ignored, so every edge counts on every platform.
- **Path cap**: the why tree and JSON paths stop at 10 routes unless you pass `--all`.
- **Workspace members** are treated as roots, so they cannot themselves be queried.
- **Yarn**: dev dependencies of a project or workspace come from its `package.json`, because Yarn Berry lists them with the regular ones in `yarn.lock`; without the `package.json` they show as `prod`. A `patch:` package (including Yarn's builtin typescript/fsevents/resolve patches) is the same node as the package it patches. Classic workspace globs are expanded with Go's `filepath.Glob` (no `**` or `!`).
- Advisory and deprecation data is only as complete as OSV and the public registries; `--online` does not read private registries.
- **Sizes** are measured only when the project is installed, next to its lockfile (`node_modules`, or `.venv`/`--venv` for Python; cargo and go are not measured). A package is measured only if the installed copy is exactly the locked name and version and lies inside the project; anything else is left unmeasured rather than guessed. Freed totals count unmeasured packages, such as another platform's optional `fsevents` or `@esbuild/*`, as 0 and say how many there were (`3 not measured`). A package installed at several paths is measured once. In JSON, size keys are absent when unmeasured (SPEC §13.5).

## JSON output

`--json` prints one document with the keys `schema`, `mode`, `query`, `results`,
and `warnings`. Arrays are never `null`, field names are snake_case, and a
not-found run adds `suggestions`. For example, `depwhy --json c`:

```json
{
  "schema": 1,
  "mode": "why",
  "query": { "name": "c", "version": null },
  "results": [
    {
      "ecosystem": "npm",
      "lockfile": "package-lock.json",
      "package": { "name": "c", "version": "1.0.0" },
      "dev_only": false,
      "direct": false,
      "paths": [
        [
          { "name": "app", "version": "1.0.0", "root": true },
          { "name": "a", "version": "1.0.0", "kind": "prod" },
          { "name": "c", "version": "1.0.0", "kind": "prod" }
        ]
      ],
      "paths_truncated": false,
      "leads": [{ "root": "app", "name": "a", "version": "1.0.0", "kind": "prod" }]
    }
  ],
  "warnings": []
}
```

The complete schema for every mode is [docs/json-schema.md](docs/json-schema.md).
From 1.0 it is a stability promise: new keys may appear, but anything that would
break a script bumps `schema`.

## How it works

Each lockfile is parsed into the same small graph: packages are nodes, "requires"
relationships are edges tagged `prod`, `dev`, `optional` or `peer`, and your
project (or each workspace member) is a root. The three modes are queries on that
graph, so adding a lockfile format means writing one parser and nothing else.

```
lockfile ──parse──▶ graph ──query──▶ result ──render──▶ text or JSON
(internal/lockfile) (internal/graph)          (internal/render)
```

`internal/cli` wires the stages together. [ARCHITECTURE.md](ARCHITECTURE.md) has the details.

## Documentation

| Document | Contents |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | How depwhy is built: packages, the graph model, algorithms, testing, CI. |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Local workflow, checks, commit and PR conventions. |
| [docs/SPEC.md](docs/SPEC.md) | The behavior contract, section by version (v0.1 core, then §12 online, §13 sizes, §14 formats, §15 monorepos, §16 completion). |
| [docs/json-schema.md](docs/json-schema.md) | The frozen `--json` output contract (schema 1). |
| [docs/PLAN.md](docs/PLAN.md) | The implementation plans for v0.1 and v0.2. |
| [docs/ROADMAP.md](docs/ROADMAP.md) | What comes next. |

## Development

You need Go 1.26 or newer.

```sh
go build ./...            # compile
go test ./...             # run all tests, including the speed guard
go test -race ./...       # as CI does
go vet ./... && gofmt -l .   # gofmt must print nothing
golangci-lint run ./...   # lint (optional locally; CI runs it)
```

The CLI is tested end to end with golden files. After changing output on purpose:

```sh
go test ./internal/cli -update   # rewrite testdata/golden
git diff testdata/golden         # read every change against docs/SPEC.md
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow, commit conventions
and the checks CI runs. To support another lockfile format, see "Adding a
lockfile format" in [ARCHITECTURE.md](ARCHITECTURE.md).

## License

[MIT](LICENSE)
