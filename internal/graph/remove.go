package graph

import "sort"

// Kept is a package that stays installed after a removal. Via is the first
// package on the shortest remaining route from a root; it equals Node when a
// root requires Node directly.
type Kept struct{ Node, Via int }

// RemoveResult is the outcome of dropping a direct dependency (SPEC §6.2).
type RemoveResult struct {
	Target      int
	NotDirect   bool   // Target is not required by any root; nothing else is computed
	RequiredBy  []Lead // when NotDirect: the direct dependencies that pull Target in
	Freed       []int  // packages that would leave the tree, sorted by name and version
	Kept        []Kept // packages Target requires that stay because something else needs them
	StillNeeded bool   // Target itself stays, required by another package
	StillVia    int    // when StillNeeded: the package that keeps it
	Kind        Kind   // kind of the root→target edge
	Root        int    // the root that requires Target
	// FreedSize is the bytes freed by the measured packages in Freed, or nil
	// when none could be measured. FreedUnmeasured counts the freed packages
	// that had no size (not installed, e.g. another platform's optional
	// package); they add 0 to FreedSize. Both are set by ComputeFreedSize.
	FreedSize       *int64
	FreedUnmeasured int
}

// Remove analyzes dropping the root→t edges (SPEC §6.2). With prod, root Dev
// edges are ignored throughout, which removes every dev-only node.
func (g *Graph) Remove(t int, prod bool) RemoveResult {
	res := RemoveResult{Target: t}
	direct := g.rootParents(t, prod)
	if len(direct) == 0 {
		res.NotDirect = true
		res.RequiredBy = g.leads(t, prod)
		return res
	}
	res.Root, res.Kind = direct[0].From, direct[0].Kind

	// skip reports whether an edge is ignored in the "after" graph.
	skip := func(e Edge) bool {
		return (prod && e.Kind == Dev && g.Nodes[e.From].Root) || (e.To == t && g.Nodes[e.From].Root)
	}
	before := g.reachable(func(e Edge) bool { return prod && e.Kind == Dev && g.Nodes[e.From].Root })
	after, parent := g.bfsFromRoots(skip)

	for i := range g.Nodes {
		if before[i] && !g.Nodes[i].Root && !after[i] {
			res.Freed = append(res.Freed, i)
		}
	}
	g.sortNodes(res.Freed)

	// first returns the first non-root node on the shortest root path to n.
	first := func(n int) int {
		for !g.Nodes[parent[n]].Root {
			n = parent[n]
		}
		return n
	}
	if after[t] {
		res.StillNeeded, res.StillVia = true, first(t)
	}
	seen := map[int]bool{t: true}
	stack := []int{t}
	var kept []int
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range g.Out[n] {
			// Workspace members keep their own dependencies; they are not "kept" for t.
			if seen[e.To] || g.Nodes[e.To].Root {
				continue
			}
			seen[e.To] = true
			stack = append(stack, e.To)
			if after[e.To] {
				kept = append(kept, e.To)
			}
		}
	}
	g.sortNodes(kept)
	for _, n := range kept {
		res.Kept = append(res.Kept, Kept{Node: n, Via: first(n)})
	}
	return res
}

// ComputeFreedSize sums the sizes of the freed packages. A package sizeOf
// cannot measure counts as 0 and increments FreedUnmeasured, so one missing
// platform-specific package does not hide the total. FreedSize stays nil, and
// the result is false, only when no freed package could be measured.
func (r *RemoveResult) ComputeFreedSize(sizeOf func(int) (int64, bool)) bool {
	if sizeOf == nil {
		return false
	}
	var total int64
	measured := 0
	for _, n := range r.Freed {
		if sz, ok := sizeOf(n); ok {
			total += sz
			measured++
		}
	}
	r.FreedUnmeasured = len(r.Freed) - measured
	if measured == 0 {
		return false
	}
	r.FreedSize = &total
	return true
}

// sortNodes orders node indexes by name, then version.
func (g *Graph) sortNodes(ix []int) {
	sort.Slice(ix, func(a, b int) bool {
		na, nb := g.Nodes[ix[a]], g.Nodes[ix[b]]
		if na.Name != nb.Name {
			return na.Name < nb.Name
		}
		return compareVersions(na.Version, nb.Version) < 0
	})
}

// reachable returns the nodes reachable from the roots, ignoring edges for
// which skip returns true.
func (g *Graph) reachable(skip func(Edge) bool) []bool {
	seen, _ := g.bfsFromRoots(skip)
	return seen
}

// bfsFromRoots returns reachability and each node's BFS parent. Roots are queued
// in lockfile order and out-edges are visited in sorted order, so ties are stable.
func (g *Graph) bfsFromRoots(skip func(Edge) bool) ([]bool, []int) {
	seen := make([]bool, len(g.Nodes))
	parent := make([]int, len(g.Nodes))
	queue := append([]int(nil), g.Roots...)
	for _, r := range g.Roots {
		seen[r] = true
		parent[r] = r
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		out := append([]Edge(nil), g.Out[n]...)
		sort.SliceStable(out, func(a, b int) bool { return g.less(out[a].To, out[b].To) })
		for _, e := range out {
			if skip(e) || seen[e.To] {
				continue
			}
			seen[e.To] = true
			parent[e.To] = n
			queue = append(queue, e.To)
		}
	}
	return seen, parent
}
