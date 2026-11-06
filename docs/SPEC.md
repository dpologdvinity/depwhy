# depwhy — Specification

`depwhy` is a single-binary CLI that explains why a package is in a project's
dependency tree, what removing a direct dependency would free, and which
packages are installed at more than one version. It reads lockfiles directly,
works offline, and uses one interface across ecosystems.

This document is the source of truth for depwhy's behavior. §1–§11 specify the
v0.1 core; later sections add each release's features (§12 online lookups, §13
sizes, §14 more formats, §15 monorepos, §16 shell completion) and override the
earlier text where they differ. The frozen `--json` contract is
[json-schema.md](json-schema.md). `docs/PLAN.md` says how the early versions were
built and `docs/ROADMAP.md` what comes next. If the plan and the spec disagree, the
spec wins.

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
- No network access of any kind, and no advisory, deprecation, or size lookup (added later as opt-in: `--online` in §12, installed sizes in §13).
- No modifying lockfiles or manifests. `--remove` is a dry-run analysis only.
- No resolving dependencies from manifests (`package.json`, `pyproject.toml`). Only lockfiles are read, with one exception: §5.2 reads `package.json` `name` for display labels.
- No `yarn.lock`, `bun.lock`, `poetry.lock`, `go.sum`, or `requirements.txt` (v0.4 added Yarn, Bun, Poetry and `go.mod`, §14; `go.sum` and `requirements.txt` remain out of scope).
- No recursive directory scanning. Only the given directory is checked (v0.5 added opt-in `-r`, §15).
- No config file and no TUI.

## 2. Tech stack (fixed)

| Item | Choice |
|---|---|
| Language | Go (module requires `go 1.26`, builds with any newer toolchain; the minimum is raised when a supported release is needed for standard-library security fixes) |
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
| `-e`, `--ecosystem` | string | all | Restrict to `npm`, `python`, `cargo`, or `go`. |
| `--prod` | bool | false | Ignore dev edges from roots (and so everything reachable only through them). |
| `-d`, `--depth` | int | 0 (unlimited) | Max levels printed in the why tree (target = level 0). Why mode only. |
| `--all` | bool | false | Print all trees and paths without the 10-path cap (§6.1). Why mode only. |
| `--json` | bool | false | Emit JSON (§7.3) instead of text. |
| `--no-color` | bool | false | Disable ANSI color. |
| `--online` | bool | false | v0.2: look up advisories and deprecated/yanked status (§12). |
| `--refresh` | bool | false | v0.2: with `--online`, ignore cached responses (§12). |
| `-V`, `--version` | bool | false | Print `depwhy <version>` and exit 0. |
| `-h`, `--help` | bool | false | Print usage and exit 0. |

`--remove`, `--dupes` and `--heaviest` are mutually exclusive. `--depth` and `--all`
shape the why tree, so combining them with another mode is a usage error rather
than being silently ignored.

### 3.2 Package argument

- Form: `name` or `name@version`. Split at the last `@` whose index is greater than 0, so `@babel/core@7.24.0` gives name `@babel/core` and version `7.24.0`, and `@babel/core` gives name `@babel/core` with no version.
- The version, if given, must match a node's version string exactly (no ranges). Go module versions always start with `v`, so for Go the `v` may be left out (`c@1.2.0` matches `v1.2.0`).
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

When `--lockfile` is not given, look in `--dir` for these exact file names, in this
priority order:

| File | Ecosystem | Format id |
|---|---|---|
| `pnpm-lock.yaml` | npm | `pnpm` |
| `yarn.lock` | npm | `yarn` |
| `bun.lock` | npm | `bun` |
| `package-lock.json` | npm | `npm` |
| `uv.lock` | python | `uv` |
| `poetry.lock` | python | `poetry` |
| `Cargo.lock` | cargo | `cargo` |
| `go.mod` | go | `gomod` |

Rules:
1. At most one lockfile per ecosystem: the first one in the table wins, with a warning such as `both pnpm-lock.yaml and package-lock.json found; using pnpm-lock.yaml`.
2. `--ecosystem` filters the detected set.
3. If no lockfile is found after filtering, exit 2 with: `no supported lockfile in <dir> (looked for pnpm-lock.yaml, yarn.lock, bun.lock, package-lock.json, uv.lock, poetry.lock, Cargo.lock, go.mod)` (with `-r`: `no supported lockfile in <dir> or its subdirectories`).
4. With `--lockfile`, the base name must be one of the names above, or exit 2 with `unrecognized lockfile name: <name>`. Rule 1 does not apply to explicit paths.
5. Load lockfiles in the table order above. That order is also the output order. (`-r` applies the same rules per directory, §15.1.)

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
    Ecosystem string // "npm" | "python" | "cargo" | "go"
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
ascending by the version order of §6.5.

