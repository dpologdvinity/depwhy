# depwhy — Specification (v0.1)

`depwhy` is a single-binary CLI that explains why a package is in a project's
dependency tree, what removing a direct dependency would free, and which
packages are installed at more than one version. It reads lockfiles directly,
works offline, and uses one interface across ecosystems.

This document is the source of truth for v0.1 behavior. `docs/PLAN.md` says how
to build it, and `docs/ROADMAP.md` says what comes after. If the plan and the spec
disagree, the spec wins.

## 1. Goals and non-goals

### Goals (v0.1)
- `depwhy <pkg>`: show every chain from the project's direct dependencies to `<pkg>`.
- `depwhy --remove <pkg>`: show which packages would leave the tree if direct dependency `<pkg>` were removed, and which would stay (and why).
- `depwhy --dupes`: list packages present at more than one version, and which direct dependencies pull in each version.
- Support four lockfile formats: `package-lock.json` (v2/v3), `pnpm-lock.yaml` (v9), `uv.lock`, `Cargo.lock` (v3/v4).
- Polyglot: if a directory has lockfiles for several ecosystems, load all of them and search all of them.
- Offline, read-only, zero config, and fast (under 200 ms on a 3,000-package lockfile).
- Stable `--json` output for scripting.

### Non-goals (v0.1)
- No network access of any kind, and no advisory, deprecation, or size lookup (see ROADMAP v0.2/v0.3).
- No modifying lockfiles or manifests. `--remove` is a dry-run analysis only.
- No resolving dependencies from manifests (`package.json`, `pyproject.toml`). Only lockfiles are read, with one exception: §5.2 reads `package.json` `name` for display labels.
- No `yarn.lock`, `bun.lock`, `poetry.lock`, `go.sum`, or `requirements.txt` (see ROADMAP).
- No recursive directory scanning. Only the given directory is checked.
- No config file and no TUI.

## 2. Tech stack (fixed)

| Item | Choice |
|---|---|
| Language | Go (module requires `go 1.23`, builds with any newer toolchain) |
| Module path | `github.com/dpologdvinity/depwhy` |
| Flags | `github.com/spf13/pflag` (GNU-style, flags may appear after positional args) |
| YAML | `go.yaml.in/yaml/v3` (maintained successor of `gopkg.in/yaml.v3`) |
| TOML | `github.com/BurntSushi/toml` |
| JSON | stdlib `encoding/json` |
| Tests | stdlib `testing` only, with golden files |
| Release | GoReleaser + GitHub Actions |

Add no other dependencies in v0.1. Color and TTY detection use only the stdlib (§7.4).

## 3. CLI interface

```
depwhy [flags] <package>[@version]      explain why <package> is installed
depwhy [flags] --remove <package>       impact of removing a direct dependency
depwhy [flags] --dupes                  packages installed at more than one version
depwhy --version | --help
```

Exactly one mode runs per invocation. Positional `<package>` is required for
"why" and `--remove`, and must be absent for `--dupes`. Violations are usage
errors (exit 2).

