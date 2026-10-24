package graph

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// testGraph builds a graph from "from>to" or "from>to:dev" specs. Nodes named in
// roots become roots; every other node is name@1.0.0.
func testGraph(t testing.TB, roots []string, edges ...string) *Graph {
	t.Helper()
	b := NewBuilder("npm", "package-lock.json")
	isRoot := map[string]bool{}
	for _, r := range roots {
		isRoot[r] = true
		b.Root(r, "")
	}
	node := func(n string) int {
		if isRoot[n] {
			return b.Root(n, "")
		}
		return b.Node(n, "1.0.0")
	}
	for _, e := range edges {
		kind := Prod
		if s, ok := strings.CutSuffix(e, ":dev"); ok {
			e, kind = s, Dev
		}
		from, to, _ := strings.Cut(e, ">")
		b.Edge(node(from), node(to), kind)
	}
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func (g *Graph) id(name string) int {
	for i, n := range g.Nodes {
		if n.Name == name {
			return i
		}
	}
	panic("no node " + name)
}

// render flattens a tree to indented "name marker" lines for easy comparison.
func renderTree(g *Graph, n *TreeNode, depth int, out *[]string) {
	name := "…"
	if n.Node >= 0 {
		name = g.Nodes[n.Node].Name
	}
	line := strings.Repeat(" ", depth) + name
	if n.Marker != "" && n.Marker != "…" {
		line += " " + n.Marker
	}
	*out = append(*out, line)
	for _, c := range n.Children {
		renderTree(g, c, depth+1, out)
	}
}

func treeLines(g *Graph, r WhyResult) []string {
	var out []string
	renderTree(g, r.Tree, 0, &out)
	return out
}

func pathNames(g *Graph, r WhyResult) []string {
	var out []string
	for _, p := range r.Paths {
		var names []string
		for _, s := range p {
			names = append(names, g.Nodes[s.Node].Name)
		}
		out = append(out, strings.Join(names, ">"))
	}
	return out
}

func TestWhyDiamond(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "app>b:dev", "a>c", "b>c")
	r := g.Why(g.id("c"), WhyOpts{})
	want := []string{"c", " a", "  app", " b", "  app"}
	if got := treeLines(g, r); !reflect.DeepEqual(got, want) {
		t.Fatalf("tree = %q", got)
	}
	if got := pathNames(g, r); !reflect.DeepEqual(got, []string{"app>a>c", "app>b>c"}) {
		t.Fatalf("paths = %q", got)
	}
	if r.Direct || r.Truncated || r.MorePaths != "" || !(!r.DevOnly) {
		t.Fatalf("flags: %+v", r)
	}
	if len(r.Leads) != 2 || r.Leads[0].Kind != Prod || r.Leads[1].Kind != Dev || g.Nodes[r.Leads[1].Direct].Name != "b" {
		t.Fatalf("leads = %+v", r.Leads)
	}
	// Edge kind of root leaves is the root→direct kind; of a→c is the a→c kind.
	if r.Tree.Children[1].Children[0].Kind != Dev {
		t.Fatalf("root leaf kind = %v", r.Tree.Children[1].Children[0].Kind)
	}
}

func TestWhyRepeatedSubtreeIsMarked(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "a>c", "a>d", "c>e", "d>e")
	r := g.Why(g.id("e"), WhyOpts{})
	want := []string{"e", " c", "  a", "   app", " d", "  a (*)"}
	if got := treeLines(g, r); !reflect.DeepEqual(got, want) {
		t.Fatalf("tree = %q", got)
	}
	if got := pathNames(g, r); !reflect.DeepEqual(got, []string{"app>a>c>e", "app>a>d>e"}) {
		t.Fatalf("paths = %q", got)
	}
}

func TestWhyCycle(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "a>b", "b>a")
	r := g.Why(g.id("b"), WhyOpts{})
	want := []string{"b", " a", "  app", "  b (cycle)"}
	if got := treeLines(g, r); !reflect.DeepEqual(got, want) {
		t.Fatalf("tree = %q", got)
	}
	if got := pathNames(g, r); !reflect.DeepEqual(got, []string{"app>a>b"}) {
		t.Fatalf("paths = %q", got)
	}
}

