# Architecture

This document explains how depwhy is built and why. For what it does, see the
[README](README.md); for the exact behavior contract, see [docs/SPEC.md](docs/SPEC.md).
The spec wins if the two ever disagree.

## Overview

depwhy answers three questions about a project's dependencies by reading
lockfiles (offline unless you pass `--online`):

| Mode | Question |
|---|---|
| `depwhy <pkg>` | Why is this package installed? |
| `depwhy --remove <pkg>` | What would removing this direct dependency free? |
| `depwhy --dupes` | Which packages are installed at more than one version? |

The central idea is that every supported lockfile is turned into the same small
graph, and all three questions are answered on that graph. Parsers know about
formats; algorithms and renderers know nothing about them. Adding a format means
writing one parser, with no changes anywhere else.

```
 lockfile files           graph                 results            output
┌──────────────┐   ┌───────────────┐   ┌─────────────────┐   ┌───────────────┐
│ package-lock │   │               │   │ WhyResult       │   │ text (trees)  │
│ pnpm-lock    │──▶│  graph.Graph  │──▶│ RemoveResult    │──▶│ JSON document │
│ uv.lock      │   │  per lockfile │   │ []Dupe          │   │               │
│ Cargo.lock   │   │               │   │                 │   │               │
└──────────────┘   └───────────────┘   └─────────────────┘   └───────────────┘
   lockfile                graph                graph              render
      └──────────────────────────── cli wires it together ────────────────────┘
```

## Package layout

```
main.go                   entry point: os.Exit(cli.Run(...))
internal/
  cli/                    flags, lockfile loading, mode dispatch, exit codes
  lockfile/               detection and one parser per format
  graph/                  graph model and the Why / Remove / Dupes algorithms
  render/                 text and JSON output, ANSI color
  health/                 optional online lookups: advisories, deprecated/yanked
testdata/
  projects/<name>/        one fixture project per ecosystem
  edge/<name>/            edge-case lockfiles used only by parser tests
  golden/                 expected CLI output (see Testing)
docs/                     SPEC.md (behavior), PLAN.md (build plan), ROADMAP.md
```

Dependencies between packages only point one way:

```
cli ──▶ render ──▶ graph
 │                   ▲
 ├────▶ lockfile ────┘
 └────▶ health
```

`graph` imports only the standard library, and so does `health`. `lockfile`
imports `graph` plus the YAML and TOML libraries. `render` imports `graph`.
`cli` imports all of them and the flag library. Nothing imports `cli`.

Everything lives under `internal/`, so the only public surface of the module is
the command line and its JSON output.

## The graph model (`internal/graph`)

A `Graph` describes one lockfile:

- **Nodes** are packages, identified by `name@version`, or by `root:<label>` for
  *roots*. A root is the project itself or a workspace member: something that
  declares dependencies rather than being depended on.
- **Edges** say "From requires To" and carry a **Kind**: `prod`, `dev`,
  `optional` or `peer`. The kind comes from the field of the lockfile that
  declared the edge.
- `Out[i]` and `In[i]` hold the edges leaving and entering node `i`, so the graph
  can be walked in both directions in constant time per edge.

Parsers never touch these slices. They use a `Builder`, which enforces the
invariants every query relies on:

1. Node IDs are unique; asking for the same package twice returns the same node.
2. `In` is the exact mirror of `Out`.
3. Duplicate edges (same From, To and Kind) are dropped.
4. At least one root exists, otherwise `Build` fails.

A graph is built once and then read-only. A few results (dev-only flags and the
sorted parent lists) are cached on it via `sync.Once` the first time they are
needed, making a `Graph` safe for concurrent reads.

### Direct dependencies and dev-only packages

A *direct dependency* is any package with an edge from a root. A package is
*dev-only* when every route from the roots to it starts with a `dev` edge leaving
a root. `DevOnly` finds these with two traversals from the roots: one following
every edge and one skipping root `dev` edges. Packages reached by the first but
not the second are dev-only.

`--prod` is implemented by treating root `dev` edges, and anything that is
dev-only, as absent.

## Lockfile parsers (`internal/lockfile`)

`Detect` looks for the four known file names in a directory (at most one per
ecosystem; `pnpm-lock.yaml` beats `package-lock.json`). `FromPaths` classifies
explicit `--lockfile` paths by base name. `Load` reads a file and dispatches to a
parser by format. Files are loaded in a fixed order (pnpm, npm, uv, cargo), which
is also the output order.

Each parser is `ParseX(path string, data []byte) (*graph.Graph, []string, error)`.
The strings are warnings. They never fail a run.