### 3.1 Flags

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--remove` | bool | false | Removal-impact mode. The positional arg is the package. |
| `--dupes` | bool | false | Duplicates mode. |
| `-C`, `--dir` | string | `.` | Directory to look for lockfiles in. |
| `-l`, `--lockfile` | string (repeatable) | none | Use these lockfile paths instead of auto-detection. The format comes from the file's base name (§4). |
| `-e`, `--ecosystem` | string | all | Restrict to `npm`, `python`, or `cargo`. |
| `--prod` | bool | false | Ignore dev edges from roots (and so everything reachable only through them). |
| `-d`, `--depth` | int | 0 (unlimited) | Max levels printed in the why tree (target = level 0). |
| `--all` | bool | false | Print all trees and paths without the 10-path cap (§6.1). |
| `--json` | bool | false | Emit JSON (§7.3) instead of text. |
| `--no-color` | bool | false | Disable ANSI color. |
| `-V`, `--version` | bool | false | Print `depwhy <version>` and exit 0. |
| `-h`, `--help` | bool | false | Print usage and exit 0. |

`--remove` and `--dupes` together is a usage error.

### 3.2 Package argument

- Form: `name` or `name@version`. Split at the last `@` whose index is greater than 0, so `@babel/core@7.24.0` gives name `@babel/core` and version `7.24.0`, and `@babel/core` gives name `@babel/core` with no version.
- The version, if given, must match a node's version string exactly (no ranges).
- Name matching is normalized per ecosystem (§5.5).

### 3.3 Exit codes

| Code | When |
|---|---|
| 0 | Success. The package was found, or `--dupes` ran (even with zero dupes). |
| 1 | The package was not found in any loaded lockfile. |
| 2 | Usage error, no lockfile found, or a lockfile could not be read or parsed. |

Errors go to stderr as `depwhy: <message>`. Warnings go to stderr as
`depwhy: warning: <message>` and never change the exit code.

## 4. Lockfile detection

When `--lockfile` is not given, look in `--dir` for these exact file names:

| File | Ecosystem | Format id |
|---|---|---|
| `pnpm-lock.yaml` | npm | `pnpm` |
| `package-lock.json` | npm | `npm` |
| `uv.lock` | python | `uv` |
| `Cargo.lock` | cargo | `cargo` |

Rules:
1. At most one lockfile per ecosystem. If both `pnpm-lock.yaml` and `package-lock.json` exist, use `pnpm-lock.yaml` and warn: `both pnpm-lock.yaml and package-lock.json found; using pnpm-lock.yaml`.
2. `--ecosystem` filters the detected set.
3. If no lockfile is found after filtering, exit 2 with: `no supported lockfile in <dir> (looked for pnpm-lock.yaml, package-lock.json, uv.lock, Cargo.lock)`.
4. With `--lockfile`, the base name must be one of the four names above, or exit 2 with `unrecognized lockfile name: <name>`. Rule 1 does not apply to explicit paths.
5. Load lockfiles in the table order above. That order is also the output order.

## 5. Data model

### 5.1 Graph (package `internal/graph`)

```go
type Kind uint8

const (
    Prod Kind = iota // regular dependency
    Dev              // devDependencies / dev groups
    Optional         // optionalDependencies / extras
    Peer             // npm peerDependencies
)

type Node struct {
    ID        string // "<name>@<version>", or "root:<label>" for roots
    Name      string // display name
    Version   string // "" for roots that have none
    Ecosystem string // "npm" | "python" | "cargo"
    Root      bool   // true for the project / workspace members
}

type Edge struct {
    From, To int // node indexes
    Kind     Kind
}

