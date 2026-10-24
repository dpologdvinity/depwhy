package render

import (
	"encoding/json"
	"io"
	"path/filepath"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// Query echoes the user's query; a missing version encodes as null.
type Query struct {
	Name    string
	Version string
}

func (q Query) MarshalJSON() ([]byte, error) {
	var v *string
	if q.Version != "" {
		v = &q.Version
	}
	return json.Marshal(struct {
		Name    string  `json:"name"`
		Version *string `json:"version"`
	}{q.Name, v})
}

// Doc is the top-level JSON document (SPEC §7.3).
type Doc struct {
	Schema      int       `json:"schema"`
	Mode        string    `json:"mode"`
	Query       *Query    `json:"query"`
	Results     []any     `json:"results"`
	Suggestions *[]string `json:"suggestions,omitempty"`
	Warnings    []string  `json:"warnings"`
}

func NewDoc(mode string, q *Query) *Doc {
	return &Doc{Schema: 1, Mode: mode, Query: q, Results: []any{}, Warnings: []string{}}
}

func (d *Doc) Add(result any) { d.Results = append(d.Results, result) }

// SetSuggestions marks the document as a not-found result.
func (d *Doc) SetSuggestions(s []string) {
	if s == nil {
		s = []string{}
	}
	d.Suggestions = &s
}

func (d *Doc) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

type pkgJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type leadJSON struct {
	Root    string `json:"root"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

type stepJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Root    bool   `json:"root,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

type whyJSON struct {
	Ecosystem      string       `json:"ecosystem"`
	Lockfile       string       `json:"lockfile"`
	Package        pkgJSON      `json:"package"`
	DevOnly        bool         `json:"dev_only"`
	Direct         bool         `json:"direct"`
	Paths          [][]stepJSON `json:"paths"`
	PathsTruncated bool         `json:"paths_truncated"`
	Leads          []leadJSON   `json:"leads"`
}

type keptJSON struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Via     pkgJSON `json:"via"`
}

type removeJSON struct {
	Ecosystem   string     `json:"ecosystem"`
	Lockfile    string     `json:"lockfile"`
	Package     pkgJSON    `json:"package"`
	Direct      bool       `json:"direct"`
	StillNeeded bool       `json:"still_needed"`
	Freed       []pkgJSON  `json:"freed"`
	Kept        []keptJSON `json:"kept"`
	RequiredBy  []leadJSON `json:"required_by"`
}

type dupeVersionJSON struct {
	Version string     `json:"version"`
	Leads   []leadJSON `json:"leads"`
}

type dupeJSON struct {
	Name     string            `json:"name"`
	Versions []dupeVersionJSON `json:"versions"`
}

type dupesJSON struct {
	Ecosystem string     `json:"ecosystem"`
	Lockfile  string     `json:"lockfile"`
	Dupes     []dupeJSON `json:"dupes"`
}

func pkg(g *graph.Graph, n int) pkgJSON {
	return pkgJSON{g.Nodes[n].Name, g.Nodes[n].Version}
}

func leadsJSON(g *graph.Graph, ls []graph.Lead) []leadJSON {
	out := make([]leadJSON, 0, len(ls))
	for _, l := range ls {
		out = append(out, leadJSON{g.Nodes[l.Root].Name, g.Nodes[l.Direct].Name, g.Nodes[l.Direct].Version, l.Kind.String()})
	}
	return out
}

// WhyJSON builds one why result entry.
func WhyJSON(g *graph.Graph, r graph.WhyResult) any {
	paths := make([][]stepJSON, 0, len(r.Paths))
	for _, p := range r.Paths {
		steps := make([]stepJSON, 0, len(p))
		for i, st := range p {
			n := g.Nodes[st.Node]
			s := stepJSON{Name: n.Name, Version: n.Version}
			if i == 0 {
				s.Root = true
			} else {
				s.Kind = st.Kind.String()
			}
			steps = append(steps, s)
		}
		paths = append(paths, steps)
	}
	return whyJSON{g.Ecosystem, filepath.Base(g.Lockfile), pkg(g, r.Target), r.DevOnly, r.Direct, paths, r.Truncated, leadsJSON(g, r.Leads)}
}

// RemoveJSON builds one remove result entry.
func RemoveJSON(g *graph.Graph, r graph.RemoveResult) any {
	out := removeJSON{
		Ecosystem: g.Ecosystem, Lockfile: filepath.Base(g.Lockfile), Package: pkg(g, r.Target),
		Direct: !r.NotDirect, StillNeeded: r.StillNeeded,
		Freed: []pkgJSON{}, Kept: []keptJSON{}, RequiredBy: leadsJSON(g, r.RequiredBy),
	}
	for _, n := range r.Freed {
		out.Freed = append(out.Freed, pkg(g, n))
	}
	for _, k := range r.Kept {
		p := pkg(g, k.Node)
		out.Kept = append(out.Kept, keptJSON{p.Name, p.Version, pkg(g, k.Via)})
	}
	return out
}

// DupesJSON builds one dupes result entry.
func DupesJSON(g *graph.Graph, ds []graph.Dupe) any {
	out := dupesJSON{Ecosystem: g.Ecosystem, Lockfile: filepath.Base(g.Lockfile), Dupes: []dupeJSON{}}
	for _, d := range ds {
		dj := dupeJSON{Name: d.Name, Versions: []dupeVersionJSON{}}
		for _, v := range d.Versions {
			dj.Versions = append(dj.Versions, dupeVersionJSON{g.Nodes[v.Node].Version, leadsJSON(g, v.Leads)})
		}
		out.Dupes = append(out.Dupes, dj)
	}
	return out
}
