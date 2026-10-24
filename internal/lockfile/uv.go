package lockfile

import (
	"fmt"
	"reflect"

	"github.com/BurntSushi/toml"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// uvLock is the subset of uv.lock that depwhy reads.
type uvLock struct {
	Version  int `toml:"version"`
	Manifest struct {
		Members []string `toml:"members"`
	} `toml:"manifest"`
	Package []uvPackage `toml:"package"`
}

// uvPackage is one [[package]] table. Source holds exactly one key such as
// registry, git or virtual; dev-dependencies and optional-dependencies map a
// group or extra name to its dependencies.
type uvPackage struct {
	Name     string             `toml:"name"`
	Version  string             `toml:"version"`
	Source   map[string]string  `toml:"source"`
	Deps     []uvDep            `toml:"dependencies"`
	Optional map[string][]uvDep `toml:"optional-dependencies"`
	Dev      map[string][]uvDep `toml:"dev-dependencies"`
}

// uvDep is a dependency reference. Version and Source are only present when
// the lockfile holds several packages with the same name.
type uvDep struct {
	Name    string            `toml:"name"`
	Version string            `toml:"version"`
	Source  map[string]string `toml:"source"`
}

// ParseUV parses uv.lock (SPEC §8.3).
//
// Roots are the workspace members listed under [manifest], or else the packages
// whose source is the project itself ("." as virtual or editable). A dependency
// is matched by name, narrowed by version and then source; if several packages
// still match, the first is used and a warning is added. Environment markers
// are ignored, so every edge counts whatever the platform.
func ParseUV(path string, data []byte) (*graph.Graph, []string, error) {
	var lock uvLock
	if _, err := toml.Decode(string(data), &lock); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var warns []string
	if lock.Version != 1 {
		warns = append(warns, fmt.Sprintf("unsupported uv.lock version %d; results may be wrong", lock.Version))
	}

	members := map[string]bool{}
	for _, m := range lock.Manifest.Members {
		members[m] = true
	}
	isRoot := func(p uvPackage) bool {
		if len(members) > 0 {
			return members[p.Name]
		}
		return p.Source["virtual"] == "." || p.Source["editable"] == "."
	}

	b := graph.NewBuilder("python", path)
	byName := map[string][]int{}
	nodes := make([]int, len(lock.Package))
	for i, p := range lock.Package {
		byName[p.Name] = append(byName[p.Name], i)
		if isRoot(p) {
			nodes[i] = b.Root(p.Name, p.Version)
		} else {
			nodes[i] = b.Node(p.Name, p.Version)
		}
	}

	resolve := func(from uvPackage, d uvDep) (int, bool) {
		cands := byName[d.Name]
		if d.Version != "" {
			cands = filterPkgs(cands, func(i int) bool { return lock.Package[i].Version == d.Version })
		}
		if len(d.Source) > 0 {
			cands = filterPkgs(cands, func(i int) bool { return reflect.DeepEqual(lock.Package[i].Source, d.Source) })
		}
		switch len(cands) {
		case 0:
			warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", d.Name, from.Name))
			return 0, false
		case 1:
		default:
			warns = append(warns, fmt.Sprintf("ambiguous dependency %s of %s; picked %s", d.Name, from.Name, lock.Package[cands[0]].Version))
		}
		return nodes[cands[0]], true
	}

	for i, p := range lock.Package {
		add := func(deps []uvDep, k graph.Kind) {
			for _, d := range deps {
				if to, ok := resolve(p, d); ok {
					b.Edge(nodes[i], to, k)
				}
			}
		}
		add(p.Deps, graph.Prod)
		for _, extra := range sortedKeys(p.Optional) {
			add(p.Optional[extra], graph.Optional)
		}
		for _, group := range sortedKeys(p.Dev) {
			add(p.Dev[group], graph.Dev)
		}
	}
	return finish(b, warns)
}

// filterPkgs returns the entries of idx for which keep is true, in order.
func filterPkgs(idx []int, keep func(int) bool) []int {
	var out []int
	for _, i := range idx {
		if keep(i) {
			out = append(out, i)
		}
	}
	return out
}
