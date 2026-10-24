package graph

import (
	"sort"
	"strconv"
)

// pathCap is the number of paths printed before the rest are summarized (SPEC §6.1).
const pathCap = 10

// pathVisitLimit bounds the path search so pathological graphs cannot hang a capped run.
const pathVisitLimit = 1_000_000

type WhyOpts struct {
	Depth int
	All   bool
	Prod  bool
}

type TreeNode struct {
	Node     int    // node index; -1 for the "…" placeholder
	Kind     Kind   // kind of the edge from this node to its parent in the tree
	Marker   string // "", "(*)", "(cycle)", "…"
	Children []*TreeNode
}

type Lead struct {
	Root, Direct int
	Kind         Kind
}

type PathStep struct {
	Node int
	Kind Kind // kind of the edge that led to this step; zero for the root
}

type WhyResult struct {
	Target    int
	Tree      *TreeNode
	Paths     [][]PathStep // root-first, capped unless All
	Truncated bool         // Paths was cut by the cap
	MorePaths string       // text-tree leaves hidden by the cap: "" or a count
	Leads     []Lead
	Direct    bool
	DevOnly   bool
}

// DevOnly reports, per node, whether it is reachable from the roots only through
// Dev edges that leave a root (SPEC §5.3). The result is cached.
func (g *Graph) DevOnly() []bool {
	if g.devOnly != nil {
		return g.devOnly
	}
	reach := func(skipRootDev bool) []bool {
		seen := make([]bool, len(g.Nodes))
		stack := append([]int(nil), g.Roots...)
		for _, r := range g.Roots {
			seen[r] = true
		}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, e := range g.Out[n] {
				if skipRootDev && e.Kind == Dev && g.Nodes[n].Root {
					continue
				}
				if !seen[e.To] {
					seen[e.To] = true
					stack = append(stack, e.To)
				}
			}
		}
		return seen
	}
	all, prod := reach(false), reach(true)
	g.devOnly = make([]bool, len(g.Nodes))
	for i := range g.devOnly {
		g.devOnly[i] = all[i] && !prod[i]
	}
	return g.devOnly
}

// less orders nodes roots first, then by name, then version.
func (g *Graph) less(a, b int) bool {
	na, nb := g.Nodes[a], g.Nodes[b]
	if na.Root != nb.Root {
		return na.Root
	}
	if na.Name != nb.Name {
		return na.Name < nb.Name
	}
	return na.Version < nb.Version
}

// parents returns one incoming edge per distinct source (lowest Kind wins), sorted
// by less. With prod, root Dev edges and dev-only sources are ignored. The graph is
// immutable after Build, so each variant is computed once for all nodes.
func (g *Graph) parents(n int, prod bool) []Edge {
	i := 0
	if prod {
		i = 1
	}
	if g.parentCache[i] == nil {
		g.parentCache[i] = g.computeParents(prod)
	}
	return g.parentCache[i][n]
}

func (g *Graph) computeParents(prod bool) [][]Edge {
	var dev []bool
	if prod {
		dev = g.DevOnly()
	}
	all := make([][]Edge, len(g.Nodes))
	for n := range g.Nodes {
		var out []Edge
		for _, e := range g.In[n] {
			if prod && ((g.Nodes[e.From].Root && e.Kind == Dev) || dev[e.From]) {
				continue
			}
			dup := false
			for j := range out {
				if out[j].From == e.From {
					dup = true
					if e.Kind < out[j].Kind {
						out[j] = e
					}
				}
			}
			if !dup {
				out = append(out, e)
			}
		}
		sort.Slice(out, func(a, b int) bool { return g.less(out[a].From, out[b].From) })
		all[n] = out
	}
	return all
}

func (g *Graph) rootParents(n int, prod bool) []Edge {
	var out []Edge
	for _, e := range g.parents(n, prod) {
		if g.Nodes[e.From].Root {
			out = append(out, e)
		}
	}
	return out
}

type treeBuilder struct {
	g        *Graph
	opts     WhyOpts
	direct   bool
	limit    int // stop after this many root leaves; 0 = unlimited
	leaves   int
	expanded map[int]bool
	onPath   map[int]bool
}

