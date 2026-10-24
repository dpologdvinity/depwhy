package graph

import "sort"

type DupeVersion struct {
	Node  int
	Leads []Lead
}

type Dupe struct {
	Name     string
	Versions []DupeVersion
}

// Dupes lists packages present at more than one version (SPEC §6.3).
func (g *Graph) Dupes(prod bool) []Dupe {
	var dev []bool
	if prod {
		dev = g.DevOnly()
	}
	byName := map[string][]int{}
	for i, n := range g.Nodes {
		if !n.Root && !(prod && dev[i]) {
			byName[n.Name] = append(byName[n.Name], i)
		}
	}
	var out []Dupe
	for name, ix := range byName {
		if len(ix) < 2 {
			continue
		}
		sort.Slice(ix, func(a, b int) bool { return g.Nodes[ix[a]].Version < g.Nodes[ix[b]].Version })
		d := Dupe{Name: name}
		for _, i := range ix {
			d.Versions = append(d.Versions, DupeVersion{Node: i, Leads: g.leads(i, prod)})
		}
		out = append(out, d)
	}
	sort.Slice(out, func(a, b int) bool {
		if len(out[a].Versions) != len(out[b].Versions) {
			return len(out[a].Versions) > len(out[b].Versions)
		}
		return out[a].Name < out[b].Name
	})
	return out
}
