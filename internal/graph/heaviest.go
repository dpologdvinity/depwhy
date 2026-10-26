package graph

import "sort"

// HeaviestItem is a direct dependency ranked by the disk size freed if removed.
type HeaviestItem struct {
	Target     int   // direct dependency node index
	FreedSize  int64 // total bytes freed
	FreedCount int   // count of freed packages
}

// HeaviestOpts controls Heaviest ranking.
type HeaviestOpts struct {
	Prod  bool                    // ignore dev dependencies
	Limit int                     // maximum items to return (0 = unlimited)
	Size  func(int) (int64, bool) // returns size in bytes for a node
}

// Heaviest ranks direct dependencies by exclusive transitive freed size.
func (g *Graph) Heaviest(opts HeaviestOpts) []HeaviestItem {
	if opts.Size == nil {
		return nil
	}
	// Direct dependencies: non-root nodes that have an edge from a root.
	directSet := map[int]bool{}
	for from, edges := range g.Out {
		if !g.Nodes[from].Root {
			continue
		}
		for _, e := range edges {
			if opts.Prod && e.Kind == Dev {
				continue
			}
			if !g.Nodes[e.To].Root {
				directSet[e.To] = true
			}
		}
	}

	var items []HeaviestItem
	for d := range directSet {
		rem := g.Remove(d, opts.Prod)
		if !rem.ComputeFreedSize(opts.Size) {
			continue
		}
		items = append(items, HeaviestItem{
			Target:     d,
			FreedSize:  *rem.FreedSize,
			FreedCount: len(rem.Freed),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].FreedSize != items[j].FreedSize {
			return items[i].FreedSize > items[j].FreedSize
		}
		ni, nj := g.Nodes[items[i].Target], g.Nodes[items[j].Target]
		if ni.Name != nj.Name {
			return ni.Name < nj.Name
		}
		return ni.Version < nj.Version
	})

	if opts.Limit > 0 && len(items) > opts.Limit {
		items = items[:opts.Limit]
	}
	return items
}