type Graph struct {
    Ecosystem string
    Lockfile  string // path as given/detected
    Nodes     []Node
    Out       [][]Edge // Out[i]: edges from node i
    In        [][]Edge // In[i]:  edges into node i
    Roots     []int    // indexes of Root nodes, in lockfile order
}
```

Invariants every parser must keep:
- Node IDs are unique within a graph. Two lockfile entries with the same name and version become one node.
- `In` is the exact mirror of `Out`.
- No duplicate edges with the same `(From, To, Kind)`.
- Every node is reachable from some root, or a warning is logged and the node is kept (lockfiles sometimes contain orphans).
- `Roots` is non-empty.

Provide a builder so parsers don't touch the slices directly:

```go
type Builder struct{ /* ... */ }
func NewBuilder(ecosystem, lockfile string) *Builder
func (b *Builder) Node(name, version string) int        // get-or-create by ID
func (b *Builder) Root(label, version string) int       // get-or-create root
func (b *Builder) Edge(from, to int, k Kind)            // dedupes
func (b *Builder) Build() (*Graph, error)                // error if no roots
```

### 5.2 Root labels

- npm: the `name` field of the root/workspace entry, or the entry's key path if `name` is missing (`.` for the root).
- pnpm: the `name` from `<dir>/<importer>/package.json` if that file is readable and has a name, otherwise the importer key (`.`, `packages/a`).
- uv: the package `name`.
- cargo: the package `name`.

### 5.3 Direct dependencies, dev-only nodes

- A direct dependency is any node `v` with an edge `root → v`.
- A node is dev-only if it is reachable from the roots only through at least one `Dev` edge leaving a root. Compute this with two BFS passes from the roots: one following all edges, and one that skips `Dev` edges whose `From` is a root. Nodes in the first set but not the second are dev-only.
- With `--prod`, drop dev-only nodes from all results. If the target is dev-only, it counts as not found, exit 1, with the hint `(found only via dev dependencies; drop --prod)`.

### 5.4 Edge kind for transitive edges

Each parser takes the kind from the field that declared the edge
(`dependencies` gives Prod, `optionalDependencies` gives Optional, and so on). Dev edges
only come from roots, except where a format records dev deps on non-root
packages (uv workspace members are roots anyway).

### 5.5 Name normalization (for matching the user's query only)

Display names always come from the lockfile, unchanged.

| Ecosystem | Normalize |
|---|---|
| npm | none (case-sensitive, exact) |
| python | PEP 503: lowercase, and replace each run of `-`, `_`, `.` with a single `-` |
| cargo | lowercase, and `_` becomes `-` |

## 6. Algorithms (package `internal/graph`)

### 6.1 Why (`func (g *Graph) Why(target int, opts WhyOpts) WhyResult`)

The query may match several nodes (one per version, and one per ecosystem). Run Why
for each matched node and print them in order: ecosystem order (§4), then version
ascending by plain string comparison of `Version`.

For a single target `t`, build the reverse tree:
- The tree root is `t`. The children of a node `n` are the `From` ends of `In[n]`, sorted by `(Root first, Name, Version)`.
- Expand depth-first. Keep an `onPath` set and a global `expanded` set:
  - If the child is on the current path, print it with the marker `(cycle)` and don't expand it.
  - If the child was already expanded elsewhere in this tree, print it with `(*)` and don't expand it.
  - If the child is a root, print it as a leaf with its label and the edge kind.
- Stop expanding at `--depth` levels if set, and print `…` as the only child of a truncated node.
- Collect `Leads []Lead` with one entry per direct dependency that has a path to `t`. Each entry holds `{Root, Direct, Kind}`, where Kind is the kind of the `root → direct` edge. If `t` is itself direct, it appears in Leads with itself as `Direct`.
- The path cap: count the root-terminated leaves printed. After 10, stop adding new branches and print `… N more paths (use --all)`. For N, keep counting leaves with a DFS bounded at 10,000 visits, and print `N+` if that bound is hit. `--all` removes the cap.

### 6.2 Remove (`func (g *Graph) Remove(target int) RemoveResult`)

1. If `t` has no incoming edge from a root, it is not a direct dependency. Return `NotDirect=true` with `RequiredBy` set to the `Leads` of §6.1. Exit code is 0.
2. `before` = nodes reachable from the roots following all edges.
3. `after` = nodes reachable from the roots while ignoring every `root → t` edge. Edges into `t` from non-roots are still followed, which is what lets `t` stay if something else needs it.
4. `Freed` = `before − after`, sorted by `(Name, Version)`. This includes `t` unless `t` is still needed.
5. `Kept` = nodes reachable from `t` (excluding `t`) that are in `after`. For each kept node, `Via` = one shortest path from a root to that node in the graph without the `root → t` edges (BFS, ties broken by sorted order). Print the first non-root node on that path.
6. If `t` itself is in `after`, set `StillNeeded=true` with `Via` as in step 5.
7. With `--prod`, compute everything on the graph with dev-only nodes removed.

### 6.3 Dupes (`func (g *Graph) Dupes() []Dupe`)

- Group non-root nodes by `Name` (the lockfile name, not the normalized one). Keep groups with two or more versions.
- For each version, `Leads` = the direct dependencies that reach it (as in §6.1 Leads).
- Sort groups by `(count of versions desc, Name asc)`, and sort versions ascending.

### 6.4 Not-found suggestions

If no node matches, collect normalized names within Levenshtein distance ≤ 2 of
the normalized query, across all loaded graphs. Print up to 3, sorted by
`(distance, name)`:
```
depwhy: lodahs not found in package-lock.json, uv.lock
did you mean: lodash, lodash.get
```

## 7. Output

### 7.1 Why (text)

```
c@1.0.0  npm · package-lock.json
└── a@1.0.0
    └── app (root, prod)

