// Package render turns graph query results into output: the text trees and
// summaries of SPEC §7.1 and §7.2, and the JSON document of §7.3. It reads
// results but never computes them, so every decision about what to show lives
// in package graph.
package render

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// Pseudo node indexes used in a printed tree for lines that are not packages.
const (
	nodeEllipsis = -1
	nodeMore     = -2
)

// pkgID formats a node as name@version, or just the name when it has no version.
func pkgID(g *graph.Graph, n int) string {
	if v := g.Nodes[n].Version; v != "" {
		return g.Nodes[n].Name + "@" + v
	}
	return g.Nodes[n].Name
}

// source formats the "ecosystem · lockfile" suffix shown on headings.
func source(g *graph.Graph) string {
	return g.Ecosystem + " · " + filepath.Base(g.Lockfile)
}

// plural formats a count with its noun, adding an "s" unless n is 1.
func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// leadText formats leads as "name (kind), ...", sorted by the direct dependency's name.
func leadText(g *graph.Graph, leads []graph.Lead, s Style, label func(graph.Lead) int) string {
	ls := append([]graph.Lead(nil), leads...)
	sort.SliceStable(ls, func(i, j int) bool { return g.Nodes[label(ls[i])].Name < g.Nodes[label(ls[j])].Name })
	parts := make([]string, len(ls))
	for i, l := range ls {
		parts[i] = fmt.Sprintf("%s (%s)", g.Nodes[label(l)].Name, s.kind(l.Kind))
	}
	return strings.Join(parts, ", ")
}

// directOf selects the direct dependency of a lead, for leadText.
func directOf(l graph.Lead) int { return l.Direct }

// rootOf selects the root of a lead, for leadText.
func rootOf(l graph.Lead) int { return l.Root }

// WhyText prints one reverse tree, with a footer summarizing the direct dependencies.
func WhyText(w io.Writer, g *graph.Graph, r graph.WhyResult, s Style) {
	fmt.Fprintln(w, s.Bold(fmt.Sprintf("%s  %s", pkgID(g, r.Target), source(g))))
	kids := append([]*graph.TreeNode(nil), r.Tree.Children...)
	if r.MorePaths != "" {
		kids = append(kids, &graph.TreeNode{Node: nodeMore, Marker: fmt.Sprintf("… %s more paths (use --all)", r.MorePaths)})
	}
	printTree(w, g, kids, "", s)

	if r.Direct {
		var own []graph.Lead
		for _, l := range r.Leads {
			if l.Direct == r.Target {
				own = append(own, l)
			}
		}
		fmt.Fprintf(w, "direct dependency of %s\n", leadText(g, own, s, rootOf))
	} else if len(r.Leads) >= 2 {
		fmt.Fprintf(w, "pulled in by %d direct dependencies: %s\n", len(r.Leads), leadText(g, r.Leads, s, directOf))
	}
}

// printTree prints kids and their descendants with box-drawing connectors.
// prefix is the indentation inherited from the ancestors.
func printTree(w io.Writer, g *graph.Graph, kids []*graph.TreeNode, prefix string, s Style) {
	for i, k := range kids {
		conn, next := "├── ", prefix+"│   "
		if i == len(kids)-1 {
			conn, next = "└── ", prefix+"    "
		}
		fmt.Fprintln(w, prefix+conn+treeLabel(g, k, s))
		printTree(w, g, k.Children, next, s)
	}
}

// treeLabel returns the text of one tree line: a package, a root leaf with its
// edge kind, a marker, or one of the pseudo lines.
func treeLabel(g *graph.Graph, k *graph.TreeNode, s Style) string {
	switch {
	case k.Node == nodeEllipsis:
		return "…"
	case k.Node == nodeMore:
		return k.Marker
	case g.Nodes[k.Node].Root:
		return fmt.Sprintf("%s (root, %s)", s.Cyan(g.Nodes[k.Node].Name), s.kind(k.Kind))
	case k.Marker != "":
		return pkgID(g, k.Node) + " " + s.Dim(k.Marker)
	}
	return pkgID(g, k.Node)
}

// RemoveText prints the impact of removing a direct dependency.
func RemoveText(w io.Writer, g *graph.Graph, r graph.RemoveResult, s Style) {
	t := pkgID(g, r.Target)
	if r.NotDirect {
		if len(r.RequiredBy) == 0 {
			fmt.Fprintf(w, "%s is not a direct dependency.\n", t)
			fmt.Fprintf(w, "run `depwhy %s` for details.\n", g.Nodes[r.Target].Name)
			return
		}
		fmt.Fprintf(w, "%s is not a direct dependency. it is required by: %s\n", t, leadText(g, r.RequiredBy, s, directOf))
		fmt.Fprintf(w, "remove those instead, or run `depwhy %s` for details.\n", g.Nodes[r.Target].Name)
		return
	}
	fmt.Fprintln(w, s.Bold(fmt.Sprintf("removing %s (direct, %s) from %s  %s", t, s.kind(r.Kind), g.Nodes[r.Root].Name, source(g))))
	if len(r.Freed) == 0 {
		fmt.Fprintln(w, "would remove 0 packages")
	} else {
		fmt.Fprintf(w, "would remove %s:\n", plural(len(r.Freed), "package"))
		for _, n := range r.Freed {
			fmt.Fprintf(w, "  %s\n", pkgID(g, n))
		}
	}
	if len(r.Kept) > 0 {
		fmt.Fprintf(w, "still needed by others (%d):\n", len(r.Kept))
		for _, k := range r.Kept {
			if k.Via == k.Node {
				fmt.Fprintf(w, "  %s  (also a direct dependency)\n", pkgID(g, k.Node))
			} else {
				fmt.Fprintf(w, "  %s  via %s\n", pkgID(g, k.Node), pkgID(g, k.Via))
			}
		}
	}
	if r.StillNeeded {
		fmt.Fprintf(w, "%s itself stays: also required via %s\n", t, pkgID(g, r.StillVia))
	}
}

// DupesText prints packages installed at more than one version.
func DupesText(w io.Writer, g *graph.Graph, ds []graph.Dupe, s Style) {
	fmt.Fprintln(w, s.Bold(source(g)))
	if len(ds) == 0 {
		fmt.Fprintln(w, "no duplicate packages")
		return
	}
	for _, d := range ds {
		fmt.Fprintf(w, "%s  %s\n", d.Name, plural(len(d.Versions), "version"))
		for _, v := range d.Versions {
			fmt.Fprintf(w, "  %s  via %s\n", g.Nodes[v.Node].Version, leadText(g, v.Leads, s, directOf))
		}
	}
}