For a single target `t`, build the reverse tree:
- The tree root is `t`. The children of a node `n` are the `From` ends of `In[n]`, sorted by `(Root first, Name, Version)`.
- Expand depth-first. Keep an `onPath` set and a global `expanded` set:
  - If the child is on the current path, print it with the marker `(cycle)` and don't expand it.
  - If the child was already expanded elsewhere in this tree, print it with `(*)` and don't expand it.
  - If the child is a root, print it as a leaf with its label and the edge kind.
- Stop expanding at `--depth` levels if set, and print `…` as the only child of a truncated node.
- Collect `Leads []Lead` with one entry per direct dependency that has a path to `t`. Each entry holds `{Root, Direct, Kind}`, where Kind is the kind of the `root → direct` edge. If `t` is itself direct, it appears in Leads with itself as `Direct`.
- The path cap: count the root-terminated leaves printed. After 10, stop adding new branches and print `… N more paths (use --all)`. For N, count remaining leaves in the deduplicated tree (linear walk). `--all` removes the cap.

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
- Sort groups by `(count of versions desc, Name asc)`, and sort versions ascending (§6.5).

### 6.4 Not-found suggestions

If no node matches, collect normalized names within Levenshtein distance ≤ 2 of
the normalized query, across all loaded graphs, leaving out the query itself. Print
up to 3, sorted by `(distance, name)`:
```
depwhy: lodahs not found in package-lock.json, uv.lock
did you mean: lodash, lodash.get
```

If the name is installed and only the version (or `--prod`) excluded it, there are no
suggestions. A version miss names the installed versions instead, and a `--prod` miss
says so:
```
depwhy: lodash@9.9.9 not found in package-lock.json (installed: lodash@4.17.21)
depwhy: mocha not found in package-lock.json (found only via dev dependencies; drop --prod)
```

### 6.5 Version order

Every place that orders versions (§6.1 to §6.3, §15.2) uses one comparator, a total
order that returns equal only for identical strings. Keys, in order:

1. PEP 440 epoch `N!` (absent is 0). A leading `v` (Go) is ignored.
2. Release: the leading dot-separated numbers, compared numerically, with missing
   segments as 0 (`9.0.0` < `10.0.0`; `1.0` ties `1.0.0`).
3. Suffix after the release, up to `+`, with a leading `-`, `.` or `_` dropped:
   `dev…` < any other suffix (semver or PEP 440 pre-release, Go pseudo-version) <
   no suffix < `post…`. Equal ranks compare naturally: digit runs by value and before
   letters, other runs bytewise (`alpha.2` < `alpha.10`).
4. Build metadata or local version after `+`, compared naturally; none first.
5. The raw strings, bytewise.

PEP 440 implicit post releases (`1.0-1`) sort as pre-releases, and spellings such as
`alpha` and `a` are not normalized.

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
- A target reachable from no root (an orphaned lockfile entry) has no tree and no
  footer: print `not reachable from any root (an orphaned lockfile entry)` under
  its header. It was found, so the exit code stays 0. `--remove` on it says it is
  not a direct dependency.
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
are snake_case. The complete, frozen schema is [docs/json-schema.md](json-schema.md),
which wins over this section where they differ.

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

### 7.5 Untrusted text

A lockfile may be hostile, so text output never prints its strings raw. Package
names, versions, root and lockfile labels, warnings and the not-found lines on
stderr show every non-printable rune (escape sequences, CR, BEL, DEL, C1 controls,
invisible formatting such as bidirectional overrides) as its Go escape, e.g. `\x1b`
or `\u202e`. JSON carries the exact strings; `encoding/json` escapes them.

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
yaml, and toml. `internal/render` imports `graph`. `internal/cli` imports all of them (v0.2 adds `internal/health`, §12.8).

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

## 12. v0.2: online health information

v0.2 adds opt-in network lookups. Without `--online`, every behavior and every byte
of output is identical to v0.1, and depwhy makes no network access of any kind.
Sections 1 to 11 stay in force; this section only adds to them.

### 12.1 Goals and non-goals
- Show known security advisories (OSV) and deprecated or yanked status next to
  packages in results.
- Network trouble never changes the exit code and never fails a run: it produces
  warnings and the result is printed without the missing information.
- Non-goals: no authentication, no private-registry support, no advisory
  severity or fix-version data, no lookups for packages that are not shown, and no
  background services.

