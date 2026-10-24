// Command depwhy explains why a package is in a project's dependency tree.
//
// It reads lockfiles (package-lock.json, pnpm-lock.yaml, uv.lock, Cargo.lock)
// and answers three questions offline: why is a package installed, what would
// removing a direct dependency free, and which packages exist at several
// versions. See the README for usage and ARCHITECTURE.md for the design.
package main

import (
	"os"

	"github.com/dpologdvinity/depwhy/internal/cli"
)

// main runs the CLI and exits with its status code.
func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
