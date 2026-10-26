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
	Category     string                 `toml:"category"`
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
		Name                 string                 `toml:"name"`
		Version              string                 `toml:"version"`
		Dependencies         []string               `toml:"dependencies"`
		OptionalDependencies map[string][]string    `toml:"optional-dependencies"`
	} `toml:"project"`
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
		// Collect from tool.poetry.dependencies
		for name := range pyproject.Tool.Poetry.Dependencies {
			if canonPy(name) == "python" {
				continue
			}
			rootDeps = append(rootDeps, rootDep{name, graph.Prod})
		}
		// tool.poetry.dev-dependencies
		for name := range pyproject.Tool.Poetry.DevDependencies {
			rootDeps = append(rootDeps, rootDep{name, graph.Dev})
		}
		// tool.poetry.group.*
		for grpName, grp := range pyproject.Tool.Poetry.Group {
			kind := graph.Dev
			if grpName != "dev" && grpName != "test" {
				kind = graph.Prod
			}
			for name := range grp.Dependencies {
				rootDeps = append(rootDeps, rootDep{name, kind})
			}
		}
		// project.dependencies
		for _, rawDep := range pyproject.Project.Dependencies {
			name := parsePyprojectDep(rawDep)
			if name != "" && canonPy(name) != "python" {
				rootDeps = append(rootDeps, rootDep{name, graph.Prod})
			}
		}
		// project.optional-dependencies
		for _, list := range pyproject.Project.OptionalDependencies {
			for _, rawDep := range list {
				name := parsePyprojectDep(rawDep)
				if name != "" && canonPy(name) != "python" {
					rootDeps = append(rootDeps, rootDep{name, graph.Dev})
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
			if seen[key] {
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
				kind := graph.Prod
				if strings.EqualFold(p.Category, "dev") {
					kind = graph.Dev
				}
				b.Edge(rootIdx, pkgNodes[i], kind)
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
			rawVal := p.Dependencies[depName]
			kind := graph.Prod
			if m, ok := rawVal.(map[string]interface{}); ok {
				if opt, ok := m["optional"].(bool); ok && opt {
					kind = graph.Optional
				}
			}
			if to, ok := resolve(p.Name, depName); ok {
				b.Edge(from, to, kind)
			} else {
				warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", depName, p.Name))
			}
		}
	}

	g, err := b.Build()
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return g, warns, nil
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