### 12.2 Flags
| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--online` | bool | false | Look up advisories and deprecated/yanked status. |
| `--refresh` | bool | false | With `--online`, ignore cached responses (fresh answers are still cached). |

`--refresh` without `--online` is a usage error (exit 2).

### 12.3 Which packages are looked up
- **Advisories**: packages shown in the output are checked: the target and every
  package printed in its tree, plus the freed and kept packages of `--remove`
  and the versions listed by `--dupes`. Requests are batched up to 1,000 packages
  per OSV query. Packages with no version, or a version that is not a registry
  version (it contains `:` or `/`, such as `file:../x`), are skipped.
- **Deprecated/yanked**: only packages shown in the output are checked: the target
  and every package printed in its tree, plus the freed and kept packages of
  `--remove` and the versions listed by `--dupes`. One request per package.

### 12.4 Sources
| Data | Request | Reads |
|---|---|---|
| Advisories | `POST https://api.osv.dev/v1/querybatch`, body `{"queries":[{"package":{"name":N,"ecosystem":E},"version":V}]}` | `results[i].vulns[].id`; E is `npm`, `PyPI` or `crates.io` |
| npm deprecation | `GET https://registry.npmjs.org/<name>/<version>`; a scoped `@x/y` is requested as `@x%2Fy` | string field `deprecated` |
| PyPI yanked | `GET https://pypi.org/pypi/<name>/<version>/json` | `info.yanked` (bool) |
| crates.io yanked | `GET https://crates.io/api/v1/crates/<name>/<version>` | `version.yanked` (bool) |

Every request sends `User-Agent: depwhy/<version> (+https://github.com/dpologdvinity/depwhy)`
(crates.io requires one). Each request has a 5 second timeout, and at most 8
requests run at once. A 404 from a registry means "no information" and is not a
warning. Any other failure (network error, timeout, status other than 200 or 404,
unparsable body) adds a warning `online lookup failed: <what>: <cause>`, and the
affected packages are shown without that information. Advisory responses are
trusted only for the package they were asked about, matched by position; a
response with the wrong number of results is a failure. Redirects are followed
only within the original host. Packages named `.` or `..` are not looked up.

### 12.5 Cache
Responses are cached per package in `$XDG_CACHE_HOME/depwhy`, or in
`os.UserCacheDir()/depwhy` when that is unset. Cache files are named
`<sha256>.json`, where the hash is computed over the ecosystem, name and version
joined by NUL bytes, ensuring cross-platform safety for scoped packages and
characters invalid in file paths, and that different packages never share an entry.
An entry is valid for 24 hours. A missing, expired, future-dated or unreadable
entry is a miss. `--refresh` skips reads. A failure to write the cache is ignored. Failed
lookups are never cached.

### 12.6 Text output
After a package's `name@version`, add the findings separated by two spaces:
`⚠ <ID>, <ID>` for advisories (sorted by ID), `[deprecated: <message>]` for a
deprecated npm package, and `[yanked]` for a yanked Python or Rust package. This
applies to the heading and every package line in why trees, the freed and kept
lists of `--remove`, and the version lines of `--dupes`. Roots are never annotated.
Text from the services is shown only after control and invisible formatting
characters are removed and whitespace is collapsed; a deprecation message is cut at
80 characters.

```
d@1.0.0  npm · package-lock.json  ⚠ GHSA-xxxx-xxxx-xxxx
├── a@1.0.0  [deprecated: use b instead]
│   └── app (root, prod)
```

Color: advisories red, deprecated and yanked yellow, when color is enabled.

### 12.7 JSON output
With `--online`, each package object (`package`, `freed[]`, `kept[]`, and dupes
`versions[]`) gains `"advisories"`, `"deprecated": "<message>"` or `null`, and
`"yanked": true|false`. `advisories` is a list of IDs, empty `[]` for a package that
was checked and has none, and `null` for one that could not be checked (a failed
lookup, or a version that is not looked up), so a script never mistakes "unchecked"
for "clean". Warnings from
lookups are added to `warnings`. Without `--online` these keys are absent.
These additions are backward-compatible; `schema` remains `1` both with and
without `--online`.

### 12.8 Package layout
New package `internal/health` (imports only the standard library and `internal/graph`
is not needed: it works on plain `Pkg{Ecosystem, Name, Version}` values) holds the
client, the cache and the per-source parsers. `internal/render` receives findings
as plain data (`render.Notes`) and does no I/O. `internal/cli` is the only package
that connects them.

