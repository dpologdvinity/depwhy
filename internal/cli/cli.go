// Package cli is depwhy's command line front end.
//
// Run parses flags, finds and loads lockfiles, runs the selected mode (why,
// --remove or --dupes) and renders the result as text or JSON. It returns the
// process exit code instead of exiting and takes its output streams as
// arguments, so tests can run it end to end.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/spf13/pflag"

	"github.com/dpologdvinity/depwhy/internal/graph"
	"github.com/dpologdvinity/depwhy/internal/lockfile"
	"github.com/dpologdvinity/depwhy/internal/render"
)

// Version is overridden at release time via -ldflags.
var Version = "dev"

// resolveVersion prefers the ldflags version, then the module version that
// `go install pkg@version` embeds, then "dev".
func resolveVersion(ldflags string, info *debug.BuildInfo) string {
	if ldflags != "dev" || info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ldflags
	}
	return info.Main.Version
}

// usage is the help text printed before the generated flag list.
const usage = `usage:
  depwhy [flags] <package>[@version]      explain why <package> is installed
  depwhy [flags] --remove <package>       impact of removing a direct dependency
  depwhy [flags] --dupes                  packages installed at more than one version
  depwhy --version | --help

flags:
`

// notes is the help text printed after the flag list. It documents the known
// limits of the supported lockfile formats (SPEC §8.4).
const notes = `
notes:
  Cargo.lock records no dev or build edges, so every cargo edge is shown as prod.
  Cargo path dependencies outside a workspace also lack a source and appear as roots.
  Platform markers are ignored; all edges count.
`

// options holds the parsed command line. Field names mirror the flags in
// SPEC §3.1; query is the positional package argument (empty for --dupes).
type options struct {
	remove, dupes, prod, all, json, noColor bool
	dir                                     string   // directory searched for lockfiles
	lockfiles                               []string // explicit lockfile paths; disables detection
	ecosystem                               string   // "", "npm", "python", or "cargo"
	depth                                   int      // max why-tree levels; 0 means unlimited
	query                                   string   // "name" or "name@version"
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
		info, _ := debug.ReadBuildInfo()
		fmt.Fprintf(stdout, "depwhy %s\n", resolveVersion(Version, info))
		return 0
	}
	if msg := validate(&o, fs.Args()); msg != "" {
		return usageErr(stderr, msg)
	}
	return execute(&o, stdout, stderr)
}

// validate checks flag combinations and the positional arguments (SPEC §3). It
// returns a usage message, or "" when the invocation is valid, and stores the
// package argument in o.query.
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

// usageErr prints a usage error to stderr and returns exit code 2.
func usageErr(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "depwhy: %s\nrun `depwhy --help` for usage\n", msg)
	return 2
}

// match is a node found by the query, with the graph that holds it.
type match struct {
	g *graph.Graph
	n int
}

// execute runs a validated invocation: it finds and loads the lockfiles, then
// hands the graphs to the selected mode. Unreadable or unparsable lockfiles
// are fatal (exit 2); parser warnings are reported but never change the exit
// code. Lockfile order is the output order (SPEC §4).
func execute(o *options, stdout, stderr io.Writer) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "depwhy: "+format+"\n", a...)
		return 2
	}
	var warnings []string
	warn := func(msg string) {
		warnings = append(warnings, msg)
		fmt.Fprintf(stderr, "depwhy: warning: %s\n", msg)
	}

	var found []lockfile.Found
	if len(o.lockfiles) > 0 {
		var err error
		if found, err = lockfile.FromPaths(o.lockfiles, o.ecosystem); err != nil {
			return fail("%v", err)
		}
	} else {
		var ws []string
		var err error
		if found, ws, err = lockfile.Detect(o.dir, o.ecosystem); err != nil {
			return fail("%v", err)
		}
		for _, w := range ws {
			warn(w)
		}
	}
	var graphs []*graph.Graph
	for _, f := range found {
		g, ws, err := lockfile.Load(f)
		if err != nil {
			return fail("%v", err)
		}
		for _, w := range ws {
			warn(filepath.Base(f.Path) + ": " + w)
		}
		graphs = append(graphs, g)
	}

	style := render.Style{}
	if f, ok := stdout.(*os.File); ok {
		style.Color = render.Enabled(f, o.noColor)
	}

	if o.dupes {
		return dupes(o, graphs, style, warnings, stdout, stderr)
	}
	return query(o, graphs, style, warnings, stdout, stderr)
}

