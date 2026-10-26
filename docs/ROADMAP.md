# depwhy — Roadmap

Each release is a separate spec, plan, and implementation cycle. Before starting a
release, extend `docs/SPEC.md` with its behavior, keeping the v0.1 sections
intact, and write its tasks into `docs/PLAN.md`. Scope stays fixed per release, and
anything new goes to a later one.

## v0.1 — Core (offline, 4 formats)
Spec: `docs/SPEC.md`. Plan: `docs/PLAN.md`.
- why / `--remove` / `--dupes`, plus `--json`
- `package-lock.json` v2/v3, `pnpm-lock.yaml` v9, `uv.lock`, `Cargo.lock`
- GoReleaser binaries, and `go install`

Exit: golden tests pass and a manual check has been done on one real project per ecosystem.

## v0.2 — Health info (opt-in network) — done
Spec: `docs/SPEC.md` §12. Plan: `docs/PLAN.md` (Tasks 17–22). Differences from the sketch below: advisories are looked up only for packages shown in the output (not the whole graph), and answers are cached per package.

Adds a `--online` flag. Without it, behavior is identical to v0.1.
- **Advisories** through OSV: `POST https://api.osv.dev/v1/querybatch` with `{"queries":[{"package":{"name":N,"ecosystem":E},"version":V}]}`, where E is `npm`, `PyPI`, or `crates.io`. Batch every node into one request, chunked at 1,000. Show `⚠ GHSA-…` next to affected nodes in trees, and add `advisories: []` to the JSON.
- **Deprecated/yanked**: npm `GET https://registry.npmjs.org/<name>/<version>` (field `deprecated`), PyPI `GET https://pypi.org/pypi/<name>/<version>/json` (`info.yanked`), and crates.io `GET https://crates.io/api/v1/crates/<name>/<version>` (`version.yanked`; it requires a `User-Agent` header). Fetch only the nodes being displayed, not the whole graph.
- Cache responses in `$XDG_CACHE_HOME/depwhy` (or `os.UserCacheDir()`) with a 24 h TTL, and add `--refresh` to bypass it.
- Network failures produce warnings, never errors, and the exit code stays the same.
- Timeouts: 5 s per request, 8 concurrent requests.

## v0.3 — Size and "what does it cost me" — done
Spec: `docs/SPEC.md` §13. Plan: `docs/PLAN.md` (Tasks 23–27).
- On-disk size when installed: npm uses `node_modules/<path>` from the lockfile key, Python uses the venv `site-packages` (`.venv` in the dir, or `--venv`), and cargo is skipped.
- `--remove` reports `would free ~X MB` when sizes are known.
- `depwhy --heaviest [N]`: direct dependencies ranked by exclusive transitive size (the freed set from §6.2).

## v0.4 — More formats
Spec: `docs/SPEC.md` §14. Plan: `docs/PLAN.md` (Tasks 28–33).
In priority order, each with fixtures and goldens:
1. `yarn.lock` Berry (YAML, `__metadata`) and Classic v1 (custom format, which needs a small hand parser).
2. `bun.lock` (the text JSONC format, bun ≥ 1.2).
3. `poetry.lock` (TOML, with `[[package]]` and `dependencies` tables).
4. Go: `go.mod` + `go mod graph` (needs the go toolchain, so warn if it's missing). `go.sum` has no graph.
5. Cargo accuracy: when `cargo` is on PATH and `--cargo-metadata` is set, use `cargo metadata --format-version 1` to get real dev and build kinds and the actual workspace members.

## v0.5 — Recursive and monorepo
- `-r/--recursive`: find lockfiles in subdirectories, respecting `.gitignore` and skipping `node_modules`, `.venv`, and `target`.
- Group output by lockfile path.
- `depwhy --dupes --across`: the same package at different versions across lockfiles in one repo.

## v1.0 — Stability
- Freeze the JSON `schema: 1` and document it in `docs/json-schema.md`. Any break bumps `schema`.
- Distribution: a Scoop bucket (still open). Done early, in v0.2.x: the Homebrew cask in `dpologdvinity/homebrew-tap`, `.deb`/`.rpm`/`.apk` packages with a signed apt repository, and the npm package `@kaitlynbassford/depwhy` (`npm/`, a launcher that downloads the right binary).
- Shell completions (`depwhy completion bash|zsh|fish`), with package-name completion from the lockfile.

## Launch checklist (alongside v0.1–v0.2)
- Demo GIF recorded with `vhs` (charmbracelet/vhs) from a committed `demo.tape`, shown at the top of the README.
- Comparison table in the README: `npm ls`, `pnpm why`, `uv tree --invert`, and `cargo tree -i` against depwhy (cross-ecosystem, removal impact, JSON).
- Post to Show HN, r/golang, r/node, r/Python, and r/rust after v0.2, since advisories make the demo stronger.
- Issue templates: bug (with lockfile snippet) and new format request.

## Ideas parked (not scheduled)
- `--graph dot|mermaid` export.
- `depwhy diff <git-ref>`: which packages entered or left the tree between two lockfile versions.
- License summary per path (needs registry data).
- A GitHub Action that comments `--remove` and `--dupes` deltas on PRs.