### 12.9 Testing
All network access in tests goes to local `httptest` servers; no test touches the
internet. Cover: batching at 1,000, caching and TTL, `--refresh`, 404 handling,
timeouts, malformed bodies, per-source parsing, scoped npm names, warnings that
leave the exit code unchanged, byte-identical output without `--online`, and golden
output for the annotated forms.

### 12.10 Definition of done (v0.2)
Everything in §12 behaves as described, tests and lint are green, a manual run against
the real services on one project per ecosystem matches the registries' own data,
README and ARCHITECTURE document the feature, and `v0.2.0` is released.

## 13. v0.3: Size and "what does it cost me"

v0.3 adds on-disk size inspection and removal savings analysis. Without installed package
directories present on disk, behavior is identical to v0.2.

### 13.1 Goals and non-goals
- On-disk size of installed packages, measured relative to the lockfile's own
  directory (also with `-l` and `-r`):
  - npm (`package-lock.json`): the install directory named by the package's
    lockfile key (`node_modules/<path>`, e.g. `node_modules/b/node_modules/c`),
    so every installed version is measured at its own copy.
  - npm (`pnpm-lock.yaml`, `yarn.lock`, `bun.lock`): pnpm's virtual store
    `node_modules/.pnpm/<name>@<version>[_<peers>]/node_modules/<name>` (scoped
    names use `+` for `/`), then `node_modules/<name>`, following its symlink.
  - Python (uv, poetry): the `*.dist-info` in the virtualenv's site-packages
    (`.venv` next to the lockfile, or `--venv`), matched by PEP 503 normalized
    name and version.
  - Cargo and Go: not measured.
- `--remove <pkg>` reports total disk space freed: `would remove N packages (freeing ~X MB):`.
- `depwhy --heaviest [N]`: rank direct dependencies by exclusive transitive size freed.
- Offline, read-only on disk, non-blocking if installed directories are missing.
- Non-goals: no downloading archives to compute uncompressed sizes; only locally installed packages are measured.

