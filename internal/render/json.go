package render

import (
	"encoding/json"
	"io"
	"path/filepath"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// Query echoes the user's query back in the JSON output.
type Query struct {
	Name    string
	Version string
}

// MarshalJSON encodes an empty Version as null rather than "".
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

// Doc is the top-level JSON document (SPEC §7.3). The schema field is a
// stability promise from 1.0: any breaking change bumps it.
type Doc struct {
	Schema      int       `json:"schema"`
	Mode        string    `json:"mode"`
	Query       *Query    `json:"query"`
	Results     []any     `json:"results"`
	Suggestions *[]string `json:"suggestions,omitempty"`
	Warnings    []string  `json:"warnings"`
}

// NewDoc starts a document for the given mode ("why", "remove" or "dupes").
// q is nil for dupes, which encodes as null.
func NewDoc(mode string, q *Query) *Doc {
	return &Doc{Schema: 1, Mode: mode, Query: q, Results: []any{}, Warnings: []string{}}
}

// Add appends one result built by WhyJSON, RemoveJSON or DupesJSON.
func (d *Doc) Add(result any) { d.Results = append(d.Results, result) }

// SetSuggestions marks the document as a not-found result.
func (d *Doc) SetSuggestions(s []string) {
	if s == nil {
		s = []string{}
	}
	d.Suggestions = &s
}

// Encode writes the document as indented JSON followed by a newline.
func (d *Doc) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

// pkgJSON is a package name and version.
type pkgJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// leadJSON is a Lead: the root, the direct dependency, and the edge kind.
type leadJSON struct {
	Root    string `json:"root"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// stepJSON is one element of a path. The first element is the root and sets
// Root; later ones carry the kind of the edge that led to them.
type stepJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Root    bool   `json:"root,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// whyJSON is one result of why mode.
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

// keptJSON is a package that stays after a removal, and the package keeping it.
type keptJSON struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Via     pkgJSON `json:"via"`
}

// removeJSON is one result of remove mode.
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

// dupeVersionJSON is one version of a duplicated package.
type dupeVersionJSON struct {
	Version string     `json:"version"`
	Leads   []leadJSON `json:"leads"`
}

// dupeJSON is a package installed at several versions.
type dupeJSON struct {
	Name     string            `json:"name"`
	Versions []dupeVersionJSON `json:"versions"`
}

// dupesJSON is one result of dupes mode: one lockfile's duplicates.
type dupesJSON struct {
	Ecosystem string     `json:"ecosystem"`
	Lockfile  string     `json:"lockfile"`
	Dupes     []dupeJSON `json:"dupes"`
}

// pkg converts a node to its JSON name/version pair.
func pkg(g *graph.Graph, n int) pkgJSON {
	return pkgJSON{g.Nodes[n].Name, g.Nodes[n].Version}
}

// leadsJSON converts leads, returning an empty non-nil slice when there are none.
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
