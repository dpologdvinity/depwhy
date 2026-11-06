package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/dpologdvinity/depwhy/internal/graph"
)

type poetryPackage struct {
	Name         string                 `toml:"name"`
	Version      string                 `toml:"version"`
	Category     string                 `toml:"category"` // Poetry < 1.5: "main" or "dev"
	Groups       []string               `toml:"groups"`   // Poetry >= 2.0: dependency groups that need it
	Optional     bool                   `toml:"optional"`
	Dependencies map[string]interface{} `toml:"dependencies"`
}

type poetryLock struct {
	Package []poetryPackage `toml:"package"`
}

type pyprojectTOML struct {
	Tool struct {
		Poetry struct {
			Name            string                 `toml:"name"`
			Version         string                 `toml:"version"`
			Dependencies    map[string]interface{} `toml:"dependencies"`
			DevDependencies map[string]interface{} `toml:"dev-dependencies"`
			Group           map[string]struct {
				Dependencies map[string]interface{} `toml:"dependencies"`
			} `toml:"group"`
		} `toml:"poetry"`
	} `toml:"tool"`
	Project struct {
		Name                 string              `toml:"name"`
		Version              string              `toml:"version"`
		Dependencies         []string            `toml:"dependencies"`
		OptionalDependencies map[string][]string `toml:"optional-dependencies"`
	} `toml:"project"`
	// DependencyGroups is PEP 735 (Poetry 2.2+): each entry is a PEP 508
	// requirement string or an {include-group = "name"} table.
	DependencyGroups map[string][]interface{} `toml:"dependency-groups"`
}