### 13.2 Flags
| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--heaviest` | int (optional, default 10) | 0 | Rank direct dependencies by exclusive transitive size freed. The count may be written `--heaviest=N` or `--heaviest N`; it must be positive. `--online` is a usage error with `--heaviest`. |
| `--venv` | string | `""` | Path to Python virtualenv (default `.venv` next to the lockfile). A relative path resolves against the current directory, like any path flag, not against `-C`. One virtualenv serves every Python lockfile of the run. When the given path is not a virtualenv with a `site-packages` directory, one warning is printed per run and Python packages stay unmeasured; a missing default `.venv` is silent. |

### 13.3 Measurement rules
- npm: the size is the sum of the regular files in the install directory,
  excluding its nested `node_modules` (those are other packages, measured on
  their own). Python: the sum of the regular files listed in the dist-info
  `RECORD` (read as CSV), checked on disk; entries that resolve outside the
  virtualenv are not counted, so scripts in its `bin` directory count.
- Symlinks are never followed while summing, and a symlink is never measured as
  its own length.
- Lockfile names and keys are untrusted. A candidate directory is measured only
  if it stays inside the lockfile's directory after symlinks are resolved and
  its `package.json` has exactly the node's name and version. Anything else
  (missing, another version, a stale install, an alias, a path that escapes) is
  *unmeasured*: unknown is preferred to wrong. A package installed at several
  paths is measured at one of them.
- Format sizes in human-readable units: `B`, `KB`, `MB`, `GB` (base 1024).
- Sizes are apparent file sizes. Package managers that hard-link files from a
  shared store (pnpm, uv) mean removing a package may reclaim little real disk
  space; the figures say how much the package occupies, not what `rm` would free.
- Unmeasured packages produce no error and no size annotation.
- Freed totals (`--remove`, `--heaviest`) count unmeasured packages as 0 and
  report how many there were, so a platform-specific optional package that is
  not installed (`fsevents`, `@esbuild/*`) does not hide the total. There is no
  total only when packages are freed and none was measured; such a direct dependency is
  left out of `--heaviest`, as is one that frees nothing.

### 13.4 Text output
- In why mode: `<name>@<version> (<size>)` on the header and tree lines when the size is known.
- In remove mode: `would remove N packages (freeing ~X MB):`, or
  `(freeing ~X MB; M not measured)` when M freed packages were unmeasured; the
  freed and kept lines carry `(<size>)` when known.
- In heaviest mode (`, M not measured` is added inside the parentheses when M > 0):
  ```
  npm · package-lock.json
  Top 3 heaviest direct dependencies by exclusive size:
    1. webpack@5.89.0         ~42.1 MB (freed: 18 packages)
    2. typescript@5.3.3       ~31.4 MB (freed: 1 package)
    3. vite@5.0.0             ~12.0 MB (freed: 30 packages, 3 not measured)
  ```
  The count in the title is the number of entries shown (at most N). With no
  measurable direct dependency: `no direct dependencies found with size data`.

### 13.5 JSON output
Size keys are **absent** when the value is unmeasured; they are never `null`.
Sizes depend on what is installed on the machine, so a consumer must treat a
missing key as "not measured", not as zero.
- `"size_bytes": <int>` appears on these package objects only: why `package`,
  remove `package`, `freed[]`, `kept[]` and `kept[].via`. Path steps, `leads`,
  `required_by`, dupes and `--across` objects never carry sizes.
- Remove results gain `"freed_size_bytes": <int>` and `"freed_unmeasured": <int>`
  (the number of freed packages counted as 0). Both are present together on a direct
  dependency's result when at least one freed package was measured, or when
  nothing is freed (a known 0).
- Heaviest mode outputs `mode: "heaviest"`; each result has `heaviest`, an array
  of `{ "name", "version", "freed_size_bytes", "freed_count", "freed_unmeasured" }`,
  all always present, sorted by `freed_size_bytes` descending, then name and version.

### 13.6 Package layout
- New package `internal/size` provides directory measurement and caching, and
  owns the containment and identity checks above.
- `internal/cli` makes one size provider per graph (memoized, sharing one
  scanner per run) and passes it to graph (freed totals) and render (annotations).
- `graph.Node.InstallPath` carries the `package-lock.json` key; other parsers leave it empty.

### 13.7 Definition of done (v0.3)
- `internal/size` tests cover symlinks, missing directories, npm nested paths, and python site-packages.
- Golden tests for `--remove` with size and `--heaviest`.
- Documentation updated in README, ARCHITECTURE, and ROADMAP.

## 14. v0.4: More formats

v0.4 expands format support across JavaScript, Python, Go, and Rust ecosystems.

### 14.1 Goals and non-goals
- Add parsers for four additional lockfile and manifest formats:
  1. `yarn.lock`: Yarn Berry (v2+, YAML with `__metadata`) and Yarn Classic (v1, line-oriented format).
  2. `bun.lock`: Bun v1.2+ JSONC lockfile format.
  3. `poetry.lock`: Poetry TOML lockfile format.
  4. `go.mod`: Go module dependencies (`go.mod` / `go.sum`).
  5. Cargo precision: `--cargo-metadata` to query `cargo metadata --format-version 1` when available.
- Non-goals:
  - No downloading external packages or remote registry lookups during lockfile parsing.
  - `go.sum` without `go.mod` has no directed dependency tree and is not loaded alone.

### 14.2 Format parsing specifications

#### 14.2.1 Yarn `yarn.lock`
- **Berry (v2+)**: YAML format containing top-level `__metadata`.
  - Keys are comma-separated descriptor lists (e.g. `"pkg@npm:^1.0.0, pkg@npm:^1.0.1"`). A descriptor splits into name and range at the first `@` after index 0.
  - Values contain `version`, `resolution`, `dependencies`, `dependenciesMeta` (`optional: true` makes an Optional edge) and `peerDependencies`.
  - Node names come from `resolution`, so an `npm:` alias is the package it names, and a `patch:` entry (user patches and Yarn's builtin compat patches) shares the node of its `npm:` base.
  - Roots: every entry whose resolution uses `workspace:`. The workspace's `package.json` (at the `workspace:` path) supplies its version and tells devDependencies apart, since the lockfile merges them into `dependencies`.
  - A dependency resolves to the entry holding the descriptor `name@range`; failing that (e.g. a `resolutions` override), to the entries registered under `name`, the first in sorted key order, with a warning when there are several.
  - A peer dependency resolves to what the package's parents resolved that name to.
- **Classic (v1)**: Line-oriented custom format starting with `# yarn lockfile v1`.
  - Blocks begin with quoted descriptor lines followed by indented properties.
  - Parse `version` string and the `dependencies` and `optionalDependencies` mappings.
  - Root: synthetic root created from project manifest or direct entries, plus one root per workspace member matched by the manifest's `workspaces` globs (list or `{"packages": [...]}`). Members have no lockfile entry, so a dependency on a member resolves by name.

#### 14.2.2 Bun `bun.lock`
- Bun 1.2+ text lockfile: JSONC, so `//` and `/* */` comments and trailing commas are removed (outside strings) before decoding.
- Top-level `workspaces` map and `packages` dictionary.
- Root: `""` (the project) and each workspace member directory. A member's own `packages` entry is `["name@workspace:<dir>"]` and maps to its root.
- Each `packages` value is an array. Element 0 is `name@version`, split at the first `@` after index 0 (scoped names, npm aliases give the real name). The first object after it holds `dependencies`, `optionalDependencies` and `peerDependencies` (index 2 for registry packages, 1 for tarball, `file:` and git ones).
- `packages` keys are install paths (`parent/child` for a nested copy). A dependency resolves to the nearest key: `from/name`, then dropping one package from the path at a time, then the top-level `name`.
- Edge kinds follow `package-lock.json`: only roots have Dev edges; unresolvable Prod/Dev dependencies warn, Optional/Peer ones are skipped.

#### 14.2.3 Poetry `poetry.lock`
- TOML format with `[[package]]` tables.
- Fields: `name`, `version`, `optional`, `groups` (Poetry >= 2.0), `category` (Poetry < 1.5), and `dependencies` table (a dependency table with `optional = true` is an Optional edge).
- Direct dependencies come from `pyproject.toml`. Only the `main` group is production:
  `[tool.poetry.dependencies]`, `[tool.poetry.group.main.dependencies]` and `[project].dependencies` -> Prod;
  `[tool.poetry.group.<name>.dependencies]` for any other group and legacy `[tool.poetry.dev-dependencies]` -> Dev;
  `[project.optional-dependencies]` and Poetry dependencies with `optional = true` (extras) -> Optional.
- Without `pyproject.toml` (or with no declared dependencies), packages that nothing depends on become direct.
  Their kind comes from `groups` (contains `main` -> Prod, else Dev), else `category` (`dev` -> Dev, else Prod);
  a production package with `optional = true` is Optional. Poetry 1.5-1.8 lockfiles record neither field, so all are Prod.
- Root: package defined by `pyproject.toml` in same directory, or directory name fallback.

#### 14.2.4 Go `go.mod`
- Parses `module <path>`, `require (...)`, and `replace (...)`.
- Requirements lacking `// indirect` comment are marked Prod edges from the root module.
- Requirements with `// indirect` are marked indirect edges.
- `replace` never renames a dependency: the node keeps the required module path,
  because the replacement only says where the code comes from. A
  version-specific replace (`old v1.0.0 => ...`) applies to that version only
  and wins over a path-wide one (`old => ...`). A replacement by the same module
  path at another version (a version pin, `x v1.0.0 => x v1.0.1`) shows the
  replacement version, since that is the code that is built. Forks
  (`old => other v1.2.3`) and local directories (`old => ./dir`) keep the
  required version, so `--online` never looks up a version of the wrong module.
