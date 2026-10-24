package graph

import (
	"sort"
	"strings"
)

// SplitQuery splits "name@version" at the last '@' with index > 0.
func SplitQuery(q string) (name, version string) {
	if i := strings.LastIndex(q, "@"); i > 0 {
		return q[:i], q[i+1:]
	}
	return q, ""
}

// Normalize canonicalizes a package name for matching (SPEC §5.5).
func Normalize(ecosystem, name string) string {
	switch ecosystem {
	case "python":
		var sb strings.Builder
		sep := false
		for _, r := range strings.ToLower(name) {
			if r == '-' || r == '_' || r == '.' {
				if !sep {
					sb.WriteByte('-')
				}
				sep = true
				continue
			}
			sep = false
			sb.WriteRune(r)
		}
		return sb.String()
	case "cargo":
		return strings.ReplaceAll(strings.ToLower(name), "_", "-")
	}
	return name
}

// Find returns non-root nodes matching name (normalized) and, if given, version, sorted by Version.
func (g *Graph) Find(name, version string) []int {
	want := Normalize(g.Ecosystem, name)
	var out []int
	for i, n := range g.Nodes {
		if n.Root || Normalize(g.Ecosystem, n.Name) != want || (version != "" && n.Version != version) {
			continue
		}
		out = append(out, i)
	}
	sort.SliceStable(out, func(a, b int) bool { return g.Nodes[out[a]].Version < g.Nodes[out[b]].Version })
	return out
}

// Suggest returns up to 3 normalized names within edit distance 2 of name, closest first.
func Suggest(gs []*Graph, name string) []string {
	type cand struct {
		name string
		dist int
	}
	seen := map[string]bool{}
	var cands []cand
	for _, g := range gs {
		q := Normalize(g.Ecosystem, name)
		for _, n := range g.Nodes {
			if n.Root {
				continue
			}
			norm := Normalize(g.Ecosystem, n.Name)
			if seen[norm] {
				continue
			}
			seen[norm] = true
			if d := levenshtein(q, norm); d <= 2 {
				cands = append(cands, cand{norm, d})
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].name < cands[j].name
	})
	var out []string
	for _, c := range cands {
		if len(out) == 3 {
			break
		}
		out = append(out, c.name)
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
