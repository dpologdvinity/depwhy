package graph

import (
	"math/rand/v2"
	"strconv"
	"testing"
	"time"
)

// synthetic builds a 3,000-node DAG with fan-out 5 plus a few back-edges.
func synthetic(t testing.TB) *Graph {
	const n = 3000
	rng := rand.New(rand.NewPCG(1, 2))
	b := NewBuilder("npm", "package-lock.json")
	root := b.Root("app", "1.0.0")
	nodes := make([]int, n)
	for i := range nodes {
		nodes[i] = b.Node("p"+strconv.Itoa(i), "1.0.0")
	}
	for i := 0; i < 20; i++ {
		b.Edge(root, nodes[i], Prod)
	}
	for i := 0; i < n-1; i++ {
		for j := 0; j < 5; j++ {
			b.Edge(nodes[i], nodes[i+1+rng.IntN(n-1-i)], Prod)
		}
	}
	for j := 0; j < 5; j++ { // back-edges create cycles
		b.Edge(nodes[n-1-rng.IntN(100)], nodes[rng.IntN(100)], Prod)
	}
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestPerformance guards the spec's speed requirement: Why, Remove and Dupes must
// each finish in under 200 ms on a 3,000-node graph. It is skipped in short mode
// and under -race, where timings are meaningless.
func TestPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if raceEnabled {
		t.Skip("wall-clock guard is not meaningful under -race")
	}
	g := synthetic(t)
	limit := 200 * time.Millisecond

	start := time.Now()
	r := g.Why(len(g.Nodes)-1, WhyOpts{})
	if d := time.Since(start); d >= limit {
		t.Errorf("Why took %v", d)
	}
	if len(r.Paths) == 0 || len(r.Leads) == 0 {
		t.Fatal("deep leaf should be reachable")
	}

	start = time.Now()
	g.Remove(g.Out[g.Roots[0]][0].To, false)
	if d := time.Since(start); d >= limit {
		t.Errorf("Remove took %v", d)
	}
	start = time.Now()
	g.Dupes(false)
	if d := time.Since(start); d >= limit {
		t.Errorf("Dupes took %v", d)
	}
}