- When `--go-graph` is passed, `go mod graph` runs in the go.mod's directory to
  add transitive edges, with a 60-second timeout and the environment
  `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOWORK=off`: no toolchain
  or module is downloaded, go.mod and go.sum are never rewritten, and a parent
  `go.work` is ignored. Its output is the whole requirement graph, so depwhy
  keeps only the build list that minimal version selection picks:
  - `go@…` and `toolchain@…` entries are not modules and are dropped.
  - For each module path only the highest version (by semver) is kept. Edges
    from other versions are dropped, and edges to them point at the selected
    version, so `--dupes` never reports two versions of one Go module.
  - Only modules reachable from the main module through kept edges become
    nodes. With module graph pruning (go 1.17+) every module that provides a
    package is a requirement of the main module, so nothing built is lost.
- If `go` is missing, fails (for example a module not in the local module
  cache, or a `go` line newer than the local toolchain) or times out, depwhy
  warns and uses the go.mod-only graph.

#### 14.2.5 Cargo Precision
- When `--cargo-metadata` is set, depwhy runs
  `cargo metadata --locked --offline --format-version 1 --manifest-path <dir>/Cargo.toml`
  in the lockfile's directory `<dir>`, with a 60-second timeout. `--locked`
  makes cargo fail rather than rewrite an out-of-date Cargo.lock, and
  `--offline` keeps it off the network.
- The environment sets `RUSTUP_TOOLCHAIN` to the user's `rustup default`
  toolchain, `RUSTC=rustc`, and empty `RUSTC_WRAPPER` and
  `RUSTC_WORKSPACE_WRAPPER` (each unless the user already set it), so a
  project's `rust-toolchain.toml` or `.cargo/config.toml` cannot pick the
  binaries cargo runs. cargo still reads the rest of the project's
  configuration, so the flag is for trusted projects only.
