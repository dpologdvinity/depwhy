package graph

import "testing"

func TestBuilderDedupesNodes(t *testing.T) {
	b := NewBuilder("npm", "package-lock.json")
	if b.Node("a", "1.0.0") != b.Node("a", "1.0.0") {
		t.Fatal("same ID must give same index")
	}
	if b.Node("a", "1.0.0") == b.Node("a", "2.0.0") {
		t.Fatal("different versions must differ")
	}
	if b.Root("app", "1") != b.Root("app", "1") {
		t.Fatal("root must dedupe")
	}
}

func TestBuilderDedupesEdgesAndMirrorsIn(t *testing.T) {
	b := NewBuilder("npm", "x")
	r := b.Root("app", "")
	a := b.Node("a", "1")
	c := b.Node("c", "1")
	b.Edge(r, a, Prod)
	b.Edge(r, a, Prod)
	b.Edge(r, a, Dev) // different kind: kept
	b.Edge(a, c, Prod)
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Out[r]) != 2 {
		t.Fatalf("Out[root] = %v", g.Out[r])
	}
	if len(g.In[a]) != 2 || len(g.In[c]) != 1 || g.In[c][0].From != a {
		t.Fatalf("In not mirrored: %v %v", g.In[a], g.In[c])
	}
	if len(g.Roots) != 1 || g.Roots[0] != r || !g.Nodes[r].Root {
		t.Fatalf("roots = %v", g.Roots)
	}
	if g.Nodes[a].ID != "a@1" || g.Nodes[r].ID != "root:app" || g.Nodes[a].Ecosystem != "npm" {
		t.Fatalf("nodes = %+v", g.Nodes)
	}
}

func TestBuildFailsWithoutRoots(t *testing.T) {
	b := NewBuilder("npm", "x")
	b.Node("a", "1")
	if _, err := b.Build(); err == nil || err.Error() != "no root packages found" {
		t.Fatalf("err = %v", err)
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{Prod: "prod", Dev: "dev", Optional: "optional", Peer: "peer"} {
		if k.String() != want {
			t.Errorf("%d = %q", k, k.String())
		}
	}
}
