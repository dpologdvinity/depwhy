// Package cli parses flags and runs depwhy.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/pflag"
)

// Version is overridden at release time via -ldflags.
var Version = "dev"

const usage = `usage:
  depwhy [flags] <package>[@version]      explain why <package> is installed
  depwhy [flags] --remove <package>       impact of removing a direct dependency
  depwhy [flags] --dupes                  packages installed at more than one version
  depwhy --version | --help

flags:
`

const notes = `
notes:
  Cargo.lock records no dev or build edges, so every cargo edge is shown as prod.
  Cargo path dependencies outside a workspace also lack a source and appear as roots.
  Platform markers are ignored; all edges count.
`

type options struct {
	remove, dupes, prod, all, json, noColor bool
	dir                                     string
	lockfiles                               []string
	ecosystem                               string
	depth                                   int
	query                                   string
}

// Run executes depwhy and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	var o options
	var version, help bool
	fs := pflag.NewFlagSet("depwhy", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.remove, "remove", false, "removal-impact mode; the argument is a direct dependency")
	fs.BoolVar(&o.dupes, "dupes", false, "list packages installed at more than one version")
	fs.StringVarP(&o.dir, "dir", "C", ".", "directory to look for lockfiles in")
	fs.StringArrayVarP(&o.lockfiles, "lockfile", "l", nil, "use this lockfile instead of auto-detection (repeatable)")
	fs.StringVarP(&o.ecosystem, "ecosystem", "e", "", "restrict to npm, python, or cargo")
	fs.BoolVar(&o.prod, "prod", false, "ignore dev dependencies")
	fs.IntVarP(&o.depth, "depth", "d", 0, "max levels printed in the why tree (0 = unlimited)")
	fs.BoolVar(&o.all, "all", false, "print all paths without the 10-path cap")
	fs.BoolVar(&o.json, "json", false, "emit JSON")
	fs.BoolVar(&o.noColor, "no-color", false, "disable ANSI color")
	fs.BoolVarP(&version, "version", "V", false, "print version and exit")
	fs.BoolVarP(&help, "help", "h", false, "print usage and exit")

	if err := fs.Parse(args); err != nil {
		return usageErr(stderr, err.Error())
	}
	if help {
		fmt.Fprint(stdout, usage+fs.FlagUsages()+notes)
		return 0
	}
	if version {
		fmt.Fprintf(stdout, "depwhy %s\n", Version)
		return 0
	}
	if msg := validate(&o, fs.Args()); msg != "" {
		return usageErr(stderr, msg)
	}
	return 0
}

func validate(o *options, pos []string) string {
	switch {
	case o.remove && o.dupes:
		return "--remove and --dupes cannot be combined"
	case o.ecosystem != "" && o.ecosystem != "npm" && o.ecosystem != "python" && o.ecosystem != "cargo":
		return "unknown ecosystem " + o.ecosystem + " (want npm, python, or cargo)"
	case o.depth < 0:
		return "--depth must be >= 0"
	case o.dupes && len(pos) != 0:
		return "--dupes takes no package argument"
	case !o.dupes && len(pos) != 1:
		return "expected exactly one package argument"
	}
	if !o.dupes {
		o.query = pos[0]
	}
	return ""
}

func usageErr(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "depwhy: %s\nrun `depwhy --help` for usage\n", msg)
	return 2
}