func (b *treeBuilder) expand(tn *TreeNode, level int) {
	var edges []Edge
	if level == 0 && b.direct {
		edges = b.g.rootParents(tn.Node, b.opts.Prod)
	} else {
		edges = b.g.parents(tn.Node, b.opts.Prod)
	}
	if len(edges) == 0 {
		return
	}
	if b.opts.Depth > 0 && level >= b.opts.Depth {
		tn.Children = []*TreeNode{{Node: -1, Marker: "…"}}
		return
	}
	b.expanded[tn.Node] = true
	b.onPath[tn.Node] = true
	for _, e := range edges {
		if b.limit > 0 && b.leaves >= b.limit {
			break
		}
		c := &TreeNode{Node: e.From, Kind: e.Kind}
		switch {
		case b.g.Nodes[e.From].Root:
			b.leaves++
		case b.onPath[e.From]:
			c.Marker = "(cycle)"
		case b.expanded[e.From]:
			c.Marker = "(*)"
		default:
			b.expand(c, level+1)
		}
		tn.Children = append(tn.Children, c)
	}
	delete(b.onPath, tn.Node)
}

func (g *Graph) buildTree(t int, o WhyOpts, direct bool, limit int) (*TreeNode, int) {
	b := &treeBuilder{g: g, opts: o, direct: direct, limit: limit, expanded: map[int]bool{}, onPath: map[int]bool{}}
	root := &TreeNode{Node: t}
	b.expand(root, 0)
	return root, b.leaves
}

// Why explains every route from the roots to t (SPEC §6.1).
func (g *Graph) Why(t int, o WhyOpts) WhyResult {
	res := WhyResult{Target: t, DevOnly: g.DevOnly()[t]}
	res.Direct = len(g.rootParents(t, o.Prod)) > 0

	limit := pathCap
	if o.All {
		limit = 0
	}
	var shown int
	res.Tree, shown = g.buildTree(t, o, res.Direct, limit)
	if limit > 0 {
		// Count the leaves the cap hid; the tree is deduplicated, so this is linear.
		uncapped := o
		uncapped.All = true
		if _, total := g.buildTree(t, uncapped, res.Direct, 0); total > shown {
			res.MorePaths = strconv.Itoa(total - shown)
		}
	}
	res.Paths, res.Truncated = g.paths(t, o, res.Direct, limit)
	res.Leads = g.leads(t, o.Prod)
	return res
}

// paths lists simple root-to-t paths, root first. limit 0 means all of them.
func (g *Graph) paths(t int, o WhyOpts, direct bool, limit int) ([][]PathStep, bool) {
	var out [][]PathStep
	var truncated bool
	onPath := map[int]bool{t: true}
	visits := 0
	stack := []PathStep{{Node: t}} // target-first; reversed on emit
	var walk func(n int, first bool)
	walk = func(n int, first bool) {
		edges := g.parents(n, o.Prod)
		if first && direct {
			edges = g.rootParents(n, o.Prod)
		}
		for _, e := range edges {
			if truncated {
				return
			}
			if visits++; visits > pathVisitLimit {
				truncated = true
				return
			}
			if onPath[e.From] {
				continue
			}
			stack[len(stack)-1].Kind = e.Kind
			if g.Nodes[e.From].Root {
				if limit > 0 && len(out) == limit {
					truncated = true
					return
				}
				p := make([]PathStep, 0, len(stack)+1)
				p = append(p, PathStep{Node: e.From})
				for i := len(stack) - 1; i >= 0; i-- {
					p = append(p, stack[i])
				}
				out = append(out, p)
				continue
			}
			onPath[e.From] = true
			stack = append(stack, PathStep{Node: e.From})
			walk(e.From, false)
			stack = stack[:len(stack)-1]
			delete(onPath, e.From)
		}
	}
	walk(t, true)
	return out, truncated
}

// leads returns one entry per direct dependency that reaches t (itself included).
func (g *Graph) leads(t int, prod bool) []Lead {
	seen := map[int]bool{t: true}
	queue := []int{t}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range g.parents(n, prod) {
			if !seen[e.From] && !g.Nodes[e.From].Root {
				seen[e.From] = true
				queue = append(queue, e.From)
			}
		}
	}
	var out []Lead
	for v := range seen {
		for _, e := range g.rootParents(v, prod) {
			out = append(out, Lead{Root: e.From, Direct: v, Kind: e.Kind})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Root != out[j].Root {
			return g.less(out[i].Root, out[j].Root)
		}
		return g.less(out[i].Direct, out[j].Direct)
	})
	return out
}
