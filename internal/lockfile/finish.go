package lockfile

import (
	"fmt"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// finish builds the graph from b and adds a warning for each package that no
// root can reach. Such orphans do occur in real lockfiles; they are kept in the
// graph but cannot appear in any result. warns holds the parser's own warnings.
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
