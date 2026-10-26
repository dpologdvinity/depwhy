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

	// Missing size returns false and leaves nil
	remMissing := g.Remove(g.byID("a@1.0.0"), false)
	ok = remMissing.ComputeFreedSize(func(n int) (int64, bool) {
		if g.Nodes[n].Name == "c" {
			return 0, false
		}
		return 100, true
	})
	if ok || remMissing.FreedSize != nil {
		t.Errorf("ComputeFreedSize with missing size should return false, got %v", ok)
	}
}