c@2.0.0  npm · package-lock.json
└── b@1.0.0
    └── app (root, dev)

d@1.0.0  npm · package-lock.json
├── a@1.0.0
│   └── app (root, prod)
└── b@1.0.0
    └── app (root, dev)
pulled in by 2 direct dependencies: a (prod), b (dev)
```

- Header: `<name>@<version>  <ecosystem> · <lockfile base name>`, bold when color is on.
- Tree drawing uses `├── `, `└── `, `│   `, and `    `.
- Root leaf: `<label> (root, <kind>)`, where kind is the `root → direct` edge kind in lowercase. If the target is itself direct, its children are only root leaves.
- Footer: if the target is direct, `direct dependency of <root label> (<kind>)`. Otherwise, if there are two or more leads, `pulled in by N direct dependencies: x (kind), y (kind)`, with leads sorted by name. Otherwise, no footer.
- Print a blank line between trees.

### 7.2 Remove and Dupes (text)

```
removing a@1.0.0 (direct, prod) from app  npm · package-lock.json
would remove 2 packages:
  a@1.0.0
  c@1.0.0
still needed by others (1):
  d@1.0.0  via b@1.0.0
```

The not-direct case:
```
c@1.0.0 is not a direct dependency. it is required by: a (prod)
remove those instead, or run `depwhy c` for details.
```

The still-needed case appends: `a@1.0.0 itself stays: also required via <node>`.

Dupes:
```
npm · package-lock.json
c  2 versions
  1.0.0  via a (prod)
  2.0.0  via b (dev)