| Format | Roots | How edges are resolved |
|---|---|---|
| `package-lock.json` v2/v3 | `""` and workspace directories | Node's lookup rule over the flat `packages` map, following `link` entries |
| `pnpm-lock.yaml` v9 | each importer | `snapshots` keys, with peer suffixes stripped; `link:`, `file:` and alias refs handled |
| `uv.lock` | `[manifest].members`, else the `.` project | by name, narrowed by version then source |
| `Cargo.lock` v3/v4 | packages with no `source` | by name, narrowed by version then source |

Things every parser does the same way:

- **Deterministic output.** Go randomizes map iteration, so parsers sort keys
  before creating nodes or edges. The same lockfile always produces the same
  graph, which the golden tests depend on.
- **Merge, don't duplicate.** Two lockfile entries with the same name and version
  become one node. For pnpm this also merges peer-dependency variants of a
  package.
- **Warn on the unexpected.** Unresolvable prod/dev dependencies and ambiguous
  matches add warnings. `finish()` also warns about packages no root can reach;
  lockfiles contain such orphans in practice, and they are kept but unreachable.
- **Wrap errors** as `parse <path>: <cause>`.

### Adding a lockfile format

1. Write `ParseFoo(path string, data []byte)` in `internal/lockfile/foo.go`. Use a
   `graph.Builder`, sort map keys, and end with `return finish(b, warns)`.
2. Add the file name, ecosystem and format id to `supported` in `load.go`, and a
   case to `Load`. Position in `supported` is the output order.
3. Add a fixture under `testdata/projects/foo/` that encodes the shared logical
   graph (see Testing), parser tests, and golden cases.
4. Mention the format in the README and SPEC.

Nothing in `graph`, `render` or `cli` needs to change.

## Algorithms (`internal/graph`)

### Why

For a target `t`, the result has three parts that are computed separately:

- **A reverse tree** for text output. The root is `t` and the children of a node
  are the packages that require it, sorted roots first, then by name and version.
  The walk is depth first with two sets: nodes on the current branch (a repeat is
  marked `(cycle)`) and nodes already expanded anywhere in this tree (a repeat is
  marked `(*)` and not expanded again). The second set is what keeps the tree
  linear in the size of the graph even when there are astronomically many paths.
  Roots are always leaves.
- **A list of paths**, root first, for JSON. These are real simple paths, found
  by depth-first search with an on-path set.
- **Leads**: one entry per direct dependency that reaches `t`, found with a single
  reverse breadth-first search.

The 10-path cap applies to the tree and the path list. When it cuts the tree,
the hidden leaves are counted by rebuilding the tree without the cap (cheap,
because of the `(*)` rule) and reported as "N more paths". A safety limit of
1,000,000 steps stops a pathological graph from hanging a capped path search.

If `t` is itself a direct dependency, the tree and paths show only the projects
that require it. The leads still list everything, so JSON consumers see the full
picture.

### Remove

Remove compares two reachability sets over the whole graph:

- `before`: packages reachable from the roots.
- `after`: packages reachable when every root→`t` edge is ignored. Edges into `t`
  from other packages are still followed, which is how `t` can stay installed.

`Freed` is `before − after`. `Kept` is the set of packages `t` requires that are
still in `after`, each with the first package on a shortest root path that keeps
it (breadth-first search with ties broken in sorted order, so the answer is
stable). The walk that finds `Kept` stops at roots: a workspace member's own
dependencies are not "kept" for the package that happens to link to it.

### Dupes

Group non-root nodes by name, keep groups with two or more versions, and attach
the leads for each version. Groups sort by version count (descending) then name.
Versions sort as plain strings, which is what the spec asks for.

### Matching and suggestions

User queries are normalized per ecosystem only for matching: PEP 503 for Python,
lowercase with `_`→`-` for cargo, exact for npm. Display names are never
changed. When nothing matches, suggestions are the normalized names within edit
distance 2 of the query, closest first.

### Complexity

Building the sorted parent lists is `O(E log E)` once per graph. After that, each
Why, Remove or Dupes query is roughly linear in the size of the graph. A test
holds `Why`, `Remove` and `Dupes` under 200 ms on a 3,000-node graph with fan-out 5.

## Output (`internal/render`)

`render` receives finished results and prints them. It never decides what is in a
result.

- **Text** (`text.go`): header, box-drawn tree, footer. Color goes through a
  `Style` whose zero value is plain text, so tests and pipes get no escape codes.
  `Enabled` implements the rule: a terminal, `NO_COLOR` unset or empty, and no
  `--no-color`.
- **JSON** (`json.go`): a `Doc` with `schema`, `mode`, `query`, `results`,
  `warnings` and, only for a not-found run, `suggestions`. The `*JSON` structs are
  the wire format. Arrays are always built non-nil so they encode as `[]`, never
  `null`. The schema is versioned because it becomes a stability promise at 1.0.

## Online lookups (`internal/health`)