- Accurately discriminates Dev, Build, and Prod dependencies and discovers precise workspace members.
- If `cargo` is missing, fails (for example Cargo.lock is out of date with
  Cargo.toml, or a registry dependency is not in the local cache) or times
  out, depwhy warns and parses Cargo.lock as without the flag.

### 14.3 Ecosystem detection order
Updated lockfile detection priority per directory:
1. npm: `pnpm-lock.yaml` > `yarn.lock` > `bun.lock` > `package-lock.json`
2. python: `uv.lock` > `poetry.lock`
3. cargo: `Cargo.lock`
4. go: `go.mod`

### 14.4 Definition of done (v0.4)
- Dedicated parsers in `internal/lockfile` with edge-case tests for each new format.
- Golden tests for yarn, bun, poetry, and go.mod fixtures.
- Detection and CLI help documentation updated.

## 15. Recursive and Monorepo (v0.5)

### 15.1 Recursive discovery

`-r` / `--recursive` is a boolean, default false. It searches `--dir` (default
`.`) and its descendants. It works with why, `--remove`, `--dupes`, `--heaviest`
and their existing modifiers. Combining it with any explicit `-l/--lockfile`
is a usage error: `--recursive cannot be combined with --lockfile`.

`lockfile.DetectRecursive(dir, ecosystem string) ([]Found, []string, error)`
returns paths relative to `dir`, using forward slashes. The CLI joins these
paths to `dir` before loading, so parsers still find adjacent manifests and
optional tool integrations run in the correct project. Installed npm/Python
sizes are measured relative to each discovered lockfile's directory;
an explicit `--venv` keeps its existing meaning.

Walk rules:

- Visit the starting directory first, then child directories in lexical,
  depth-first order. In each directory use §14.3's ecosystem priority:
  `pnpm-lock.yaml` > `yarn.lock` > `bun.lock` > `package-lock.json`;
  `uv.lock` > `poetry.lock`; `Cargo.lock`; `go.mod`.
- Select at most one file per ecosystem **per directory**. `--ecosystem`
  filters every directory. Ignored lockfiles are excluded before priority is
  applied. Priority warnings in descendants are prefixed with the directory's
  relative path. A directory need not contain a lockfile to be traversed.
- Always prune `.git`, `node_modules`, `.venv`, `venv`, `target`, `dist`,
  `build`, `vendor`, `__pycache__`, `.tox`, and `testdata` (test fixtures, which
  the Go tool also skips) below the starting directory.
  Ignore negations cannot reopen these directories.
- Read `.gitignore` in each visited directory. Rules are relative to the file's
  directory, inherited by descendants, and applied in order (last match wins).
  Support comments and blank lines, `!` negation, escaped leading `#`/`!`,
  escaped spaces, directory-only trailing `/`, anchored paths, shell globs
  (`*`, `?`, character classes) and `**` path segments. Patterns with no slash
  match a basename at any depth; a leading or middle slash anchors a pattern
  to its `.gitignore` directory. Nested files can override ancestor rules.
  As in Git, a pruned directory cannot be reopened by negating a file inside it.
- Only `.gitignore` files at or below `--dir` apply: no parent-repository,
  global Git excludes, `.git/info/exclude` or Git index inspection. Ignore rules
  also apply to tracked files, since discovery reads the filesystem only.
- Do not follow directory symlinks. Explicitly selected roots and lockfile
  symlinks retain normal filesystem behavior. Traversal and `.gitignore` read
  errors are fatal (exit 2); missing `.gitignore` files are normal.
- If no supported file survives filtering, exit 2 with
  `no supported lockfile in <dir> or its subdirectories`.

Every graph remains independent. Text results are grouped in discovery order
and their headings identify the relative lockfile path, including removal
messages for non-direct packages. JSON result `lockfile` fields contain that
same path. Parser warnings and not-found messages also identify relative paths.
Without `-r` or `--across`, existing detection and output stay unchanged.

### 15.2 Cross-lockfile duplicates

`--across` is a boolean, default false, valid only with `--dupes`. Otherwise
exit 2 with `--across needs --dupes`. It accepts recursive discovery, normal
directory detection or repeated explicit `-l` flags. It does not imply `-r`.
Explicit paths retain their existing resolution relative to the working
directory; displayed source paths are relative to `--dir` (including `../`
for files outside it). Repeated spellings of the same cleaned absolute path
count once.

