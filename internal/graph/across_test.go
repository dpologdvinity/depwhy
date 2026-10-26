package graph

import (
	"reflect"
	"testing"
)

// acrossGraph builds a workspace with a prod and a dev version of shared.
func acrossGraph(t *testing.T, ecosystem, file, prodVersion, devVersion string) *Graph {
	t.Helper()
	b := NewBuilder(ecosystem, file)
	r := b.Root("app", "")
	workspace := b.Root("workspace", "")
	a := b.Node("a", "1")
	b.Edge(r, a, Prod)
	b.Edge(workspace, a, Optional)
	b.Edge(a, b.Node("shared", prodVersion), Prod)
	if devVersion != "" {
		b.Edge(r, b.Node("shared", devVersion), Dev)
	}
	b.Node("orphan", prodVersion)
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestDupesAcross(t *testing.T) {
	a := acrossGraph(t, "npm", "a/package-lock.json", "1", "3")
	b := acrossGraph(t, "npm", "b/package-lock.json", "2", "3")
	c := acrossGraph(t, "npm", "c/package-lock.json", "1", "")
	python := acrossGraph(t, "python", "poetry.lock", "9", "")
	graphs := []*Graph{b, python, c, a, a}
	ds := DupesAcross(graphs, false)
	if len(ds) != 2 || ds[0].Name != "shared" || ds[0].Ecosystem != "npm" || ds[0].Lockfiles != 3 || len(ds[0].Versions) != 3 {
		t.Fatalf("dupes = %+v", ds)
	}
	v := ds[0].Versions[0]
	if v.Version != "1" || len(v.Sources) != 2 || v.Sources[0].Graph != a || v.Sources[1].Graph != c || len(v.Sources[0].Leads) != 2 {
		t.Fatalf("version 1 = %+v", v)
	}
	if ds[0].Versions[2].Version != "3" || len(ds[0].Versions[2].Sources) != 2 || ds[0].Versions[2].Sources[0].Leads[0].Kind != Dev {
		t.Fatalf("version 3 = %+v", ds[0].Versions[2])
	}
	if ds[1].Name != "orphan" || ds[1].Lockfiles != 3 || len(ds[1].Versions[0].Sources[0].Leads) != 0 {
		t.Fatalf("orphan = %+v", ds[1])
	}
	prod := DupesAcross(graphs, true)
	if len(prod) != 2 || len(prod[1].Versions) != 2 || prod[1].Name != "shared" {
		t.Fatalf("prod = %+v", prod)
	}
	for _, l := range prod[1].Versions[0].Sources[0].Leads {
		if l.Kind == Dev {
			t.Fatal("dev lead in prod results")
		}
	}
	if got := DupesAcross([]*Graph{c, python}, false); len(got) != 0 {
		t.Fatalf("different ecosystems must not conflict: %+v", got)
	}
	if got := DupesAcross([]*Graph{a}, false); len(got) != 1 || got[0].Lockfiles != 1 {
		t.Fatalf("single lockfile duplicates = %+v", got)
	}
	for i := 0; i < 10; i++ {
		if got := DupesAcross(graphs, false); !reflect.DeepEqual(got, ds) {
			t.Fatal("nondeterministic aggregation")
		}
	}
}

func TestAcrossTieOrder(t *testing.T) {
	npm := acrossGraph(t, "npm", "package-lock.json", "1", "2")
	python := acrossGraph(t, "python", "poetry.lock", "1", "2")
	ds := DupesAcross([]*Graph{python, npm}, false)
	if len(ds) != 2 || ds[0].Ecosystem != "npm" || ds[1].Ecosystem != "python" {
		t.Fatalf("order = %+v", ds)
	}
}

func TestAcrossSourceLabelOrder(t *testing.T) {
	a := acrossGraph(t, "npm", "/repo/inside/a/package-lock.json", "1", "3")
	b := acrossGraph(t, "npm", "/repo/zoutside/package-lock.json", "1", "3")
	a.LockfileLabel, b.LockfileLabel = "a/package-lock.json", "../zoutside/package-lock.json"
	ds := DupesAcross([]*Graph{a, b}, false)
	if len(ds) != 1 || ds[0].Versions[0].Sources[0].Graph != b {
		t.Fatal("sources must sort by the displayed relative path")
	}
}
