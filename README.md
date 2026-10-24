# depwhy

Why is this package in my dependency tree? `depwhy` reads your lockfile and tells you, offline, with one interface for npm, pnpm, Python (uv), and Cargo.

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
```

## Install

```sh
go install github.com/dpologdvinity/depwhy@latest
```

Or download a binary from the GitHub releases page (linux, macOS, and Windows; amd64 and arm64). Homebrew is planned.

## Usage

```
depwhy [flags] <package>[@version]      explain why <package> is installed
depwhy [flags] --remove <package>       impact of removing a direct dependency
depwhy [flags] --dupes                  packages installed at more than one version
```

| Flag | Meaning |
|---|---|
| `--remove` | Removal-impact mode. The argument is a direct dependency. |
| `--dupes` | List packages installed at more than one version. |
| `-C`, `--dir` | Directory to look for lockfiles in (default `.`). |
| `-l`, `--lockfile` | Use these lockfile paths instead of auto-detection (repeatable). |
| `-e`, `--ecosystem` | Restrict to `npm`, `python`, or `cargo`. |
| `--prod` | Ignore dev dependencies. |
| `-d`, `--depth` | Max levels printed in the why tree (0 = unlimited). |
| `--all` | Print all paths without the 10-path cap. |
| `--json` | Emit JSON. |
| `--no-color` | Disable ANSI color. |
| `-V`, `--version` | Print the version. |

Exit codes: `0` found (or `--dupes` ran), `1` package not found, `2` usage error or unreadable lockfile.

If a directory holds lockfiles for several ecosystems, all of them are loaded and searched.

## Supported lockfiles

| File | Versions |
|---|---|
| `package-lock.json` | lockfileVersion 2 and 3 |
| `pnpm-lock.yaml` | lockfileVersion 9.x |
| `uv.lock` | version 1 |
| `Cargo.lock` | versions 3 and 4 |

## Notes and limitations

- `Cargo.lock` records no dev or build edges, so every cargo edge is shown as `prod`. Path dependencies outside a workspace also lack a source and appear as roots.
- The why tree shows at most 10 root paths; use `--all` to see the rest.
- Platform markers (`marker` in `uv.lock`) are ignored, so all edges count.
- Only the given directory is scanned, and nothing is read from manifests except `package.json` names for pnpm labels.

## JSON

`--json` prints a stable document with `schema`, `mode`, `query`, `results`, and `warnings` keys. See `docs/SPEC.md` §7.3 for the shape.

## License

MIT
