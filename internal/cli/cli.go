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
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/dpologdvinity/depwhy/internal/graph"
	"github.com/dpologdvinity/depwhy/internal/lockfile"
	"github.com/dpologdvinity/depwhy/internal/render"
	"github.com/dpologdvinity/depwhy/internal/size"
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
  depwhy [flags] --heaviest [N]           rank direct dependencies by exclusive size
  depwhy completion bash|zsh|fish         print a shell completion script
  depwhy --version | --help

flags:
`

// notes is the help text printed after the flag list. It documents the known
// limits of the supported lockfile formats (SPEC §8.4).
const notes = `
notes:
  Cargo.lock records no dev or build edges, so every cargo edge is shown as prod
  (--cargo-metadata asks cargo for them). Cargo path dependencies outside a
  workspace also lack a source and appear as roots.
  go.mod lists the build's requirements; --go-graph adds the edges between them.
  Yarn and Bun dev dependencies come from package.json next to the lockfile.
  Platform markers are ignored; all edges count.
  --go-graph and --cargo-metadata run go and cargo offline; use them only on
  trusted projects. --online is the only flag that uses the network.
  JSON output: docs/json-schema.md (schema 1).
`

// options holds the parsed command line. Field names mirror the flags in
// SPEC §3.1; query is the positional package argument (empty for --dupes).
type options struct {
	remove, dupes, prod, all, json, noColor bool
	recursive, across                       bool     // v0.5: monorepo discovery and duplicates
	online, refresh                         bool     // v0.2: network lookups (SPEC §12)
	heaviest                                int      // v0.3: heaviest mode limit (SPEC §13)
	venv                                    string   // v0.3: custom python virtualenv path
	goGraph                                 bool     // v0.4: refine go mod graph (SPEC §14)
	cargoMetadata                           bool     // v0.4: precise cargo metadata (SPEC §14)
	completePackages                        bool     // hidden: list package names for shell completion
	dir                                     string   // directory searched for lockfiles
	lockfiles                               []string // explicit lockfile paths; disables detection
	ecosystem                               string   // "", "npm", "python", "cargo", or "go"
	depth                                   int      // max why-tree levels; 0 means unlimited
	query                                   string   // "name" or "name@version"
}

// newFlagSet defines depwhy's flags, storing their values in o, version and help.
func newFlagSet(o *options, version, help *bool) *pflag.FlagSet {
	fs := pflag.NewFlagSet("depwhy", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.remove, "remove", false, "removal-impact mode; the argument is a direct dependency")
	fs.BoolVar(&o.dupes, "dupes", false, "list packages installed at more than one version")
	fs.BoolVarP(&o.recursive, "recursive", "r", false, "find lockfiles recursively, respecting .gitignore")
	fs.BoolVar(&o.across, "across", false, "with --dupes, find conflicting versions across lockfiles")
	fs.StringVarP(&o.dir, "dir", "C", ".", "directory to look for lockfiles in")
	fs.StringArrayVarP(&o.lockfiles, "lockfile", "l", nil, "use this lockfile instead of auto-detection (repeatable)")
	fs.StringVarP(&o.ecosystem, "ecosystem", "e", "", "restrict to npm, python, cargo, or go")
	fs.BoolVar(&o.prod, "prod", false, "ignore dev dependencies")
	fs.IntVarP(&o.depth, "depth", "d", 0, "max levels printed in the why tree (0 = unlimited; why mode only)")
	fs.BoolVar(&o.all, "all", false, "print all paths without the 10-path cap (why mode only)")
	fs.BoolVar(&o.json, "json", false, "emit JSON")
	fs.BoolVar(&o.noColor, "no-color", false, "disable ANSI color")
	fs.BoolVar(&o.online, "online", false, "look up security advisories and deprecated/yanked status (uses the network)")
	fs.BoolVar(&o.refresh, "refresh", false, "with --online, ignore cached answers")
	fs.IntVar(&o.heaviest, "heaviest", 0, "rank the N (default 10) direct dependencies freeing the most disk space")
	fs.Lookup("heaviest").NoOptDefVal = "10"
	fs.StringVar(&o.venv, "venv", "", "path to Python virtualenv (default .venv next to the lockfile)")
	fs.BoolVar(&o.goGraph, "go-graph", false, "refine Go module transitive dependencies using go mod graph")
	fs.BoolVar(&o.cargoMetadata, "cargo-metadata", false, "use cargo metadata for precise Cargo dependency discrimination")
	fs.BoolVarP(version, "version", "V", false, "print version and exit")
	fs.BoolVarP(help, "help", "h", false, "print usage and exit")
	fs.BoolVar(&o.completePackages, completeFlag, false, "list package names for shell completion")
	_ = fs.MarkHidden(completeFlag) // cannot fail: the flag was just defined
	return fs
}

// Run executes depwhy and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	var o options
	var version, help bool
	fs := newFlagSet(&o, &version, &help)

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
	if o.completePackages {
		// Called by the completion scripts with whatever the user typed so far,
		// so the mode flags and arguments are ignored. Only the lockfile selection
		// counts, and nothing slow or networked runs on a Tab press.
		o.online, o.goGraph, o.cargoMetadata = false, false, false
		return execute(&o, stdout, stderr)
	}
	if isCompletion(fs.Args()) {
		if msg := writeCompletion(stdout, fs, fs.Arg(1)); msg != "" {
			return usageErr(stderr, msg)
		}
		return 0
	}
	if msg := validate(&o, fs); msg != "" {
		return usageErr(stderr, msg)
	}
	return execute(&o, stdout, stderr)
}

// validate checks flag combinations and the positional arguments (SPEC §3). It
// returns a usage message, or "" when the invocation is valid, and stores the
// package argument in o.query.
func validate(o *options, fs *pflag.FlagSet) string {
	pos := fs.Args()
	if fs.Changed("heaviest") {
		// `--heaviest [N]`: pflag only attaches an optional value written as
		// --heaviest=N, so N given as the next word arrives as the positional
		// argument. Heaviest mode takes no package, so a lone number there is N.
		if len(pos) == 1 && o.heaviest == 10 {
			n, err := strconv.Atoi(pos[0])
			if err == nil {
				o.heaviest, pos = n, nil
			}
		}
		if o.heaviest <= 0 {
			return "--heaviest needs a positive number of dependencies"
		}
		if o.online {
			return "--online is not supported with --heaviest"
		}
	}
	if (fs.Changed("depth") || o.all) && (o.remove || o.dupes || o.heaviest > 0) {
		return "--depth and --all apply only to why mode"
	}
	if o.across && !o.dupes {
		return "--across needs --dupes"
	}
	if o.recursive && len(o.lockfiles) > 0 {
		return "--recursive cannot be combined with --lockfile"
	}
	modeCount := 0
	if o.remove {
		modeCount++
	}
	if o.dupes {
		modeCount++
	}
	if o.heaviest > 0 {
		modeCount++
	}
	if modeCount > 1 {
		return "--remove, --dupes, and --heaviest cannot be combined"
	}
	switch {
	case o.ecosystem != "" && o.ecosystem != "npm" && o.ecosystem != "python" && o.ecosystem != "cargo" && o.ecosystem != "go":
		return "unknown ecosystem " + o.ecosystem + " (want npm, python, cargo, or go)"
	case o.refresh && !o.online:
		return "--refresh needs --online"
	case o.depth < 0:
		return "--depth must be >= 0"
	case o.dupes && len(pos) != 0:
		return "--dupes takes no package argument"
	case o.heaviest > 0 && len(pos) != 0:
		return "--heaviest takes no package argument"
	case !o.dupes && o.heaviest == 0 && len(pos) != 1:
		return "expected exactly one package argument"
	}
	if !o.dupes && o.heaviest == 0 {
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

// session is the state of one run: the parsed options, the loaded graphs, and
// the warnings collected so far.
type session struct {
	o        *options
	graphs   []*graph.Graph
	style    render.Style
	stdout   io.Writer
	stderr   io.Writer
	warnings []string

	scanner *size.Scanner                            // shared by every size provider
	sizers  map[*graph.Graph]func(int) (int64, bool) // size provider per graph, see sizeOf
}

// warn prints a warning to stderr and keeps it for the JSON document. Warnings
// never change the exit code. Warnings often quote the lockfile, so the
// terminal copy is escaped; JSON keeps the exact text.
func (s *session) warn(msg string) {
	s.warnings = append(s.warnings, msg)
	fmt.Fprintf(s.stderr, "depwhy: warning: %s\n", render.Escape(msg))
}

// execute runs a validated invocation: it finds and loads the lockfiles, then
// hands the graphs to the selected mode. Unreadable or unparsable lockfiles
// are fatal (exit 2); parser warnings are reported but never change the exit
// code. Lockfile order is the output order (SPEC §4).
func execute(o *options, stdout, stderr io.Writer) int {
	s := &session{o: o, stdout: stdout, stderr: stderr}
	fail := func(format string, a ...any) int {
		// Parse errors may quote the lockfile, so escape them for the terminal.
		fmt.Fprintf(stderr, "depwhy: %s\n", render.Escape(fmt.Sprintf(format, a...)))
		return 2
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
		if o.recursive {
			found, ws, err = lockfile.DetectRecursive(o.dir, o.ecosystem)
		} else {
			found, ws, err = lockfile.Detect(o.dir, o.ecosystem)
		}
		if err != nil {
			return fail("%v", err)
		}
		for _, w := range ws {
			s.warn(w)
		}
	}
	for i := range found {
		found[i].GoGraph = o.goGraph
		found[i].CargoMetadata = o.cargoMetadata
	}
	for _, f := range found {
		label := filepath.Base(f.Path)
		if o.recursive {
			label = f.Path
			f.Path = filepath.Join(o.dir, filepath.FromSlash(f.Path))
		} else if o.across {
			base, err := filepath.Abs(o.dir)
			if err != nil {
				return fail("%v", err)
			}
			p, err := filepath.Abs(f.Path)
			if err != nil {
				return fail("%v", err)
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return fail("%v", err)
			}
			f.Path, label = p, filepath.ToSlash(rel)
		}
		g, ws, err := lockfile.Load(f)
		if err != nil {
			return fail("%v", err)
		}
		if o.recursive || o.across {
			g.LockfileLabel = label
		}
		for _, w := range ws {
			s.warn(label + ": " + w)
		}
		s.graphs = append(s.graphs, g)
	}

	if o.completePackages {
		return s.packageNames()
	}
	if f, ok := stdout.(*os.File); ok {
		s.style.Color = render.Enabled(f, o.noColor)
	}
	if o.dupes {
		if o.across {
			return s.across()
		}
		return s.dupes()
	}
	if o.heaviest > 0 {
		return s.heaviest()
	}
	return s.query()
}

// packageNames prints every non-root package name in the loaded lockfiles once,
// sorted, for the shell completion scripts.
func (s *session) packageNames() int {
	seen := map[string]bool{}
	var names []string
	for _, g := range s.graphs {
		for _, n := range g.Nodes {
			if !n.Root && !seen[n.Name] {
				seen[n.Name] = true
				names = append(names, n.Name)
			}
		}
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintln(s.stdout, n)
	}
	return 0
}

// dupes runs --dupes mode: one block of text (or one JSON result) per lockfile.
// It always exits 0, even when no duplicates exist.
func (s *session) dupes() int {
	results := make([][]graph.Dupe, len(s.graphs))
	shown := map[*graph.Graph][]int{}
	for i, g := range s.graphs {
		results[i] = g.Dupes(s.o.prod)
		for _, d := range results[i] {
			for _, v := range d.Versions {
				shown[g] = append(shown[g], v.Node)
			}
		}
	}
	notes := s.lookup(shown)

	doc := render.NewDoc("dupes", nil)
	for i, g := range s.graphs {
		if s.o.json {
			doc.Add(render.DupesJSON(g, results[i], notes[g]))
			continue
		}
		if i > 0 {
			fmt.Fprintln(s.stdout)
		}
		st := s.style
		st.Notes = notes[g]
		render.DupesText(s.stdout, g, results[i], st)
	}
	return s.finishJSON(doc)
}

// sizeOf returns the size provider for g: the on-disk size of a node, and
// false when it is a root or not measurable (SPEC §13.3). Sizes are measured
// in the lockfile's own directory (or --venv for Python). A provider is made
// once per graph and remembers every answer, and all providers share one
// scanner, so nothing is measured twice in a run.
func (s *session) sizeOf(g *graph.Graph) func(int) (int64, bool) {
	if f, ok := s.sizers[g]; ok {
		return f
	}
	if s.sizers == nil {
		s.sizers = map[*graph.Graph]func(int) (int64, bool){}
		s.scanner = size.NewScanner()
	}
	dir := filepath.Dir(g.Lockfile)
	var measure func(graph.Node) (int64, error)
	switch g.Ecosystem {
	case "npm":
		measure = func(n graph.Node) (int64, error) {
			return s.scanner.NPMSize(dir, n.InstallPath, n.Name, n.Version)
		}
	case "python":
		if site, venv, err := size.FindPythonSitePackages(dir, s.o.venv); err == nil {
			measure = func(n graph.Node) (int64, error) {
				return s.scanner.PythonSize(site, venv, n.Name, n.Version)
			}
		}
	}
	type answer struct {
		size int64
		ok   bool
	}
	memo := map[int]answer{}
	f := func(n int) (int64, bool) {
		if measure == nil || g.Nodes[n].Root {
			return 0, false
		}
		a, done := memo[n]
		if !done {
			sz, err := measure(g.Nodes[n])
			a = answer{sz, err == nil}
			memo[n] = a
		}
		return a.size, a.ok
	}
	s.sizers[g] = f
	return f
}

// heaviest runs --heaviest mode: ranks direct dependencies by exclusive transitive freed size.
func (s *session) heaviest() int {
	doc := render.NewDoc("heaviest", nil)
	for i, g := range s.graphs {
		sizeOf := s.sizeOf(g)
		items := g.Heaviest(graph.HeaviestOpts{
			Prod:  s.o.prod,
			Limit: s.o.heaviest,
			Size:  sizeOf,
		})
		if s.o.json {
			doc.Add(render.HeaviestJSON(g, items))
			continue
		}
		if i > 0 {
			fmt.Fprintln(s.stdout)
		}
		render.HeaviestText(s.stdout, g, items, s.style)
	}
	return s.finishJSON(doc)
}

// query runs the why and --remove modes. It collects every node matching the
// query across all graphs (ecosystem order, then version ascending) and
// prints one result per match. With no match it prints suggestions and
// returns exit code 1.
func (s *session) query() int {
	o := s.o
	name, version := graph.SplitQuery(o.query)
	mode := "why"
	if o.remove {
		mode = "remove"
	}
	doc := render.NewDoc(mode, &render.Query{Name: name, Version: version})

	var matches []match
	devOnly := false
	for _, g := range s.graphs {
		for _, n := range g.Find(name, version) {
			if o.prod && g.DevOnly()[n] {
				devOnly = true
				continue
			}
			matches = append(matches, match{g, n})
		}
	}
	if len(matches) == 0 {
		return s.notFound(doc, name, devOnly)
	}

	// Compute every result first: the packages they show decide what to look up.
	shown := map[*graph.Graph][]int{}
	whys := make([]graph.WhyResult, len(matches))
	removes := make([]graph.RemoveResult, len(matches))
	for i, m := range matches {
		shown[m.g] = append(shown[m.g], m.n)
		if o.remove {
			rem := m.g.Remove(m.n, o.prod)
			if !rem.NotDirect { // nothing is analyzed for a non-direct target
				rem.ComputeFreedSize(s.sizeOf(m.g))
			}
			removes[i] = rem
			shown[m.g] = append(shown[m.g], removes[i].Freed...)
			for _, k := range removes[i].Kept {
				shown[m.g] = append(shown[m.g], k.Node)
			}
			continue
		}
		whys[i] = m.g.Why(m.n, graph.WhyOpts{Depth: o.depth, All: o.all, Prod: o.prod})
		shown[m.g] = append(shown[m.g], treeNodes(m.g, whys[i].Tree)...)
	}
	notes := s.lookup(shown)

	for i, m := range matches {
		st := s.style
		st.Notes = notes[m.g]
		sizeOf := s.sizeOf(m.g)
		sizes := map[int]int64{}
		measured := shown[m.g]
		for _, k := range removes[i].Kept {
			measured = append(measured, k.Via) // JSON kept[].via carries a size too
		}
		for _, n := range measured {
			if sz, ok := sizeOf(n); ok {
				sizes[n] = sz
			}
		}
		if len(sizes) > 0 {
			st.Sizes = sizes
		}
		switch {
		case o.json && o.remove:
			doc.Add(render.RemoveJSONWithSizes(m.g, removes[i], notes[m.g], sizes))
		case o.json:
			doc.Add(render.WhyJSONWithSizes(m.g, whys[i], notes[m.g], sizes))
		default:
			if i > 0 {
				fmt.Fprintln(s.stdout)
			}
			if o.remove {
				render.RemoveText(s.stdout, m.g, removes[i], st)
			} else {
				render.WhyText(s.stdout, m.g, whys[i], st)
			}
		}
	}
	return s.finishJSON(doc)
}

// notFound reports that the query matched nothing: an error line and
// suggestions on stderr, a JSON document on stdout in --json mode, exit code 1.
func (s *session) notFound(doc *render.Doc, name string, devOnly bool) int {
	var files []string
	for _, g := range s.graphs {
		file := g.LockfileLabel
		if file == "" {
			file = filepath.Base(g.Lockfile)
		}
		files = append(files, file)
	}
	msg := fmt.Sprintf("%s not found in %s", s.o.query, strings.Join(files, ", "))
	// When the name exists the miss came from the version or --prod: say which
	// versions are there instead of suggesting other names.
	var installed []string
	seen := map[string]bool{}
	for _, g := range s.graphs {
		for _, n := range g.Find(name, "") {
			if id := g.Nodes[n].Name + "@" + g.Nodes[n].Version; !seen[id] {
				seen[id] = true
				installed = append(installed, id)
			}
		}
	}
	switch {
	case devOnly:
		msg += " (found only via dev dependencies; drop --prod)"
	case len(installed) > 0:
		msg += " (installed: " + strings.Join(installed, ", ") + ")"
	}
	// The query, labels and names may hold control characters: escape the lines.
	fmt.Fprintf(s.stderr, "depwhy: %s\n", render.Escape(msg))
	sugg := []string{}
	if len(installed) == 0 {
		sugg = graph.Suggest(s.graphs, name)
	}
	if len(sugg) > 0 {
		fmt.Fprintf(s.stderr, "did you mean: %s\n", render.Escape(strings.Join(sugg, ", ")))
	}
	if s.o.json {
		doc.SetSuggestions(sugg)
		if code := s.finishJSON(doc); code != 0 {
			return code
		}
	}
	return 1
}

// treeNodes lists the package nodes printed in a why tree, root leaves and the
// "…" placeholder excluded.
func treeNodes(g *graph.Graph, t *graph.TreeNode) []int {
	var out []int
	var walk func(*graph.TreeNode)
	walk = func(n *graph.TreeNode) {
		if n.Node >= 0 && !g.Nodes[n.Node].Root {
			out = append(out, n.Node)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(t)
	return out
}

// finishJSON writes the accumulated JSON document when --json is set. Text
// mode prints as it goes, so there is nothing left to do.
func (s *session) finishJSON(doc *render.Doc) int {
	if s.o.json {
		doc.Warnings = append(doc.Warnings, s.warnings...)
		if err := doc.Encode(s.stdout); err != nil {
			fmt.Fprintf(s.stderr, "depwhy: %v\n", err)
			return 2
		}
	}
	return 0
}