Only used with `--online` (SPEC §12). Everything else in depwhy is offline and
deterministic; this package is the one place that does network I/O, and `cli` is
the only package that calls it. It works on plain `Pkg{Ecosystem, Name, Version}`
values, so it knows nothing about graphs.

```
cli: collect the packages a result will show ─▶ health.Lookup ─▶ render.Notes ─▶ render
```

`Client.Lookup` takes the shown packages and returns what it learned:

1. Skip packages that name no published release (no version, or a version like
   `file:../x`).
2. Serve what it can from the **cache**: one small JSON file per package, named
   by a SHA-256 of `ecosystem:name@version` (safe for scoped names on every OS),
   valid for 24 hours. `--refresh` skips reads but still writes.
3. Ask **OSV** about the rest in one `querybatch` request per 1,000 packages.
   Results come back in query order and are matched by position; a wrong count
   is treated as a failure rather than guessed at.
4. Ask each package's **registry** (npm, PyPI, crates.io) whether it is
   deprecated or yanked: one request per package, at most 8 at a time, 5 seconds
   each. A 404 means "no information".
5. Cache only complete answers, so a failed lookup is retried next time.

Failures never stop a run. They are folded into at most one warning per service
(for example `online lookup failed: npm registry (3 packages): ...`), the exit
code is unaffected, and the affected packages are printed without information.
The client's URLs are fields, so every test talks to a local `httptest` server.

`cli` converts the result into `render.Notes` (a map from node index to the
findings) and puts it on the `render.Style` it hands to the renderers; the JSON
builders take the same value. A nil `Notes` means offline, which is why offline
output is byte-identical to v0.1.

## The command line (`internal/cli`)

`Run(args, stdout, stderr) int` returns the exit code instead of calling
`os.Exit` and writes to the streams it is given, so the whole program can be
tested in-process.

Flow: parse flags with pflag, validate combinations, find lockfiles, load and
parse each one, run the mode, render.

Exit codes are `0` success, `1` package not found, `2` usage error or an
unreadable lockfile. Warnings go to stderr as `depwhy: warning: ...` and never
change the exit code. In `--json` mode a not-found result still prints a JSON
document on stdout, and the error message still goes to stderr.

When several lockfiles are present they are all loaded and a query is run against
each; results are printed in lockfile order.

The reported version comes from `-ldflags` in release builds, then from the module
version embedded by `go install pkg@version`, then falls back to `dev`.

## Testing

| Layer | How |
|---|---|
| `graph` | Hand-built graphs through the `Builder`: diamonds, cycles, direct and dev-only targets, the path cap, `--prod`, `--depth`. |
| `lockfile` | One test file per format, using the fixtures plus inline data for edge cases. |
| `render` | Exact expected strings for the text output; decoded structure for JSON. |
| `health` | Local `httptest` servers standing in for OSV and the registries: batching, caching and TTL, `--refresh`, 404s, timeouts, bad bodies, concurrency limit, warning aggregation. No test uses the internet. |
| `cli` | **Golden tests.** `Run` is executed against every fixture project for ten argument lists; stdout, stderr and the exit code are compared with `testdata/golden/*.txt`. |
| speed | A guard test builds a 3,000-node graph and fails if a query takes 200 ms or more. It is skipped under `-short` and `-race`. |

All four fixture projects encode the same logical graph so their goldens are
comparable (the npm and pnpm goldens differ only in the lockfile name):

```
app (root)
├── a@1.0.0   prod
│   ├── c@1.0.0
│   └── d@1.0.0
└── b@1.0.0   dev
    ├── c@2.0.0   (uv has one c: a Python environment cannot hold two versions)
    └── d@1.0.0
```

Run `go test ./internal/cli -update` to rewrite the goldens, then **read every
changed golden against the spec**. A golden is the spec in executable form, so a
wrong one is a bug that hides itself.

The `uv` and `cargo` lockfiles are stored as `uv.lock.fixture` and
`Cargo.lock.fixture`. The golden test copies each fixture directory to a
temporary directory and drops the `.fixture` suffix before running the CLI, so
the program under test sees the real file names. The parser tests read the same
files.

## CI and releases

`.github/workflows/ci.yml` runs on every pull request into `master` and every push
to it. Every action is pinned to a commit SHA, and a newer push cancels the run it
supersedes. The jobs form a fail-fast pipeline:

```
dependencies ──┬──▶ lint ──────────┐
               ├──▶ test (matrix) ─┼──▶ build artifacts
               └──▶ govulncheck ───┘
```