```
Zero dupes: `no duplicate packages` (per ecosystem).

### 7.3 JSON

Top-level keys are always present. Arrays are never `null` (use `[]`). Field names
are snake_case. This schema becomes a stability promise at 1.0.

```json
{
  "schema": 1,
  "mode": "why",
  "query": {"name": "c", "version": null},
  "results": [
    {
      "ecosystem": "npm",
      "lockfile": "package-lock.json",
      "package": {"name": "c", "version": "1.0.0"},
      "dev_only": false,
      "direct": false,
      "paths": [
        [
          {"name": "app", "version": "1.0.0", "root": true},
          {"name": "a", "version": "1.0.0", "kind": "prod"},
          {"name": "c", "version": "1.0.0", "kind": "prod"}
        ]
      ],
      "paths_truncated": false,
      "leads": [{"root": "app", "name": "a", "version": "1.0.0", "kind": "prod"}]
    }
  ],
  "warnings": []
}
```

- `paths` are written root-first (the reverse of the text tree). Each element after the first carries the `kind` of the edge that led to it.
- The 10-path cap applies unless `--all` is set. `paths_truncated` says whether it cut anything.
- Remove mode: `"mode":"remove"`, and each result has `package`, `direct`, `still_needed`, `freed: [{name,version}]`, `kept: [{name,version,via:{name,version}}]`, and `required_by` (leads, used when not direct).
- Dupes mode: `"mode":"dupes"`, and each result has `ecosystem`, `lockfile`, and `dupes: [{name, versions:[{version, leads:[…]}]}]`.
- Not found (exit 1): `results: []`, plus `"suggestions": [..]`.
- Errors with exit 2 are still plain text on stderr, with no JSON.

### 7.4 Color

Use ANSI color only when stdout is a terminal (`fi.Mode()&os.ModeCharDevice != 0`),
`NO_COLOR` is unset or empty, and `--no-color` is not set. Colors: header bold,
root labels cyan, `(dev)`/`dev` kinds dim, markers `(*)`/`(cycle)` dim. Golden tests
run with color off.

## 8. Lockfile parsing (package `internal/lockfile`)

Each parser has the signature `func ParseX(path string, data []byte) (*graph.Graph, []string /*warnings*/, error)`.
A dispatcher `Load(path string) (*graph.Graph, []string, error)` picks the parser by base name.
Parse errors are wrapped as `parse <path>: <cause>`.

### 8.1 npm `package-lock.json`

- Read `lockfileVersion`. If it is 2 or 3, use the `packages` map. If it is 1 or missing, return the error `package-lock.json lockfileVersion 1 is not supported; regenerate it with npm >= 7 (npm install)`. Ignore the legacy `dependencies` map.
- Keys:
  - `""` is the root project, so make it a root.
  - A key that does not contain `node_modules/` is a workspace member directory (for example `packages/a`), so make it a root.
  - Other keys are installed packages. The name is the `name` field if present, otherwise the substring after the last `node_modules/`, which keeps scopes (`node_modules/@x/y` gives `@x/y`).
  - An entry with `"link": true` is a symlink to the key in its `resolved` field. Don't create a node for it. Edge resolution follows it.
- Edges: for each entry at key `P`, for each `name` in `dependencies` (Prod), `optionalDependencies` (Optional), `peerDependencies` (Peer), and, for root/workspace entries only, `devDependencies` (Dev):
  - Resolve with Node's lookup: try `P + "/node_modules/" + name` (or `"node_modules/" + name` when `P == ""`). Then repeatedly strip the last `/node_modules/<seg>` (scoped names take two segments) from `P` and retry, ending with `node_modules/<name>`.
  - If the resolved entry is a link, continue from its `resolved` key (a workspace root).
  - If it is unresolved and the kind is Optional or Peer, skip it silently. If it is unresolved and the kind is Prod or Dev, add the warning `unresolved dependency <name> of <P>`.
- Ignore `dev`, `optional`, `peer`, `resolved`, `integrity`, and `engines` fields on entries. Kinds come from edges, and dev-only status is computed (§5.3).

### 8.2 pnpm `pnpm-lock.yaml`

- `lockfileVersion` must be a string starting with `9.`. Otherwise return the error `pnpm-lock.yaml lockfileVersion <v> is not supported; need 9.x (pnpm >= 9)`.
- `importers`: each key (`.`, `packages/a`) is a root. Its `dependencies`, `devDependencies`, and `optionalDependencies` map `name → {specifier, version}`.
- `snapshots`: each key is `<name>@<version>[(<peer>@<v>)...]`, and each value has `dependencies` and `optionalDependencies` mapping `name → ref`.
- Node identity: strip everything from the first `(` in a snapshot key or ref. The node ID is the result. Split it at the last `@` with index greater than 0 to get name and version. Different peer variants of the same package therefore become one node, with their edges merged.
- Resolving a ref for dependency `depName`:
  1. `ref` starts with `link:`: the target is the importer at `path.Clean(path.Join(importerKey, strings.TrimPrefix(ref, "link:")))`. If that is not an importer, warn and skip.
  2. `ref` starts with `file:`: create a node named `depName` with version `ref`.
  3. After stripping parens, `ref` contains an `@` at index greater than 0: it is an alias, and the target key is the ref itself (`string-width@4.2.3`).
  4. Otherwise the target key is `depName + "@" + ref`.
- A snapshot that is missing a target key is still created as a node from its key. Warn only if neither `snapshots` nor `packages` has it.
- The `packages` section is used only to confirm existence. Its values are ignored.

### 8.3 uv `uv.lock`

- TOML. Read top-level `version` (expect 1) and ignore `revision`. Any other version produces a warning but parsing continues.
- `[[package]]` entries have `name`, `version` (may be absent, so use `""`), `source` (an inline table with exactly one key out of `registry`, `git`, `path`, `directory`, `editable`, `virtual`, `url`), `dependencies`, `optional-dependencies` (map of extra name → list), and `dev-dependencies` (map of group name → list). List items are `{name, version?, source?, marker?, extra?}`.
- Roots: if `[manifest].members` exists, every package whose name is in it. Otherwise, packages whose `source` is `{virtual = "."}` or `{editable = "."}`.
- Resolve a dependency item by name. If several packages share the name, filter by `version`, then by `source`. If it is still ambiguous, take the first and warn `ambiguous dependency <name> of <pkg>; picked <version>`.
- Kinds: `dependencies` give Prod, `optional-dependencies` give Optional, and `dev-dependencies` give Dev (from any package, but in practice only roots have them).
- Ignore `marker` in v0.1. All edges count regardless of platform.

### 8.4 cargo `Cargo.lock`

- TOML. `version` may be absent (old format), 3, or 4. Any other value produces a warning but parsing continues.
- `[[package]]` entries have `name`, `version`, `source` (absent for workspace or path crates), and `dependencies` (array of strings).
- A dependency string is `"name"`, `"name version"`, or `"name version (source)"`. Resolve by name, then by version if given, then by source if given. If several match, warn and pick the first.
- Roots: packages without `source`. **Known limitation:** non-workspace path dependencies also lack `source` and will show up as roots. Cargo.lock doesn't record dev or build edges either, so every edge is Prod. Print both limitations in `--help` under "Notes".

## 9. Package layout

```
depwhy/
├── main.go                     // os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
├── internal/
│   ├── cli/cli.go              // flag parsing, detection, mode dispatch, exit codes
│   ├── cli/cli_test.go         // golden end-to-end tests
│   ├── graph/graph.go          // types + Builder
│   ├── graph/why.go            // Why, Leads, dev-only
│   ├── graph/remove.go
│   ├── graph/dupes.go
│   ├── graph/match.go          // normalization, query split, suggestions
│   ├── graph/*_test.go
│   ├── lockfile/load.go        // Detect, Load dispatcher
│   ├── lockfile/npm.go
│   ├── lockfile/pnpm.go
│   ├── lockfile/uv.go
│   ├── lockfile/cargo.go
│   ├── lockfile/*_test.go
│   └── render/{text.go,json.go,color.go}
├── testdata/
│   ├── projects/<name>/<lockfile>   // fixtures (see PLAN)
│   └── golden/<name>__<case>.txt    // expected stdout+exit code
├── .goreleaser.yaml
├── .github/workflows/{ci.yml,release.yml}
├── README.md
└── LICENSE                      // MIT
```

`internal/graph` imports only the stdlib. `internal/lockfile` imports `graph`,
yaml, and toml. `internal/render` imports `graph`. `internal/cli` imports all of them.

## 10. Testing requirements

- Unit tests for every function in §6, using hand-built graphs and including a cycle (a→b→a), a diamond, a target that is direct, a target that is dev-only, and `--prod`.
- A parser test per format using the fixtures in PLAN Task 3. Assert node IDs, roots, and edges with kinds.
- Golden tests: `cli.Run` is executed against each fixture directory with a set of argument lists. The combined stdout and `exit=<code>` are compared to `testdata/golden/*.txt`. `go test ./internal/cli -update` rewrites the golden files.
- `go vet ./...` and `gofmt -l .` (which must print nothing) run in CI.
- Performance: a plain test (skipped under `testing.Short()`) builds a synthetic 3,000-node graph and fails if `Why` or `Remove` takes 200 ms or more.

## 11. Definition of done (v0.1)

- Every command in §3 behaves as specified on all four fixtures, and the golden tests pass.
- `go test ./...`, `go vet ./...`, and `gofmt -l .` are clean.
- Manual check on one real project per ecosystem, with the results noted in the PR description.
- README with install, usage examples (copied from golden outputs), and the cargo limitations.
- A tagged `v0.1.0` release builds binaries for linux/darwin/windows × amd64/arm64 through GoReleaser.
