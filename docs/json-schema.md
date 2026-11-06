# depwhy JSON output, schema 1

`depwhy --json` prints exactly one JSON document on stdout. This file is the
contract for that document. From depwhy 1.0 it is a stability promise:

- **Compatible changes** keep `"schema": 1`. They are limited to adding new keys to
  objects, and adding new values to fields documented as open (`mode`,
  `ecosystem`, `kind`). Consumers must ignore keys they do not know.
- **Breaking changes** bump `schema`. These include removing or renaming a key,
  changing a type or a key's meaning, making a present key optional, and changing
  an ordering this document guarantees.

The text output is for people and carries no such promise; scripts should use
`--json`.

`docs/SPEC.md` describes the behavior behind the fields (§7.3, §12.7, §13.5,
§15.3). Where the two disagree about the JSON shape, this file wins.

## Conventions

- Keys are `snake_case`.
- Arrays are never `null`; an empty collection is `[]`.
- The output is pretty-printed with two-space indents and ends with a newline. Key
  order inside an object is stable, but do not rely on it; parse the JSON.
- A key described as *optional* is either present with the stated type or absent.
  It is never `null` unless the type says so.
- Errors with exit code 2 (usage errors, no lockfile, unreadable lockfile) print
  plain text on stderr and **no JSON**. Exit codes 0 and 1 always print a document.
- Warnings are printed on stderr as they happen and also collected in `warnings`.

## Top level

```json
{
  "schema": 1,
  "mode": "why",
  "query": { "name": "c", "version": null },
  "results": [],
  "warnings": []
}
```

