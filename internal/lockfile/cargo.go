package lockfile

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

type cargoLock struct {
	Version int `toml:"version"`
	Package []struct {
		Name         string   `toml:"name"`
		Version      string   `toml:"version"`
		Source       string   `toml:"source"`
		Dependencies []string `toml:"dependencies"`
	} `toml:"package"`
}

// ParseCargo parses Cargo.lock (version absent, 3, or 4).
func ParseCargo(path string, data []byte) (*graph.Graph, []string, error) {
	var lock cargoLock
	if _, err := toml.Decode(string(data), &lock); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var warns []string
	if v := lock.Version; v != 0 && v != 3 && v != 4 {
		warns = append(warns, fmt.Sprintf("unsupported Cargo.lock version %d; results may be wrong", v))
	}

	b := graph.NewBuilder("cargo", path)
	byName := map[string][]int{}
	nodes := make([]int, len(lock.Package))
	for i, p := range lock.Package {
		byName[p.Name] = append(byName[p.Name], i)
		if p.Source == "" {
			nodes[i] = b.Root(p.Name, p.Version)
		} else {
			nodes[i] = b.Node(p.Name, p.Version)
		}
	}

	for i, p := range lock.Package {
		for _, s := range p.Dependencies {
			parts := strings.SplitN(s, " ", 3)
			cands := byName[parts[0]]
			if len(parts) > 1 {
				cands = filterPkgs(cands, func(j int) bool { return lock.Package[j].Version == parts[1] })
			}
			if len(parts) > 2 {
				src := strings.Trim(parts[2], "()")
				cands = filterPkgs(cands, func(j int) bool { return lock.Package[j].Source == src })
			}
			switch len(cands) {
			case 0:
				warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", s, p.Name))
				continue
			case 1:
			default:
				warns = append(warns, fmt.Sprintf("ambiguous dependency %s of %s; picked %s", parts[0], p.Name, lock.Package[cands[0]].Version))
			}
			b.Edge(nodes[i], nodes[cands[0]], graph.Prod)
		}
	}
	return finish(b, warns)
}