// ParsePoetry parses a poetry.lock file (SPEC §14.2.3).
func ParsePoetry(path string, data []byte) (*graph.Graph, []string, error) {
	var lock poetryLock
	if _, err := toml.Decode(string(data), &lock); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	b := graph.NewBuilder("python", path)
	byName := map[string][]int{}
	pkgNodes := make([]int, len(lock.Package))

	for i, p := range lock.Package {
		idx := b.Node(p.Name, p.Version)
		pkgNodes[i] = idx
		c := canonPy(p.Name)
		byName[c] = append(byName[c], idx)
	}

	// Read pyproject.toml if present
	dir := filepath.Dir(path)
	pyprojectPath := filepath.Join(dir, "pyproject.toml")
	var pyproject pyprojectTOML
	hasPyproject := false
	if raw, err := os.ReadFile(pyprojectPath); err == nil {
		if _, err := toml.Decode(string(raw), &pyproject); err == nil {
			hasPyproject = true
		}
	}

	rootName := "app"
	rootVersion := ""
	if hasPyproject {
		if pyproject.Tool.Poetry.Name != "" {
			rootName = pyproject.Tool.Poetry.Name
			rootVersion = pyproject.Tool.Poetry.Version
		} else if pyproject.Project.Name != "" {
			rootName = pyproject.Project.Name
			rootVersion = pyproject.Project.Version
		}
	} else {
		base := filepath.Base(filepath.Clean(dir))
		if base != "." && base != "/" && base != "" {
			rootName = base
		}
	}

	rootIdx := b.Root(rootName, rootVersion)
	var warns []string

	resolve := func(fromLabel, depName string) (int, bool) {
		cands := byName[canonPy(depName)]
		if len(cands) == 0 {
			return 0, false
		}
		if len(cands) > 1 {
			warns = append(warns, fmt.Sprintf("ambiguous dependency %s of %s; picked first", depName, fromLabel))
		}
		return cands[0], true
	}

	type rootDep struct {
		name string
		kind graph.Kind
	}
	var rootDeps []rootDep

	if hasPyproject {
		// Poetry treats only the main group as production; every other group
		// (dev, test, lint, docs, ...) is non-production, and optional = true
		// main dependencies are extras.
		addPoetry := func(deps map[string]interface{}, kind graph.Kind) {
			for name, v := range deps {
				if canonPy(name) == "python" {
					continue
				}
				k := kind
				if k == graph.Prod && poetryOptional(v) {
					k = graph.Optional
				}
				rootDeps = append(rootDeps, rootDep{name, k})
			}
		}
		addPoetry(pyproject.Tool.Poetry.Dependencies, graph.Prod)
		addPoetry(pyproject.Tool.Poetry.DevDependencies, graph.Dev)
		for grpName, grp := range pyproject.Tool.Poetry.Group {
			kind := graph.Dev
			if grpName == "main" {
				kind = graph.Prod
			}
			addPoetry(grp.Dependencies, kind)
		}
		// project.dependencies
		for _, rawDep := range pyproject.Project.Dependencies {
			name := parsePyprojectDep(rawDep)
			if name != "" && canonPy(name) != "python" {
				rootDeps = append(rootDeps, rootDep{name, graph.Prod})
			}
		}
		// project.optional-dependencies are extras
		for _, list := range pyproject.Project.OptionalDependencies {
			for _, rawDep := range list {
				name := parsePyprojectDep(rawDep)
				if name != "" && canonPy(name) != "python" {
					rootDeps = append(rootDeps, rootDep{name, graph.Optional})
				}
			}
		}
		// [dependency-groups] are all non-production. An {include-group}
		// table only adds another group's requirements, which this loop
		// already records as dev, so tables are skipped rather than followed.
		for _, list := range pyproject.DependencyGroups {
			for _, entry := range list {
				if s, ok := entry.(string); ok {
					if name := parsePyprojectDep(s); name != "" {
						rootDeps = append(rootDeps, rootDep{name, graph.Dev})
					}
				}
			}
		}
	}

	// Stable sort rootDeps
	sort.SliceStable(rootDeps, func(i, j int) bool {
		if rootDeps[i].name != rootDeps[j].name {
			return rootDeps[i].name < rootDeps[j].name
		}
		return rootDeps[i].kind < rootDeps[j].kind
	})

	if len(rootDeps) > 0 {
		seen := map[string]bool{}
		for _, rd := range rootDeps {
			key := rd.name + ":" + string(rune(rd.kind))
			// A self-reference such as the extra all = ["proj[x]"] is not a dependency.
			if seen[key] || canonPy(rd.name) == canonPy(rootName) {
				continue
			}
			seen[key] = true
			if to, ok := resolve(rootName, rd.name); ok {
				b.Edge(rootIdx, to, rd.kind)
			} else {
				warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", rd.name, rootName))
			}
		}
	} else {
		// Fallback when no pyproject.toml or no declared dependencies:
		// connect packages with in-degree 0
		depCounts := map[string]int{}
		for _, p := range lock.Package {
			for dep := range p.Dependencies {
				depCounts[canonPy(dep)]++
			}
		}
		for i, p := range lock.Package {
			if depCounts[canonPy(p.Name)] == 0 {
				b.Edge(rootIdx, pkgNodes[i], poetryLockKind(p))
			}
		}
	}

	// Edges between packages
	for i, p := range lock.Package {
		from := pkgNodes[i]

		depNames := make([]string, 0, len(p.Dependencies))
		for d := range p.Dependencies {
			depNames = append(depNames, d)
		}
		sort.Strings(depNames)

		for _, depName := range depNames {
			kind := graph.Prod
			if poetryOptional(p.Dependencies[depName]) {
				kind = graph.Optional
			}
			if to, ok := resolve(p.Name, depName); ok {
				b.Edge(from, to, kind)
			} else {
				warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", depName, p.Name))
			}
		}
	}

	return finish(b, warns)
}

// poetryLockKind classifies a package from the lockfile alone: Poetry >= 2.0
// records groups (main is production, every other group is dev), Poetry < 1.5
// records category, and Poetry 1.5-1.8 records neither (so Prod). A
// production package marked optional is only installed through an extra.
func poetryLockKind(p poetryPackage) graph.Kind {
	dev := strings.EqualFold(p.Category, "dev")
	if len(p.Groups) > 0 {
		dev = true
		for _, g := range p.Groups {
			if g == "main" {
				dev = false
			}
		}
	}
	switch {
	case dev:
		return graph.Dev
	case p.Optional:
		return graph.Optional
	}
	return graph.Prod
}

// poetryOptional reports whether a Poetry dependency spec (a version string, a
// table, or a list of tables for multiple constraints) sets optional = true.
func poetryOptional(v interface{}) bool {
	switch t := v.(type) {
	case map[string]interface{}:
		opt, _ := t["optional"].(bool)
		return opt
	case []map[string]interface{}:
		for _, m := range t {
			if opt, _ := m["optional"].(bool); opt {
				return true
			}
		}
	case []interface{}:
		for _, m := range t {
			if poetryOptional(m) {
				return true
			}
		}
	}
	return false
}

func canonPy(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, ".", "-")
	return s
}

func parsePyprojectDep(s string) string {
	s = strings.TrimSpace(s)
	end := strings.IndexFunc(s, func(r rune) bool {
		return r != '-' && r != '_' && r != '.' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
	if end != -1 {
		return s[:end]
	}
	return s
}
