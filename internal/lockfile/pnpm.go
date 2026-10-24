package lockfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// pnpmDep is an importer dependency: the range written in package.json and the
// version pnpm resolved it to.
type pnpmDep struct{ Specifier, Version string }

// pnpmImporter is a project or workspace member and its direct dependencies.
type pnpmImporter struct {
	Dependencies         map[string]pnpmDep `yaml:"dependencies"`
	DevDependencies      map[string]pnpmDep `yaml:"devDependencies"`
	OptionalDependencies map[string]pnpmDep `yaml:"optionalDependencies"`
}

// pnpmSnapshot is one resolved package and what it depends on (name → ref).
type pnpmSnapshot struct {
	Dependencies         map[string]string `yaml:"dependencies"`
	OptionalDependencies map[string]string `yaml:"optionalDependencies"`
}

// pnpmLock is the subset of pnpm-lock.yaml (lockfileVersion 9) depwhy reads.
// Packages is only used to check that a snapshot has matching metadata.
type pnpmLock struct {
	LockfileVersion string                  `yaml:"lockfileVersion"`
	Importers       map[string]pnpmImporter `yaml:"importers"`
	Packages        map[string]any          `yaml:"packages"`
	Snapshots       map[string]pnpmSnapshot `yaml:"snapshots"`
}

// pnpmStrip drops the peer-dependency suffix, which is everything from the first
// "(": react-dom@18.2.0(react@18.2.0) becomes react-dom@18.2.0.
func pnpmStrip(key string) string {
	if i := strings.Index(key, "("); i >= 0 {
		return key[:i]
	}
	return key
}

// pnpmSplitKey splits a snapshot key or ref into name and version at the last
// "@" that is not the first character, so scoped names survive:
// @babel/core@7.24.0(...) gives "@babel/core" and "7.24.0".
func pnpmSplitKey(key string) (name, version string) {
	key = pnpmStrip(key)
	if i := strings.LastIndex(key, "@"); i > 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
}

// sortedKeys returns the keys of m in ascending order. Parsers use it so that
// Go's random map iteration never leaks into the graph.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ParsePnpm parses pnpm-lock.yaml (lockfileVersion 9.x, SPEC §8.2).
//
// Each importer becomes a root, labelled with the name in its package.json
// (read from next to the lockfile) or else its path. Peer-dependency variants
// of a package collapse into one node and their edges are merged. link: refs
// point at other importers, file: refs become nodes named after the dependency,
// and aliases ("string-width-cjs: string-width@4.2.3") resolve to the real
// package.
func ParsePnpm(lockPath string, data []byte) (*graph.Graph, []string, error) {
	var lock pnpmLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", lockPath, err)
	}
	if !strings.HasPrefix(lock.LockfileVersion, "9.") {
		return nil, nil, fmt.Errorf("parse %s: pnpm-lock.yaml lockfileVersion %s is not supported; need 9.x (pnpm >= 9)", lockPath, lock.LockfileVersion)
	}

	b := graph.NewBuilder("npm", lockPath)
	var warns []string
	dir := filepath.Dir(lockPath)

	roots := map[string]int{}
	for _, key := range sortedKeys(lock.Importers) {
		label, version := key, ""
		if raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(key), "package.json")); err == nil {
			var pj struct{ Name, Version string }
			if json.Unmarshal(raw, &pj) == nil {
				if pj.Name != "" {
					label = pj.Name
				}
				version = pj.Version
			}
		}
		roots[key] = b.Root(label, version)
	}

	exists := map[string]bool{}
	for k := range lock.Packages {
		exists[pnpmStrip(k)] = true
	}
	snapKeys := sortedKeys(lock.Snapshots)
	for _, k := range snapKeys {
		exists[pnpmStrip(k)] = true
		name, version := pnpmSplitKey(k)
		b.Node(name, version)
	}

	// resolve maps a dependency ref to a node, or reports false after warning.
	resolve := func(importer, depName, ref string) (int, bool) {
		switch {
		case strings.HasPrefix(ref, "link:"):
			target := path.Clean(path.Join(importer, strings.TrimPrefix(ref, "link:")))
			if r, ok := roots[target]; ok {
				return r, true
			}
			warns = append(warns, fmt.Sprintf("%s: %s links to %s, which is not an importer", importer, depName, ref))
			return 0, false
		case strings.HasPrefix(ref, "file:"):
			return b.Node(depName, ref), true
		}
		key := pnpmStrip(ref)
		if !strings.Contains(key[min(1, len(key)):], "@") {
			key = depName + "@" + key
		}
		if !exists[key] {
			warns = append(warns, fmt.Sprintf("%s not found in snapshots or packages", key))
		}
		name, version := pnpmSplitKey(key)
		return b.Node(name, version), true
	}

	for _, key := range sortedKeys(lock.Importers) {
		imp := lock.Importers[key]
		for _, g := range []struct {
			deps map[string]pnpmDep
			kind graph.Kind
		}{{imp.Dependencies, graph.Prod}, {imp.DevDependencies, graph.Dev}, {imp.OptionalDependencies, graph.Optional}} {
			for _, name := range sortedKeys(g.deps) {
				if to, ok := resolve(key, name, g.deps[name].Version); ok {
					b.Edge(roots[key], to, g.kind)
				}
			}
		}
	}
	for _, k := range snapKeys {
		snap := lock.Snapshots[k]
		name, version := pnpmSplitKey(k)
		from := b.Node(name, version)
		for _, g := range []struct {
			deps map[string]string
			kind graph.Kind
		}{{snap.Dependencies, graph.Prod}, {snap.OptionalDependencies, graph.Optional}} {
			for _, dn := range sortedKeys(g.deps) {
				if to, ok := resolve(".", dn, g.deps[dn]); ok {
					b.Edge(from, to, g.kind)
				}
			}
		}
	}
	return finish(b, warns)
}
