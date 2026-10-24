# depwhy

[![ci](https://github.com/dpologdvinity/depwhy/actions/workflows/ci.yml/badge.svg)](https://github.com/dpologdvinity/depwhy/actions/workflows/ci.yml)

**Why is this package in my dependency tree?** `depwhy` reads your lockfile and
tells you, offline, with one interface for npm, pnpm, Python (uv), and Cargo.

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
- **Polyglot**: if a directory holds lockfiles for several ecosystems, all of them are loaded and searched.
- **Offline by default, read-only always**: no network unless you pass `--online`, no package manager needed, no lockfile or manifest changed.
- **Health info (opt-in)**: with `--online`, packages show known security advisories and deprecated or yanked status.
- **Scriptable**: `--json` gives a stable, documented output.

## Install

With Go 1.26 or newer:

```sh
go install github.com/dpologdvinity/depwhy@latest
```

On macOS with Homebrew (available from the first release after v0.1.0):

```sh
brew install dpologdvinity/tap/depwhy
```

Or download a binary for Linux, macOS, or Windows (amd64 and arm64) from the
[releases page](https://github.com/dpologdvinity/depwhy/releases).

Check the install with `depwhy --version`.

## Usage

```
depwhy [flags] <package>[@version]      explain why <package> is installed
depwhy [flags] --remove <package>       impact of removing a direct dependency
depwhy [flags] --dupes                  packages installed at more than one version
```

Run it in a project directory, or point it at one with `-C`:

```sh
depwhy lodash                     # why is lodash installed?
depwhy lodash@4.17.21             # only that version
depwhy @babel/core                # scoped npm names work
depwhy --remove express           # what would removing express free?
depwhy --dupes --prod             # duplicates, ignoring dev dependencies
depwhy -C ../other-project react  # look somewhere else
depwhy --json lodash | jq .       # machine-readable
```

Names are matched the way the ecosystem does: exactly for npm, PEP 503
normalization for Python (`Foo_Bar` matches `foo-bar`), and `_`/`-` equivalence
for Cargo. If nothing matches, depwhy suggests close names.

### Flags

| Flag | Meaning |
|---|---|
| `--remove` | Removal-impact mode. The argument is a direct dependency. |
| `--dupes` | List packages installed at more than one version. |
| `-C`, `--dir` | Directory to look for lockfiles in (default `.`). |
| `-l`, `--lockfile` | Use these lockfile paths instead of auto-detection (repeatable). |
| `-e`, `--ecosystem` | Restrict to `npm`, `python`, or `cargo`. |
| `--prod` | Ignore dev dependencies, and everything reachable only through them. |
| `-d`, `--depth` | Max levels printed in the why tree (0 = unlimited). |
| `--all` | Print all paths without the 10-path cap. |
| `--json` | Emit JSON. |
| `--online` | Look up security advisories (OSV) and deprecated/yanked status. This is the only flag that uses the network. |
| `--refresh` | With `--online`, ignore cached answers. |
| `--no-color` | Disable ANSI color. Color is also off when output is not a terminal or `NO_COLOR` is set. |
| `-V`, `--version` | Print the version. |
| `-h`, `--help` | Print usage. |

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
| `package-lock.json` | lockfileVersion 2 and 3 | npm |
| `pnpm-lock.yaml` | lockfileVersion 9.x | npm |
| `uv.lock` | version 1 | python |
| `Cargo.lock` | versions 3 and 4 | cargo |

If both `pnpm-lock.yaml` and `package-lock.json` are present, the pnpm file is used and depwhy warns.
Only the given directory is checked; there is no recursive scan.

## Limitations

- **Cargo**: `Cargo.lock` records no dev or build edges, so every edge is shown as `prod`. Path dependencies outside a workspace also lack a source and appear as roots.
- **Python markers**: environment markers in `uv.lock` are ignored, so every edge counts on every platform.
- **Path cap**: the why tree and JSON paths stop at 10 routes unless you pass `--all`.
- **Workspace members** are treated as roots, so they cannot themselves be queried.
- Advisory and deprecation data is only as complete as OSV and the public registries; `--online` does not read private registries.
- Only lockfiles are read, apart from `package.json` names used to label pnpm roots. `yarn.lock`, `bun.lock`, `poetry.lock`, and `requirements.txt` are not supported yet; see the [roadmap](docs/ROADMAP.md).

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

The full schema, including `--remove` and `--dupes`, is in [docs/SPEC.md](docs/SPEC.md) §7.3.
The schema becomes a stability promise at 1.0.

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
| [docs/SPEC.md](docs/SPEC.md) | The behavior contract for v0.1. |
| [docs/PLAN.md](docs/PLAN.md) | The implementation plan for v0.1. |
| [docs/ROADMAP.md](docs/ROADMAP.md) | What comes after v0.1. |

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
