package graph

import "testing"

func TestHeaviest(t *testing.T) {
	g := fixtureGraph(t)
	// a: 100, b: 200, c1: 50, c2: 100, d: 500
	sizes := map[string]int64{
		"a@1.0.0": 100,
		"b@1.0.0": 200,
		"c@1.0.0": 50,
		"c@2.0.0": 100,
		"d@1.0.0": 500,
	}
	sizeOf := func(n int) (int64, bool) {
		sz, ok := sizes[g.Nodes[n].ID]
		return sz, ok
	}

	items := g.Heaviest(HeaviestOpts{
		Size: sizeOf,
	})
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	// b frees b (200) + c2 (100) = 300
	if g.Nodes[items[0].Target].Name != "b" || items[0].FreedSize != 300 || items[0].FreedCount != 2 {
		t.Errorf("items[0] = %+v, want b with size 300", items[0])
	}
	// a frees a (100) + c1 (50) = 150
	if g.Nodes[items[1].Target].Name != "a" || items[1].FreedSize != 150 || items[1].FreedCount != 2 {
		t.Errorf("items[1] = %+v, want a with size 150", items[1])
	}

	// Test Prod: ignores dev dependency b
	prodItems := g.Heaviest(HeaviestOpts{
		Prod: true,
		Size: sizeOf,
	})
	if len(prodItems) != 1 || g.Nodes[prodItems[0].Target].Name != "a" {
		t.Errorf("prodItems = %+v, want only a", prodItems)
	}

	// Test Limit
	limited := g.Heaviest(HeaviestOpts{
		Limit: 1,
		Size:  sizeOf,
	})
	if len(limited) != 1 || g.Nodes[limited[0].Target].Name != "b" {
		t.Errorf("limited = %+v, want 1 item (b)", limited)
	}
}

func TestComputeFreedSize(t *testing.T) {
	g := fixtureGraph(t)
	rem := g.Remove(g.byID("a@1.0.0"), false)
	sizes := map[string]int64{
		"a@1.0.0": 100,
		"c@1.0.0": 50,
	}
	ok := rem.ComputeFreedSize(func(n int) (int64, bool) {
		sz, ok := sizes[g.Nodes[n].ID]
		return sz, ok
	})
	if !ok || rem.FreedSize == nil || *rem.FreedSize != 150 {
		t.Errorf("ComputeFreedSize = %v, FreedSize = %v, want 150", ok, rem.FreedSize)
	}

	// A missing size counts as 0 and is recorded as unmeasured.
	partial := g.Remove(g.byID("a@1.0.0"), false)
	ok = partial.ComputeFreedSize(func(n int) (int64, bool) {
		if g.Nodes[n].Name == "c" {
			return 0, false
		}
		return 100, true
	})
	if !ok || partial.FreedSize == nil || *partial.FreedSize != 100 || partial.FreedUnmeasured != 1 {
		t.Errorf("partial = %v, %v, %d; want 100 with 1 unmeasured", ok, partial.FreedSize, partial.FreedUnmeasured)
	}

	// Nothing measurable: no size at all.
	none := g.Remove(g.byID("a@1.0.0"), false)
	ok = none.ComputeFreedSize(func(int) (int64, bool) { return 0, false })
	if ok || none.FreedSize != nil || none.FreedUnmeasured != 2 {
		t.Errorf("none = %v, %v, %d; want no size, 2 unmeasured", ok, none.FreedSize, none.FreedUnmeasured)
	}
	if nilSize := g.Remove(g.byID("a@1.0.0"), false); nilSize.ComputeFreedSize(nil) {
		t.Error("nil sizeOf must report no size")
	}
}

// TestHeaviestPartialSizes checks that a direct dependency whose freed set is
// only partly installed (platform-specific optional packages) is still ranked,
// with the missing packages counted, and that one with no measurable package
// is left out.
func TestHeaviestPartialSizes(t *testing.T) {
	g := fixtureGraph(t)
	sizes := map[string]int64{"a@1.0.0": 100, "b@1.0.0": 7}
	items := g.Heaviest(HeaviestOpts{Size: func(n int) (int64, bool) {
		sz, ok := sizes[g.Nodes[n].ID]
		return sz, ok
	}})
	want := []HeaviestItem{
		{Target: g.byID("a@1.0.0"), FreedSize: 100, FreedCount: 2, FreedUnmeasured: 1},
		{Target: g.byID("b@1.0.0"), FreedSize: 7, FreedCount: 2, FreedUnmeasured: 1},
	}
	if len(items) != 2 || items[0] != want[0] || items[1] != want[1] {
		t.Fatalf("items = %+v, want %+v", items, want)
	}
	delete(sizes, "b@1.0.0")
	items = g.Heaviest(HeaviestOpts{Size: func(n int) (int64, bool) {
		sz, ok := sizes[g.Nodes[n].ID]
		return sz, ok
	}})
	if len(items) != 1 || items[0] != want[0] {
		t.Fatalf("items = %+v, want only a", items)
	}
}
