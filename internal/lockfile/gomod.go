package lockfile

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// goModGraph runs `go mod graph` in dir and returns its output. It is a
// variable so tests can substitute canned output.
//
// The environment keeps the run offline and read-only: GOTOOLCHAIN=local stops a
// go or toolchain line from downloading and running another toolchain,
// GOPROXY=off stops module downloads, -mod=readonly stops go.mod/go.sum
// rewrites, and GOWORK=off keeps a go.work in a parent directory from
// widening the graph beyond this go.mod.
var goModGraph = func(dir string) ([]byte, error) {
	return runTool(dir, []string{"GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=-mod=readonly", "GOWORK=off"}, "go", "mod", "graph")
}

// goEdge is one requirement; a From with an empty Version is the main module.
type goEdge struct{ From, To module.Version }

// ParseGoMod parses a go.mod file (SPEC §14.2.4).
//
// Without goGraph the graph is the main module plus one edge per require
// directive, Indirect for `// indirect` ones. With goGraph it runs
// `go mod graph` in the go.mod's directory and keeps the requirements of the
// versions minimal version selection picks; if that fails it warns and falls
// back to the require directives.
func ParseGoMod(path string, data []byte, goGraph bool) (*graph.Graph, []string, error) {
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	rootName := "app"
	if f.Module != nil && f.Module.Mod.Path != "" {
		rootName = f.Module.Mod.Path
	} else {
		base := filepath.Base(filepath.Dir(path))
		if base != "." && base != "/" && base != "" {
			rootName = base
		}
	}

	kinds := map[string]graph.Kind{} // direct requirement kind by module path
	var edges []goEdge
	for _, req := range f.Require {
		if req.Indirect {
			kinds[req.Mod.Path] = graph.Indirect
		}
		edges = append(edges, goEdge{To: req.Mod})
	}

	var warns []string
	if goGraph {
		out, err := goModGraph(filepath.Dir(path))
		if err != nil {
			warns = append(warns, fmt.Sprintf("go mod graph failed: %v; falling back to direct go.mod requirements", err))
		} else {
			edges = selectedGoEdges(out)
		}
	}

	b := graph.NewBuilder("go", path)
	root := b.Root(rootName, "")
	nodes := map[module.Version]int{{}: root}
	node := func(m module.Version) int {
		if i, ok := nodes[m]; ok {
			return i
		}
		i := b.Node(m.Path, replacedVersion(f.Replace, m))
		nodes[m] = i
		return i
	}
	// Walk outward from the main module so that modules required only by
	// dropped (unselected) versions never become unreachable nodes.
	reqs := map[module.Version][]module.Version{}
	for _, e := range edges {
		reqs[e.From] = append(reqs[e.From], e.To)
	}
	for queue := []module.Version{{}}; len(queue) > 0; queue = queue[1:] {
		from := queue[0]
		for _, to := range reqs[from] {
			k := graph.Prod
			if from.Version == "" {
				k = kinds[to.Path]
			}
			if _, seen := nodes[to]; !seen {
				queue = append(queue, to)
			}
			b.Edge(nodes[from], node(to), k)
		}
	}

	g, err := b.Build()
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return g, warns, nil
}

// selectedGoEdges turns `go mod graph` output into the requirement edges of the
// selected build list. go mod graph prints the whole requirement graph:
// go@ and toolchain@ pseudo-modules, and every version any module requires,
// not just the one minimal version selection picks (the highest per path).
// Edges from unselected versions are dropped and edges to them are pointed at
// the selected version. The main module is printed without a version, so its
// edges keep an empty From.
func selectedGoEdges(out []byte) []goEdge {
	var edges []goEdge
	selected := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		from, to := splitModVer(fields[0]), splitModVer(fields[1])
		if pseudoGoModule(from.Path) || pseudoGoModule(to.Path) {
			continue
		}
		if from.Version == "" {
			from = module.Version{}
		}
		for _, m := range []module.Version{from, to} {
			if m.Version != "" && semver.Compare(m.Version, selected[m.Path]) > 0 {
				selected[m.Path] = m.Version
			}
		}
		edges = append(edges, goEdge{from, to})
	}
	kept := edges[:0]
	for _, e := range edges {
		if e.From.Version != selected[e.From.Path] {
			continue
		}
		e.To.Version = selected[e.To.Path]
		kept = append(kept, e)
	}
	return kept
}

// pseudoGoModule reports whether path is one of the go and toolchain entries
// `go mod graph` prints for go and toolchain directives; they are not modules.
func pseudoGoModule(path string) bool {
	return path == "go" || path == "toolchain"
}

// replacedVersion returns the version to show for module m under go.mod's
// replace directives. A replacement never renames a module: the dependency is
// still m.Path, the replacement is only where its code comes from. A
// version-specific replace (old@v => ...) applies to that version only and
// wins over a path-wide one. Only a replacement by the same path at another
// version (a version pin) changes the version shown, because that is the code
// that is built; forks and local directories keep the required version.
func replacedVersion(reps []*modfile.Replace, m module.Version) string {
	var match *modfile.Replace
	for _, r := range reps {
		if r.Old.Path == m.Path && (r.Old.Version == m.Version || r.Old.Version == "" && match == nil) {
			match = r
		}
	}
	if match != nil && match.New.Path == m.Path && match.New.Version != "" {
		return match.New.Version
	}
	return m.Version
}

// splitModVer splits "path@version" (or a bare "path") as printed by go mod graph.
func splitModVer(s string) module.Version {
	p, v, _ := strings.Cut(s, "@")
	return module.Version{Path: p, Version: v}
}
