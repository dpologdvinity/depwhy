package render

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
	"github.com/dpologdvinity/depwhy/internal/lockfile"
)

// npmGraph parses the shared npm fixture.
func npmGraph(t *testing.T) *graph.Graph {
	t.Helper()
	data, err := os.ReadFile("../../testdata/projects/npm/package-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := lockfile.ParseNPM("testdata/projects/npm/package-lock.json", data)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// find returns the index of the node with the given ID or fails the test.
func find(t *testing.T, g *graph.Graph, id string) int {
	t.Helper()
	for i, n := range g.Nodes {
		if n.ID == id {
			return i
		}
	}
	t.Fatalf("no node %s", id)
	return -1
}

// whyText renders the why tree for the node with the given ID.
func whyText(t *testing.T, id string, o graph.WhyOpts, s Style) string {
	g := npmGraph(t)
	var buf bytes.Buffer
	WhyText(&buf, g, g.Why(find(t, g, id), o), s)
	return buf.String()
}

// TestWhyTextDiamond checks the spec's main example, including the footer.
func TestWhyTextDiamond(t *testing.T) {
	want := `d@1.0.0  npm · package-lock.json
├── a@1.0.0
│   └── app (root, prod)
└── b@1.0.0
    └── app (root, dev)
pulled in by 2 direct dependencies: a (prod), b (dev)
`
	if got := whyText(t, "d@1.0.0", graph.WhyOpts{}, Style{}); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWhyTextSingleLead checks that a single lead gets no footer.
func TestWhyTextSingleLead(t *testing.T) {
	want := `c@1.0.0  npm · package-lock.json
└── a@1.0.0
    └── app (root, prod)
`
	if got := whyText(t, "c@1.0.0", graph.WhyOpts{}, Style{}); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

// TestWhyTextDirect checks the "direct dependency of" footer.
func TestWhyTextDirect(t *testing.T) {
	want := `a@1.0.0  npm · package-lock.json
└── app (root, prod)
direct dependency of app (prod)
`
	if got := whyText(t, "a@1.0.0", graph.WhyOpts{}, Style{}); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

// TestWhyTextDepthEllipsis checks how a depth-truncated branch is drawn.
func TestWhyTextDepthEllipsis(t *testing.T) {
	want := `d@1.0.0  npm · package-lock.json
├── a@1.0.0
│   └── …
└── b@1.0.0
    └── …
pulled in by 2 direct dependencies: a (prod), b (dev)
`
	if got := whyText(t, "d@1.0.0", graph.WhyOpts{Depth: 1}, Style{}); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

// TestWhyTextMarkersAndCap checks the (*) and (cycle) markers and the "more paths"
// line, which must be the last child.
func TestWhyTextMarkersAndCap(t *testing.T) {
	b := graph.NewBuilder("npm", "package-lock.json")
	app := b.Root("app", "")
	a, c, d := b.Node("a", "1"), b.Node("c", "1"), b.Node("d", "1")
	b.Edge(app, a, graph.Prod)
	b.Edge(a, c, graph.Prod)
	b.Edge(a, d, graph.Prod)
	b.Edge(c, d, graph.Prod)
	b.Edge(d, c, graph.Prod)
	g, _ := b.Build()
	var buf bytes.Buffer
	WhyText(&buf, g, g.Why(d, graph.WhyOpts{}), Style{})
	want := `d@1  npm · package-lock.json
├── a@1
│   └── app (root, prod)
└── c@1
    ├── a@1 (*)
    └── d@1 (cycle)
`
	if buf.String() != want {
		t.Fatalf("got:\n%s", buf.String())
	}

	b = graph.NewBuilder("npm", "package-lock.json")
	app = b.Root("app", "")
	tgt := b.Node("t", "1")
	for _, n := range []string{"m01", "m02", "m03", "m04", "m05", "m06", "m07", "m08", "m09", "m10", "m11", "m12"} {
		m := b.Node(n, "1")
		b.Edge(app, m, graph.Prod)
		b.Edge(m, tgt, graph.Prod)
	}
	g, _ = b.Build()
	buf.Reset()
	WhyText(&buf, g, g.Why(tgt, graph.WhyOpts{}), Style{})
	out := buf.String()
	if !strings.Contains(out, "└── … 2 more paths (use --all)\n") || strings.Contains(out, "m11@1") {
		t.Fatalf("got:\n%s", out)
	}
	if !strings.Contains(out, "├── m10@1\n│   └── app (root, prod)\n└── … 2 more paths (use --all)\n") {
		t.Fatalf("connector wrong:\n%s", out)
	}
}

// TestWhyTextColor checks that color adds escape codes and plain output has none.
func TestWhyTextColor(t *testing.T) {
	got := whyText(t, "d@1.0.0", graph.WhyOpts{}, Style{Color: true})
	for _, s := range []string{"\x1b[1md@1.0.0  npm · package-lock.json\x1b[0m", "\x1b[36mapp\x1b[0m", "\x1b[2mdev\x1b[0m"} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in %q", s, got)
		}
	}
	if plain := whyText(t, "d@1.0.0", graph.WhyOpts{}, Style{}); strings.Contains(plain, "\x1b") {
		t.Fatal("plain output has escapes")
	}
}

// removeText renders the removal report for the node with the given ID.
func removeText(t *testing.T, id string, prod bool) string {
	g := npmGraph(t)
	var buf bytes.Buffer
	RemoveText(&buf, g, g.Remove(find(t, g, id), prod), Style{})
	return buf.String()
}

// TestRemoveText checks the direct and not-direct removal reports.
func TestRemoveText(t *testing.T) {
	want := `removing a@1.0.0 (direct, prod) from app  npm · package-lock.json
would remove 2 packages:
  a@1.0.0
  c@1.0.0
still needed by others (1):
  d@1.0.0  via b@1.0.0
`
	if got := removeText(t, "a@1.0.0", false); got != want {
		t.Fatalf("got:\n%s", got)
	}
	want = "c@1.0.0 is not a direct dependency. it is required by: a (prod)\nremove those instead, or run `depwhy c` for details.\n"
	if got := removeText(t, "c@1.0.0", false); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

// TestRemoveTextStillNeeded checks the report when the target stays installed.
func TestRemoveTextStillNeeded(t *testing.T) {
	b := graph.NewBuilder("npm", "package-lock.json")
	app := b.Root("app", "")
	a, bb := b.Node("a", "1.0.0"), b.Node("b", "1.0.0")
	b.Edge(app, a, graph.Prod)
	b.Edge(app, bb, graph.Prod)
	b.Edge(bb, a, graph.Prod)
	g, _ := b.Build()
	var buf bytes.Buffer
	RemoveText(&buf, g, g.Remove(a, false), Style{})
	want := `removing a@1.0.0 (direct, prod) from app  npm · package-lock.json
would remove 0 packages
a@1.0.0 itself stays: also required via b@1.0.0
`
	if buf.String() != want {
		t.Fatalf("got:\n%s", buf.String())
	}
}

// TestDupesText checks the duplicates listing and the empty case.
func TestDupesText(t *testing.T) {
	g := npmGraph(t)
	var buf bytes.Buffer
	DupesText(&buf, g, g.Dupes(false), Style{})
	want := `npm · package-lock.json
c  2 versions
  1.0.0  via a (prod)
  2.0.0  via b (dev)
`
	if buf.String() != want {
		t.Fatalf("got:\n%s", buf.String())
	}
	buf.Reset()
	DupesText(&buf, g, g.Dupes(true), Style{})
	if buf.String() != "npm · package-lock.json\nno duplicate packages\n" {
		t.Fatalf("got:\n%s", buf.String())
	}
}

// TestJSONWhy checks the shape of a why document: schema, query, path elements,
// leads, and the absence of suggestions.
func TestJSONWhy(t *testing.T) {
	g := npmGraph(t)
	doc := NewDoc("why", &Query{Name: "c"})
	doc.Add(WhyJSON(g, g.Why(find(t, g, "c@1.0.0"), graph.WhyOpts{})))
	var buf bytes.Buffer
	if err := doc.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["schema"] != float64(1) || m["mode"] != "why" {
		t.Fatalf("%v", m)
	}
	q := m["query"].(map[string]any)
	if q["name"] != "c" || q["version"] != nil {
		t.Fatalf("query = %v", q)
	}
	res := m["results"].([]any)[0].(map[string]any)
	if res["ecosystem"] != "npm" || res["lockfile"] != "package-lock.json" || res["direct"] != false || res["dev_only"] != false || res["paths_truncated"] != false {
		t.Fatalf("%v", res)
	}
	path := res["paths"].([]any)[0].([]any)
	first := path[0].(map[string]any)
	second := path[1].(map[string]any)
	if first["name"] != "app" || first["root"] != true || second["kind"] != "prod" || second["name"] != "a" {
		t.Fatalf("path = %v", path)
	}
	lead := res["leads"].([]any)[0].(map[string]any)
	if lead["root"] != "app" || lead["name"] != "a" || lead["kind"] != "prod" {
		t.Fatalf("lead = %v", lead)
	}
	if _, ok := m["suggestions"]; ok {
		t.Fatal("suggestions only on not-found")
	}
	if m["warnings"] == nil {
		t.Fatal("warnings must be [] not null")
	}
}

// TestJSONNotFoundRemoveDupes checks the not-found document and the remove and
// dupes documents, including that arrays are never null.
func TestJSONNotFoundRemoveDupes(t *testing.T) {
	doc := NewDoc("why", &Query{Name: "zzz"})
	doc.SetSuggestions(nil)
	var buf bytes.Buffer
	if err := doc.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"results": []`, `"suggestions": []`, `"warnings": []`} {
		if !strings.Contains(buf.String(), s) {
			t.Errorf("missing %s in %s", s, buf.String())
		}
	}

	g := npmGraph(t)
	doc = NewDoc("remove", &Query{Name: "a", Version: "1.0.0"})
	doc.Add(RemoveJSON(g, g.Remove(find(t, g, "a@1.0.0"), false)))
	buf.Reset()
	if err := doc.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	var m struct {
		Query   map[string]any
		Results []struct {
			Direct      bool
			StillNeeded bool `json:"still_needed"`
			Freed       []map[string]string
			Kept        []struct {
				Name, Version string
				Via           map[string]string
			}
			RequiredBy []any `json:"required_by"`
		}
	}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	r := m.Results[0]
	if m.Query["version"] != "1.0.0" || !r.Direct || r.StillNeeded || len(r.Freed) != 2 || r.Kept[0].Name != "d" || r.Kept[0].Via["name"] != "b" || r.RequiredBy == nil {
		t.Fatalf("%s", buf.String())
	}

	doc = NewDoc("dupes", nil)
	doc.Add(DupesJSON(g, g.Dupes(false)))
	buf.Reset()
	if err := doc.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"query": null`) || !strings.Contains(buf.String(), `"dupes": [`) {
		t.Fatalf("%s", buf.String())
	}
}

// TestEnabled checks the color rules: a terminal enables it; --no-color, NO_COLOR
// and a regular file disable it.
func TestEnabled(t *testing.T) {
	f, err := os.Open(os.DevNull) // a char device
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	t.Setenv("NO_COLOR", "")
	if !Enabled(f, false) {
		t.Error("char device should enable color")
	}
	if Enabled(f, true) {
		t.Error("--no-color wins")
	}
	t.Setenv("NO_COLOR", "1")
	if Enabled(f, false) {
		t.Error("NO_COLOR wins")
	}
	t.Setenv("NO_COLOR", "")
	reg, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reg.Close() }()
	if Enabled(reg, false) {
		t.Error("regular file is not a terminal")
	}
}