func TestWhyDirectTargetShowsOnlyRootLeaves(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "app>b", "b>a")
	r := g.Why(g.id("a"), WhyOpts{})
	if !r.Direct {
		t.Fatal("want direct")
	}
	if got := treeLines(g, r); !reflect.DeepEqual(got, []string{"a", " app"}) {
		t.Fatalf("tree = %q", got)
	}
	if got := pathNames(g, r); !reflect.DeepEqual(got, []string{"app>a"}) {
		t.Fatalf("paths = %q", got)
	}
	found := false
	for _, l := range r.Leads {
		found = found || l.Direct == r.Target
	}
	if !found {
		t.Fatalf("leads must include target itself: %+v", r.Leads)
	}
}

func TestWhyDepthTruncates(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "a>b", "b>c")
	r := g.Why(g.id("c"), WhyOpts{Depth: 1})
	want := []string{"c", " b", "  …"}
	if got := treeLines(g, r); !reflect.DeepEqual(got, want) {
		t.Fatalf("tree = %q", got)
	}
	if r.Tree.Children[0].Children[0].Marker != "…" {
		t.Fatal("want … marker")
	}
	// Leads are still complete, and a root exactly at the depth limit is shown.
	if len(r.Leads) != 1 {
		t.Fatalf("leads = %+v", r.Leads)
	}
	r = g.Why(g.id("a"), WhyOpts{Depth: 1})
	if got := treeLines(g, r); !reflect.DeepEqual(got, []string{"a", " app"}) {
		t.Fatalf("tree = %q", got)
	}
}

func parallelGraph(t testing.TB, n int) *Graph {
	var edges []string
	for i := 1; i <= n; i++ {
		edges = append(edges, fmt.Sprintf("app>m%02d", i), fmt.Sprintf("m%02d>t", i))
	}
	return testGraph(t, []string{"app"}, edges...)
}

func TestWhyPathCap(t *testing.T) {
	g := parallelGraph(t, 12)
	r := g.Why(g.id("t"), WhyOpts{})
	if len(r.Tree.Children) != 10 {
		t.Fatalf("children = %d", len(r.Tree.Children))
	}
	if r.MorePaths != "2" || !r.Truncated || len(r.Paths) != 10 {
		t.Fatalf("more=%q trunc=%v paths=%d", r.MorePaths, r.Truncated, len(r.Paths))
	}
	if len(r.Leads) != 12 {
		t.Fatalf("leads must not be capped: %d", len(r.Leads))
	}
	r = g.Why(g.id("t"), WhyOpts{All: true})
	if len(r.Tree.Children) != 12 || r.MorePaths != "" || r.Truncated || len(r.Paths) != 12 {
		t.Fatalf("all: children=%d more=%q paths=%d", len(r.Tree.Children), r.MorePaths, len(r.Paths))
	}
	r = g.Why(g.id("t"), WhyOpts{})
	if g2 := parallelGraph(t, 10); g2.Why(g2.id("t"), WhyOpts{}).MorePaths != "" {
		t.Fatal("exactly 10 paths must not truncate")
	}
}

func TestDevOnlyAndProd(t *testing.T) {
	g := testGraph(t, []string{"app"}, "app>a", "app>b:dev", "b>c", "a>d", "b>d")
	dev := g.DevOnly()
	for name, want := range map[string]bool{"app": false, "a": false, "d": false, "b": true, "c": true} {
		if dev[g.id(name)] != want {
			t.Errorf("devonly[%s] = %v", name, dev[g.id(name)])
		}
	}
	if r := g.Why(g.id("c"), WhyOpts{}); !r.DevOnly {
		t.Error("c must be flagged DevOnly")
	}
	r := g.Why(g.id("d"), WhyOpts{Prod: true})
	if got := treeLines(g, r); !reflect.DeepEqual(got, []string{"d", " a", "  app"}) {
		t.Fatalf("prod tree = %q", got)
	}
	if got := pathNames(g, r); !reflect.DeepEqual(got, []string{"app>a>d"}) {
		t.Fatalf("prod paths = %q", got)
	}
	if len(r.Leads) != 1 {
		t.Fatalf("prod leads = %+v", r.Leads)
	}
	r = g.Why(g.id("d"), WhyOpts{})
	if len(r.Leads) != 2 {
		t.Fatalf("leads = %+v", r.Leads)
	}
}

func TestWhyMorePathsIgnoresDepthHiddenBranches(t *testing.T) {
	var edges []string
	for i := 1; i <= 12; i++ {
		edges = append(edges, fmt.Sprintf("app>m%02d", i), fmt.Sprintf("m%02d>t", i))
	}
	g := testGraph(t, []string{"app"}, edges...)
	r := g.Why(g.id("t"), WhyOpts{Depth: 1})
	if r.MorePaths != "" {
		t.Fatalf("MorePaths = %q; depth hides branches, the cap hid none", r.MorePaths)
	}
}