// dupes runs --dupes mode: one block of text (or one JSON result) per lockfile.
// It always exits 0, even when no duplicates exist.
func dupes(o *options, graphs []*graph.Graph, style render.Style, warnings []string, stdout, stderr io.Writer) int {
	doc := render.NewDoc("dupes", nil)
	doc.Warnings = append(doc.Warnings, warnings...)
	for i, g := range graphs {
		ds := g.Dupes(o.prod)
		if o.json {
			doc.Add(render.DupesJSON(g, ds))
			continue
		}
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		render.DupesText(stdout, g, ds, style)
	}
	return finishJSON(o, doc, stdout, stderr)
}

// query runs the why and --remove modes. It collects every node matching the
// query across all graphs (ecosystem order, then version ascending) and
// prints one result per match. With no match it prints suggestions and
// returns exit code 1.
func query(o *options, graphs []*graph.Graph, style render.Style, warnings []string, stdout, stderr io.Writer) int {
	name, version := graph.SplitQuery(o.query)
	mode := "why"
	if o.remove {
		mode = "remove"
	}
	doc := render.NewDoc(mode, &render.Query{Name: name, Version: version})
	doc.Warnings = append(doc.Warnings, warnings...)

	var matches []match
	devOnly := false
	for _, g := range graphs {
		for _, n := range g.Find(name, version) {
			if o.prod && g.DevOnly()[n] {
				devOnly = true
				continue
			}
			matches = append(matches, match{g, n})
		}
	}
	if len(matches) == 0 {
		var files []string
		for _, g := range graphs {
			files = append(files, filepath.Base(g.Lockfile))
		}
		msg := fmt.Sprintf("%s not found in %s", o.query, strings.Join(files, ", "))
		if devOnly {
			msg += " (found only via dev dependencies; drop --prod)"
		}
		fmt.Fprintf(stderr, "depwhy: %s\n", msg)
		sugg := graph.Suggest(graphs, name)
		if len(sugg) > 0 {
			fmt.Fprintf(stderr, "did you mean: %s\n", strings.Join(sugg, ", "))
		}
		if o.json {
			doc.SetSuggestions(sugg)
			if err := doc.Encode(stdout); err != nil {
				fmt.Fprintf(stderr, "depwhy: %v\n", err)
				return 2
			}
		}
		return 1
	}

	for i, m := range matches {
		if o.remove {
			r := m.g.Remove(m.n, o.prod)
			if o.json {
				doc.Add(render.RemoveJSON(m.g, r))
				continue
			}
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			render.RemoveText(stdout, m.g, r, style)
			continue
		}
		r := m.g.Why(m.n, graph.WhyOpts{Depth: o.depth, All: o.all, Prod: o.prod})
		if o.json {
			doc.Add(render.WhyJSON(m.g, r))
			continue
		}
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		render.WhyText(stdout, m.g, r, style)
	}
	return finishJSON(o, doc, stdout, stderr)
}

// finishJSON writes the accumulated JSON document when --json is set. Text
// mode prints as it goes, so there is nothing left to do.
func finishJSON(o *options, doc *render.Doc, stdout, stderr io.Writer) int {
	if o.json {
		if err := doc.Encode(stdout); err != nil {
			fmt.Fprintf(stderr, "depwhy: %v\n", err)
			return 2
		}
	}
	return 0
}