| Job | What it enforces |
|---|---|
| dependencies | `go mod download`, `go mod verify`, and that `go.mod`/`go.sum` are tidy. Warms the module cache. |
| lint | `gofmt`, `go vet`, `golangci-lint` (config in `.golangci.yml`). |
| test | `go test -race` on Linux, macOS and Windows with the minimum Go version in `go.mod`, plus one Linux leg on the newest Go. Total coverage is reported to the job summary, uploaded as an artifact, and must be at least 90%. |
| govulncheck | Known vulnerabilities in dependencies and reachable standard-library code. |
| build artifacts | A GoReleaser snapshot build for every OS/arch target, uploaded as an artifact. Runs only after the jobs above pass, and catches release-config mistakes before a tag does. |

`.github/workflows/release.yml` runs on a `v*` tag. It runs vet and the race tests
first and only then runs GoReleaser, which builds `CGO_ENABLED=0` binaries for
Linux, macOS and Windows on amd64 and arm64, sets `internal/cli.Version` through
`-ldflags`, and publishes archives with checksums and a changelog built from
Conventional Commit messages. It also commits a Homebrew cask to
`dpologdvinity/homebrew-tap` (a tap shared with other projects) using the
`HOMEBREW_TAP_GITHUB_TOKEN` secret; pre-release tags skip that step. Tags with a suffix such as `-rc.1` are published as
pre-releases.

### Distribution channels

| Channel | How it is produced |
|---|---|
| GitHub release archives, checksums | GoReleaser (`archives`, `checksum`) |
| `go install` | the module itself; the version comes from the embedded module info |
| Homebrew | GoReleaser commits `Casks/depwhy.rb` to `dpologdvinity/homebrew-tap` |
| `.deb`, `.rpm`, `.apk` | GoReleaser `nfpms`, attached to the release |
| apt repository | `apt.yml`, described below |
| npm | `npm/`: a zero-dependency launcher that downloads and checksum-verifies the release binary; published by `release.yml` |

**The npm package** (`npm/`) contains no binary. `bin/depwhy.js` finds the native
binary under `vendor/`, downloading it first if it is missing, and runs it with the
same arguments. `lib/install.js` picks the release asset for the machine (`platform.js`),
downloads `checksums.txt` and the archive from the GitHub release of the same version,
refuses to install unless the SHA-256 matches, and unpacks it with the system `tar`.
A postinstall script does this at install time; because npm 12 skips install scripts
by default, the launcher repeats it on first run, so both paths work. The release
workflow sets the package version from the tag and publishes with provenance.

**The apt repository** is a signed *flat* repository served from GitHub Pages at
`https://dpologdvinity.github.io/depwhy/apt`. Its state is derived, not stored:
`apt.yml` downloads the `.deb` files of every stable release, and
`scripts/build-apt-repo.sh` rebuilds the indexes from scratch (`dpkg-scanpackages
--multiversion`, so old versions stay installable), writes a `Release` file with
`apt-ftparchive`, and signs it (`Release.gpg` and a clear-signed `InRelease`). The
public key is published next to it as `key.gpg`. The private key lives only in the
`APT_GPG_PRIVATE_KEY` Actions secret. Because the site is rebuilt from the releases
every time, there is no branch of generated files to keep in sync, and republishing
(for example after a key rotation) is one manual run of the `apt` workflow.

CI proves the pipeline on every change: it builds a repository from the snapshot
`.deb` files with a throwaway key, installs from it in a clean Debian container, and
checks that a tampered index is rejected (`scripts/test-apt-repo.sh`). Pre-release
tags (`-rc.1`) never touch the repository.

Dependabot keeps Go modules and the pinned Actions up to date.

## Design decisions

- **Lockfiles first, network only on request.** Reading the lockfile gives the exact resolved tree with no
  network and no package manager installed. Online data is strictly additive and opt-in. The cost is that anything the
  lockfile does not record, such as cargo's dev and build edges, cannot be shown.
- **One graph shape for all ecosystems.** This is what makes polyglot repos and a
  single JSON schema possible, and it keeps the algorithms free of format logic.
- **Edge kinds come from the declaring field.** Dev-only status is computed from
  the graph, never read from per-package flags, so it means the same thing in
  every format.
- **Tree deduplication instead of path enumeration.** Printing every path through
  a real dependency graph can be exponential. Marking repeats with `(*)` keeps the
  tree linear, and a cap keeps the path list small.
- **Standard library first.** The only dependencies are pflag, a YAML library and
  a TOML library; tests use only `testing`. Levenshtein distance, color and
  terminal detection are a few lines each.

## Known limitations

- Cargo: no dev or build edges, and path dependencies outside a workspace appear
  as roots (SPEC §8.4).
- Platform markers (`uv.lock`) are ignored, so every edge counts on every platform.
- Workspace members cannot be queried by name; only packages are matched.
- Versions are compared as strings, not semantic versions.

See [docs/ROADMAP.md](docs/ROADMAP.md) for what is planned next.