| Key | Type | Meaning |
|---|---|---|
| `schema` | integer | Always `1` for this document version. |
| `mode` | string | `"why"`, `"remove"`, `"dupes"` or `"heaviest"`. |
| `across` | `true` (optional) | Present only for `--dupes --across`; see [Across results](#across-results). |
| `query` | object or `null` | `{ "name": string, "version": string or null }` as typed, for `why` and `remove`. `null` for `dupes` and `heaviest`. |
| `results` | array | One object per result, shaped by `mode` (below). |
| `suggestions` | array of strings (optional) | Present only when the query matched nothing (exit 1). Close package names, possibly `[]`. |
| `warnings` | array of strings | Every warning of the run, in order. |

`results` is `[]` when nothing matched (exit 1) and for a `dupes`, `--across` or
`heaviest` run with nothing to report (exit 0).

## Shared objects

### Lockfile identity

Every per-lockfile result carries:

| Key | Type | Meaning |
|---|---|---|
| `ecosystem` | string | `"npm"` (npm, pnpm, Yarn, Bun), `"python"` (uv, Poetry), `"cargo"` or `"go"`. Open: new ecosystems may be added. |
| `lockfile` | string | The lockfile's base name (`"package-lock.json"`). With `-r` or `--across` it is the path relative to `-C`, with `/` separators (`"web/package-lock.json"`). |

### Package

`{ "name": string, "version": string }`, plus optional keys:

| Key | Type | Where | Meaning |
|---|---|---|---|
| `size_bytes` | integer (optional) | why `package`; remove `package`, `freed[]`, `kept[]`, `kept[].via` | Installed size on disk. Absent when not measured (nothing installed, or the location could not be verified). Absent means unknown, not zero. |
| `advisories` | array of strings, or `null` | with `--online`: why `package`; remove `package`, `freed[]`, `kept[]`; dupes `versions[]`; across `sources[]` | Advisory IDs (OSV, GHSA, RUSTSEC, PYSEC). `[]` means checked and clean, and `null` means it could not be checked. |
| `deprecated` | string or `null` | same as `advisories` | The registry's deprecation message, or `null`. |
| `yanked` | boolean | same as `advisories` | The version was yanked (PyPI, crates.io). |

Without `--online`, `advisories`, `deprecated` and `yanked` are absent. With
`--online` they are present together on every object listed above.

### Lead

A direct dependency that leads to the package:
`{ "root": string, "name": string, "version": string, "kind": string }`. `root` is
the project or workspace member that declares the dependency, and `kind` is the edge
kind from that root.

### Edge kind

`kind` is one of `"prod"`, `"dev"`, `"optional"`, `"peer"` or `"indirect"`
(a Go `// indirect` requirement). Open: new kinds may be added.

## Why results (`"mode": "why"`)

One result per matching package version, per lockfile.

| Key | Type | Meaning |
|---|---|---|
| `ecosystem`, `lockfile` | | [Lockfile identity](#lockfile-identity). |
| `package` | [Package](#package) | The matched package. |
| `dev_only` | boolean | Every route to it goes through a dev dependency. |
| `direct` | boolean | It is itself a direct dependency of a root. |
| `paths` | array of arrays of steps | Routes from a root to the package, root first. |
| `paths_truncated` | boolean | The 10-path cap hid routes; pass `--all` to get every route. |
| `leads` | array of [Lead](#lead) | The direct dependencies it is reached through. |

A step is `{ "name": string, "version": string }`. The first step of each path is
the root and adds `"root": true`; its `version` may be `""` when the project
declares none. Every later step adds `"kind"`, the kind of the edge that led to it.
Steps never carry sizes or health fields.

A package that is in the lockfile but reachable from no root (an orphaned entry)
is still a result: `paths` and `leads` are empty and `direct` is false. Text mode
prints `not reachable from any root (an orphaned lockfile entry)` under its
heading, and the run exits 0. The lockfile's warnings also name it.

## Remove results (`"mode": "remove"`)

One result per matching package, per lockfile.

| Key | Type | Meaning |
|---|---|---|
| `ecosystem`, `lockfile` | | [Lockfile identity](#lockfile-identity). |
| `package` | [Package](#package) | The package to remove. |
| `direct` | boolean | It is a direct dependency. When `false`, nothing can be removed directly; see `required_by`. |
| `still_needed` | boolean | Another route keeps it installed after the removal. |
| `freed` | array of [Package](#package) | Packages that would leave the tree, including itself when it leaves. |
| `kept` | array | Packages it uses that stay: [Package](#package) fields plus `"via"`, the [Package](#package) that keeps it. |
| `required_by` | array of [Lead](#lead) | For a non-direct package, the direct dependencies that pull it in. |
| `freed_size_bytes` | integer (optional) | Total installed size of `freed`. |
| `freed_unmeasured` | integer (optional) | How many `freed` packages had no size and counted as 0. Present exactly when `freed_size_bytes` is. |

## Dupes results (`"mode": "dupes"`, without `across`)

One result per lockfile, including lockfiles with no duplicates.

| Key | Type | Meaning |
|---|---|---|
| `ecosystem`, `lockfile` | | [Lockfile identity](#lockfile-identity). |
| `dupes` | array | `{ "name": string, "versions": [...] }` for each package installed at more than one version. |

Each version entry is `{ "version": string, "leads": [Lead] }`, plus the health keys
with `--online`.

## Across results (`"mode": "dupes"`, `"across": true`)

One result per package that has more than one version across all loaded lockfiles
of one ecosystem.

| Key | Type | Meaning |
|---|---|---|
| `ecosystem` | string | See [Lockfile identity](#lockfile-identity). |
| `name` | string | The package name. |
| `versions` | array | `{ "version": string, "sources": [...] }`. |

Each source is `{ "lockfile": string, "version": string, "roots": [string],
"direct_deps": [Lead] }`, plus the health keys with `--online`. `roots` is the
sorted, unique list of roots in that lockfile that reach the version.

## Heaviest results (`"mode": "heaviest"`)

One result per lockfile.

| Key | Type | Meaning |
|---|---|---|
| `ecosystem`, `lockfile` | | [Lockfile identity](#lockfile-identity). |
| `heaviest` | array | Up to N entries `{ "name", "version", "freed_size_bytes", "freed_count", "freed_unmeasured" }`, all always present. |

`freed_size_bytes` is what removing that direct dependency would free,
`freed_count` the number of packages it would remove, and `freed_unmeasured` how many
of those had no size. A direct dependency is left out when none of the packages it
frees was measured, or when it frees nothing (it is still needed elsewhere), so
`heaviest` is `[]` for a project that is not installed.

## Ordering

These orders are part of the contract:

- `results` follow lockfile order: the order lockfiles were found (detection
  priority per directory, directories in walk order with `-r`) or given with `-l`.
  Within a lockfile, why and remove results are ordered by version.
- Dupes packages sort by number of versions (most first), then name. Across
  packages sort the same way, then by ecosystem. Their `versions` ascend in
  depwhy's version order (SPEC §6.5), and across `sources` sort by lockfile path.
- `heaviest` entries sort by `freed_size_bytes` descending, then name, then version.
- `paths`, `leads`, `freed`, `kept` and `required_by` have a deterministic order
  for a given lockfile; it is not otherwise specified.

## Checking the schema

`internal/cli/schema_test.go` decodes every JSON golden file in `testdata/golden`
and compares the set of key paths it finds with `testdata/json-keys.txt`. Adding,
removing or renaming a key fails that test until the list is updated, so every
change to the shape is deliberate and gets reviewed against this document.
