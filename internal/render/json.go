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
	Name      string `json:"name"`
	Version   string `json:"version"`
	SizeBytes *int64 `json:"size_bytes,omitempty"`
	*healthJSON
}

// healthJSON is the --online addition to a package object. It is embedded by
// pointer, so offline output has none of these keys.
type healthJSON struct {
	Advisories []string `json:"advisories"`
	Deprecated *string  `json:"deprecated"`
	Yanked     bool     `json:"yanked"`
}

// health returns the online fields for a node, or nil when the run was offline.
// Advisories is [] for a package that was checked and is clean, and null for one
// that could not be checked (a failed lookup, or a version no service knows), so
// a script never mistakes "unchecked" for "clean".
func (n Notes) health(node int) *healthJSON {
	if n == nil {
		return nil
	}
	note := n[node]
	h := &healthJSON{Yanked: note.Yanked}
	if note.Advisories != nil { // nil means "could not be checked": encode null, not []
		h.Advisories = append([]string{}, note.Advisories...)
	}
	if note.Deprecated != "" {
		h.Deprecated = &note.Deprecated
	}
	return h
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
	Name      string `json:"name"`
	Version   string `json:"version"`
	SizeBytes *int64 `json:"size_bytes,omitempty"`
	*healthJSON
	Via pkgJSON `json:"via"`
}

// removeJSON is one result of remove mode.
type removeJSON struct {
	Ecosystem      string     `json:"ecosystem"`
	Lockfile       string     `json:"lockfile"`
	Package        pkgJSON    `json:"package"`
	Direct         bool       `json:"direct"`
	StillNeeded    bool       `json:"still_needed"`
	FreedSizeBytes *int64     `json:"freed_size_bytes,omitempty"`
	Freed          []pkgJSON  `json:"freed"`
	Kept           []keptJSON `json:"kept"`
	RequiredBy     []leadJSON `json:"required_by"`
}

// dupeVersionJSON is one version of a duplicated package.
type dupeVersionJSON struct {
	Version string `json:"version"`
	*healthJSON
	Leads []leadJSON `json:"leads"`
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
	return pkgJSON{Name: g.Nodes[n].Name, Version: g.Nodes[n].Version}
}

// pkgWith is pkg plus online findings and size for the node (none when notes/sizes are nil).
func pkgWith(g *graph.Graph, n int, notes Notes, sizes map[int]int64) pkgJSON {
	p := pkg(g, n)
	p.healthJSON = notes.health(n)
	if sizes != nil {
		if sz, ok := sizes[n]; ok {
			p.SizeBytes = &sz
		}
	}
	return p
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
func WhyJSON(g *graph.Graph, r graph.WhyResult, notes Notes) any {
	return WhyJSONWithSizes(g, r, notes, nil)
}

// WhyJSONWithSizes builds one why result entry with optional size information.
func WhyJSONWithSizes(g *graph.Graph, r graph.WhyResult, notes Notes, sizes map[int]int64) any {
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
	return whyJSON{g.Ecosystem, filepath.Base(g.Lockfile), pkgWith(g, r.Target, notes, sizes), r.DevOnly, r.Direct, paths, r.Truncated, leadsJSON(g, r.Leads)}
}

// RemoveJSON builds one remove result entry.
func RemoveJSON(g *graph.Graph, r graph.RemoveResult, notes Notes) any {
	return RemoveJSONWithSizes(g, r, notes, nil)
}

// RemoveJSONWithSizes builds one remove result entry with optional size information.
func RemoveJSONWithSizes(g *graph.Graph, r graph.RemoveResult, notes Notes, sizes map[int]int64) any {
	out := removeJSON{
		Ecosystem: g.Ecosystem, Lockfile: filepath.Base(g.Lockfile), Package: pkgWith(g, r.Target, notes, sizes),
		Direct: !r.NotDirect, StillNeeded: r.StillNeeded, FreedSizeBytes: r.FreedSize,
		Freed: []pkgJSON{}, Kept: []keptJSON{}, RequiredBy: leadsJSON(g, r.RequiredBy),
	}
	for _, n := range r.Freed {
		out.Freed = append(out.Freed, pkgWith(g, n, notes, sizes))
	}
	for _, k := range r.Kept {
		p := pkg(g, k.Node)
		if sizes != nil {
			if sz, ok := sizes[k.Node]; ok {
				p.SizeBytes = &sz
			}
		}
		viaP := pkg(g, k.Via)
		if sizes != nil {
			if sz, ok := sizes[k.Via]; ok {
				viaP.SizeBytes = &sz
			}
		}
		out.Kept = append(out.Kept, keptJSON{p.Name, p.Version, p.SizeBytes, notes.health(k.Node), viaP})
	}
	return out
}

// DupesJSON builds one dupes result entry.
func DupesJSON(g *graph.Graph, ds []graph.Dupe, notes Notes) any {
	out := dupesJSON{Ecosystem: g.Ecosystem, Lockfile: filepath.Base(g.Lockfile), Dupes: []dupeJSON{}}
	for _, d := range ds {
		dj := dupeJSON{Name: d.Name, Versions: []dupeVersionJSON{}}
		for _, v := range d.Versions {
			dj.Versions = append(dj.Versions, dupeVersionJSON{g.Nodes[v.Node].Version, notes.health(v.Node), leadsJSON(g, v.Leads)})
		}
		out.Dupes = append(out.Dupes, dj)
	}
	return out
}

type heaviestItemJSON struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	FreedSize  int64  `json:"freed_size_bytes"`
	FreedCount int    `json:"freed_count"`
}

type heaviestJSON struct {
	Ecosystem string             `json:"ecosystem"`
	Lockfile  string             `json:"lockfile"`
	Heaviest  []heaviestItemJSON `json:"heaviest"`
}

// HeaviestJSON builds one heaviest result entry.
func HeaviestJSON(g *graph.Graph, items []graph.HeaviestItem) any {
	out := heaviestJSON{
		Ecosystem: g.Ecosystem,
		Lockfile:  filepath.Base(g.Lockfile),
		Heaviest:  []heaviestItemJSON{},
	}
	for _, it := range items {
		n := g.Nodes[it.Target]
		out.Heaviest = append(out.Heaviest, heaviestItemJSON{
			Name:       n.Name,
			Version:    n.Version,
			FreedSize:  it.FreedSize,
			FreedCount: it.FreedCount,
		})
	}
	return out
}
