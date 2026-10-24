package lockfile

import (
	"fmt"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// finish builds the graph and warns about packages no root reaches.
func finish(b *graph.Builder, warns []string) (*graph.Graph, []string, error) {
	g, err := b.Build()
	if err != nil {
		return nil, nil, err
	}
	seen := make([]bool, len(g.Nodes))
	stack := append([]int(nil), g.Roots...)
	for _, r := range g.Roots {
		seen[r] = true
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range g.Out[n] {
			if !seen[e.To] {
				seen[e.To] = true
				stack = append(stack, e.To)
			}
		}
	}
	for i, ok := range seen {
		if !ok {
			warns = append(warns, fmt.Sprintf("%s is not reachable from any root", g.Nodes[i].ID))
		}
	}
	return g, warns, nil
}
