// Command depwhy explains why a package is in a project's dependency tree.
//
// It reads lockfiles (npm, pnpm, Yarn, Bun, uv, Poetry, Cargo and Go) and
// answers, offline: why is a package installed, what would removing a direct
// dependency free, which packages exist at several versions, and which direct
// dependencies are heaviest. See the README for usage and ARCHITECTURE.md for
// the design.
package main

import (
	"os"

	"github.com/dpologdvinity/depwhy/internal/cli"
)

// main runs the CLI and exits with its status code.
func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
