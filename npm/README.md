# depwhy

**Why is this package in my dependency tree?** `depwhy` reads your lockfile
(`package-lock.json`, `pnpm-lock.yaml`, `uv.lock`, `Cargo.lock`) and tells you, offline
by default, which direct dependencies pull a package in, what removing a direct
dependency would free, and which packages are installed at more than one version.

```sh
npm install -g depwhy      # or: npx depwhy <package>
depwhy lodash
depwhy --remove express
depwhy --dupes
```

This npm package is a thin launcher. On install it downloads the matching native
binary for your platform from the
[GitHub release](https://github.com/dpologdvinity/depwhy/releases) of the same
version, checks its SHA-256 against the release's `checksums.txt`, and runs it. If
your npm skips install scripts (`--ignore-scripts`, or npm 12's default), the binary
is downloaded the first time you run `depwhy` instead.

Supported: Linux, macOS and Windows on x64 and arm64, Node.js 18 or newer.

Full documentation, flags, and the JSON format: https://github.com/dpologdvinity/depwhy
