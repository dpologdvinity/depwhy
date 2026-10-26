package graph

import (
	"path/filepath"
	"sort"
)

// DupeSource identifies a version in one graph, keeping node indexes local.
type DupeSource struct {
	Graph *Graph
	Node  int
	Leads []Lead
}

// AcrossVersion is a package version and every lockfile containing it.
type AcrossVersion struct {
	Version string
	Sources []DupeSource
}

// AcrossDupe describes conflicting versions within one ecosystem.
type AcrossDupe struct {
	Ecosystem, Name string
	Lockfiles       int
	Versions        []AcrossVersion
}

// DupesAcross finds packages with multiple versions across the supplied graphs,
// including conflicts within a single lockfile. It uses Dupes' exact-name and
// prod semantics, but never combines packages from different ecosystems.
func DupesAcross(graphs []*Graph, prod bool) []AcrossDupe {
	type key struct{ ecosystem, name string }
	groups := map[key]map[string][]DupeSource{}
	seen := map[string]bool{}
	for _, g := range graphs {
		file := filepath.Clean(g.Lockfile)
		if seen[file] {
			continue
		}
		seen[file] = true
		var dev []bool
		if prod {
			dev = g.DevOnly()
		}
		for i, n := range g.Nodes {
			if n.Root || (prod && dev[i]) {
				continue
			}
			k := key{g.Ecosystem, n.Name}
			if groups[k] == nil {
				groups[k] = map[string][]DupeSource{}
			}
			groups[k][n.Version] = append(groups[k][n.Version], DupeSource{Graph: g, Node: i})
		}
	}
	var out []AcrossDupe
	for k, versions := range groups {
		if len(versions) < 2 {
			continue
		}
		d := AcrossDupe{Ecosystem: k.ecosystem, Name: k.name}
		files := map[string]bool{}
		for version, sources := range versions {
			for i := range sources {
				s := &sources[i]
				s.Leads = s.Graph.leads(s.Node, prod)
				files[filepath.Clean(s.Graph.Lockfile)] = true
			}
			sort.Slice(sources, func(i, j int) bool { return sourcePath(sources[i]) < sourcePath(sources[j]) })
			d.Versions = append(d.Versions, AcrossVersion{Version: version, Sources: sources})
		}
		d.Lockfiles = len(files)
		sort.Slice(d.Versions, func(i, j int) bool { return d.Versions[i].Version < d.Versions[j].Version })
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Versions) != len(out[j].Versions) {
			return len(out[i].Versions) > len(out[j].Versions)
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Ecosystem < out[j].Ecosystem
	})
	return out
}

// sourcePath uses the same relative path the renderer shows, when available.
func sourcePath(s DupeSource) string {
	if s.Graph.LockfileLabel != "" {
		return s.Graph.LockfileLabel
	}
	return s.Graph.Lockfile
}