Aggregate **all non-root nodes**, not just each graph's local duplicates.
Group by ecosystem and exact lockfile package name, matching §6.3. Keep packages
with more than one distinct version across the loaded set, including conflicts
already present within a single lockfile. Equal names from different ecosystems
are unrelated; equal versions in different lockfiles are sources of one version.
With `--prod`, filter dev-only nodes and dev leads exactly as for local dupes.
Orphans remain eligible and have no roots or direct dependencies.

Sort packages by distinct version count descending, then name, then ecosystem.
Sort versions by the version order of §6.5; sources by lockfile path. Each source
uses its own graph's existing lead traversal, preserving workspace roots and edge
kinds. Count only lockfiles containing that package. Text format:

```text
shared  2 versions across 2 lockfiles
  1.0.0  in web/package-lock.json via tool (prod)
  2.0.0  in admin/package-lock.json via tool (prod)
```

Multiple leads are comma-separated; a source with none prints
`via (no direct dependency)`. No matches prints
`no duplicate packages across lockfiles`. Exit 0 even with no duplicates.
`--online`/`--refresh` annotate only the versions shown, using the existing
lookup and warning behavior; text annotations appear beside each source version.

### 15.3 Across JSON schema

Keep `schema: 1`, `mode: "dupes"`, `query: null`, and the existing `warnings`
array. Add top-level `"across": true` **only** for across mode. `results` holds
one object per duplicated package:

```json
{
  "schema": 1,
  "mode": "dupes",
  "across": true,
  "query": null,
  "results": [
    {
      "ecosystem": "npm",
      "name": "shared",
      "versions": [
        {
          "version": "1.0.0",
          "sources": [
            {
              "lockfile": "web/package-lock.json",
              "version": "1.0.0",
              "roots": ["web"],
              "direct_deps": [
                {"root": "web", "name": "tool", "version": "1.0.0", "kind": "prod"}
              ]
            }
          ]
        },
        {
          "version": "2.0.0",
          "sources": [
            {
              "lockfile": "admin/package-lock.json",
              "version": "2.0.0",
              "roots": ["admin"],
              "direct_deps": [
                {"root": "admin", "name": "tool", "version": "1.0.0", "kind": "prod"}
              ]
            }
          ]
        }
      ]
    }
  ],
  "warnings": []
}
```

`roots` is the sorted unique list of workspace/project labels from that source's
leads. `direct_deps` uses the existing lead schema and ordering, including the
root for each direct dependency. Empty collections encode as `[]`, never `null`.
With `--online`, each source also carries the existing `advisories`, `deprecated`
and `yanked` fields. A clean empty run has `"results": []`.

### 15.4 Definition of done

- Recursive walker, ignore semantics and all four analysis modes have tests.
- Cross-file-only conflicts, ecosystems, workspaces, prod and empty cases tested.
- Mixed monorepo fixture and deterministic text/JSON golden tests.
- Documentation and CLI help cover the new flags.
- Formatting, vet, lint and race tests pass; total statement coverage ≥ 90%.

## 16. Shell completion (v1.0)

`depwhy completion bash|zsh|fish` prints a completion script to stdout and exits 0.
Any other shell name is a usage error (exit 2). The command is recognized only as
exactly two positional words, `completion <shell>`, which is otherwise a usage
error, so a lone `depwhy completion` is still a query for a package named
`completion`.

The scripts complete:
- flags, generated from the flag set, so a new flag needs no script change;
- values: directories for `-C` and `--venv`, files for `-l`, `npm python cargo go`
  for `-e`, nothing for `--depth`;
- package names everywhere else.

Package names come from the hidden flag `--complete-packages`. The script passes it
every word typed before the cursor. It loads the lockfiles those words select
(`-C`, `-l`, `-e`, `-r`), ignores the mode flags and positional arguments, and
prints each non-root package name once, sorted, one per line. It never makes
network requests and never runs `go` or `cargo` (`--online`, `--go-graph` and
`--cargo-metadata` are ignored; so are `--help` and `--version`). Errors exit 2
like any run; the scripts discard stderr, so a directory without a lockfile simply
completes nothing.

bash hands the completion function the words as typed and splits `--flag=value`
at `=`. The bash script therefore strips one level of quoting (`'…'`, `"…"`,
backslash) from each word and joins `--flag`, `=`, `value` again before calling
`--complete-packages`, by scanning characters, never with `eval`.

Package names are untrusted input. The scripts must never evaluate them: bash
reads them line by line (not through `compgen -W`, which expands its word list)
and quotes each with `printf %q`; zsh adds them with `compadd`; fish takes them
from a command substitution. A test runs each shell against a lockfile whose
package name is `$(touch PWNED)`.

The bash script works with bash 3.2 (the macOS default) and newer.
