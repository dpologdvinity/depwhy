package graph

import (
	"reflect"
	"testing"
)

// fixtureGraph mirrors testdata/projects/npm: app→a(prod),b(dev); a→c1,d; b→c2,d.
func fixtureGraph(t testing.TB) *Graph {
	t.Helper()
	b := NewBuilder("npm", "package-lock.json")
	app := b.Root("app", "1.0.0")
	a, bb := b.Node("a", "1.0.0"), b.Node("b", "1.0.0")
	c1, c2, d := b.Node("c", "1.0.0"), b.Node("c", "2.0.0"), b.Node("d", "1.0.0")
	b.Edge(app, a, Prod)
	b.Edge(app, bb, Dev)
	b.Edge(a, c1, Prod)
	b.Edge(a, d, Prod)
	b.Edge(bb, c2, Prod)
	b.Edge(bb, d, Prod)
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func (g *Graph) byID(id string) int {
	for i, n := range g.Nodes {
		if n.ID == id {
			return i
		}
	}
	panic("no node " + id)
}

func ids(g *Graph, ix []int) []string {
	out := []string{}
	for _, i := range ix {
		out = append(out, g.Nodes[i].ID)
	}
	return out
}

func keptStrings(g *Graph, ks []Kept) []string {
	out := []string{}
	for _, k := range ks {
		out = append(out, g.Nodes[k.Node].ID+" via "+g.Nodes[k.Via].ID)
	}
	return out
}

func TestRemoveDirect(t *testing.T) {
	g := fixtureGraph(t)
	r := g.Remove(g.byID("a@1.0.0"), false)
	if r.NotDirect || r.StillNeeded || r.Kind != Prod || g.Nodes[r.Root].Name != "app" {
		t.Fatalf("%+v", r)
	}
	if got := ids(g, r.Freed); !reflect.DeepEqual(got, []string{"a@1.0.0", "c@1.0.0"}) {
		t.Fatalf("freed = %v", got)
	}
	if got := keptStrings(g, r.Kept); !reflect.DeepEqual(got, []string{"d@1.0.0 via b@1.0.0"}) {
		t.Fatalf("kept = %v", got)
	}
}

func TestRemoveNotDirect(t *testing.T) {
	g := fixtureGraph(t)
	r := g.Remove(g.byID("c@1.0.0"), false)
	if !r.NotDirect || len(r.RequiredBy) != 1 || g.Nodes[r.RequiredBy[0].Direct].Name != "a" || r.RequiredBy[0].Kind != Prod {
		t.Fatalf("%+v", r)
	}
	if len(r.Freed) != 0 {
		t.Fatalf("freed = %v", r.Freed)
	}
}

func TestRemoveTargetStaysWhenRequiredElsewhere(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "app>b", "b>a", "a>x")
	r := g.Remove(g.id("a"), false)
	if !r.StillNeeded || g.Nodes[r.StillVia].Name != "b" {
		t.Fatalf("%+v", r)
	}
	if len(r.Freed) != 0 {
		t.Fatalf("freed = %v", ids(g, r.Freed))
	}
	if got := keptStrings(g, r.Kept); !reflect.DeepEqual(got, []string{"x@1.0.0 via b@1.0.0"}) {
		t.Fatalf("kept = %v", got)
	}
}

func TestRemoveProdIgnoresDevRoutes(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "app>b:dev", "b>a")
	if r := g.Remove(g.id("a"), false); !r.StillNeeded {
		t.Fatalf("without --prod a stays via dev b: %+v", r)
	}
	r := g.Remove(g.id("a"), true)
	if r.StillNeeded || !reflect.DeepEqual(ids(g, r.Freed), []string{"a@1.0.0"}) {
		t.Fatalf("prod: %+v", r)
	}
	// A dev dependency is not a direct dependency under --prod.
	if r := g.Remove(g.id("b"), true); !r.NotDirect || len(r.RequiredBy) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestRemoveHandlesCycles(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "a>b", "b>a", "b>c")
	r := g.Remove(g.id("a"), false)
	if got := ids(g, r.Freed); !reflect.DeepEqual(got, []string{"a@1.0.0", "b@1.0.0", "c@1.0.0"}) {
		t.Fatalf("freed = %v", got)
	}
}

func TestDupes(t *testing.T) {
	g := fixtureGraph(t)
	ds := g.Dupes(false)
	if len(ds) != 1 || ds[0].Name != "c" || len(ds[0].Versions) != 2 {
		t.Fatalf("%+v", ds)
	}
	v1, v2 := ds[0].Versions[0], ds[0].Versions[1]
	if g.Nodes[v1.Node].Version != "1.0.0" || g.Nodes[v2.Node].Version != "2.0.0" {
		t.Fatalf("version order: %+v", ds[0].Versions)
	}
	if len(v1.Leads) != 1 || g.Nodes[v1.Leads[0].Direct].Name != "a" || v1.Leads[0].Kind != Prod {
		t.Fatalf("leads1 = %+v", v1.Leads)
	}
	if len(v2.Leads) != 1 || g.Nodes[v2.Leads[0].Direct].Name != "b" || v2.Leads[0].Kind != Dev {
		t.Fatalf("leads2 = %+v", v2.Leads)
	}
	if got := g.Dupes(true); len(got) != 0 {
		t.Fatalf("prod must drop dev-only c@2.0.0: %+v", got)
	}
}

func TestDupesSortByCountThenName(t *testing.T) {
	b := NewBuilder("npm", "l")
	r := b.Root("app", "")
	for _, p := range [][2]string{{"z", "1"}, {"z", "2"}, {"a", "1"}, {"a", "2"}, {"m", "1"}, {"m", "2"}, {"m", "3"}, {"solo", "1"}} {
		b.Edge(r, b.Node(p[0], p[1]), Prod)
	}
	g, _ := b.Build()
	var names []string
	for _, d := range g.Dupes(false) {
		names = append(names, d.Name)
	}
	if !reflect.DeepEqual(names, []string{"m", "a", "z"}) {
		t.Fatalf("order = %v", names)
	}
}

func TestRemoveKeptStopsAtWorkspaceRoots(t *testing.T) {
	g := testGraph(t, []string{"app", "lib"}, "app>a", "a>lib", "lib>x")
	r := g.Remove(g.id("a"), false)
	if len(r.Kept) != 0 {
		t.Fatalf("kept = %v", keptStrings(g, r.Kept))
	}
}
