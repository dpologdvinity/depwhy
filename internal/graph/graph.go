// Package graph is depwhy's ecosystem-independent dependency model and the
// queries that run on it.
//
// Lockfile parsers (package lockfile) build a Graph through a Builder. The
// algorithms in this package answer the three user questions: Why explains
// every route from the project to a package, Remove reports what dropping a
// direct dependency would free, and Dupes lists packages present at more than
// one version. The package imports only the standard library.
package graph

import "errors"

// Kind says why an edge exists, taken from the field that declared it.
type Kind uint8

// The edge kinds. Prod is the zero value, so an unset Kind means a regular
// dependency. The order matters: when two edges join the same pair of nodes,
// the lower Kind is the one shown.
const (
	Prod     Kind = iota // regular dependency
	Dev                  // devDependencies / dev groups
	Optional             // optionalDependencies / extras
	Peer                 // npm peerDependencies
)

// String returns the lowercase kind name used in text and JSON output.
func (k Kind) String() string {
	return [...]string{"prod", "dev", "optional", "peer"}[k]
}

// Node is one package in the graph, or a root (the project or a workspace
// member). Display names come straight from the lockfile.
type Node struct {
	ID        string // "<name>@<version>", or "root:<label>" for roots; unique per graph
	Name      string // package name, or the label for a root
	Version   string // "" for roots that have none
	Ecosystem string // "npm" | "python" | "cargo"
	Root      bool   // true for the project and workspace members
}

// Edge is a dependency: node From requires node To. Both are indexes into
// Graph.Nodes.
type Edge struct {
	From, To int
	Kind     Kind
}

// Graph is the dependency graph of one lockfile. Treat it as read-only once
// Build has returned it; query results are cached on it. It is not safe for
// concurrent use.
//
// Invariants kept by Builder: node IDs are unique, In mirrors Out exactly,
// there are no duplicate (From, To, Kind) edges, and Roots is not empty.
type Graph struct {
	Ecosystem string   // "npm" | "python" | "cargo"
	Lockfile  string   // lockfile path as given or detected
	Nodes     []Node   // all nodes; an index into this slice identifies a node
	Out       [][]Edge // Out[i] lists the edges leaving node i
	In        [][]Edge // In[i] lists the edges entering node i
	Roots     []int    // indexes of root nodes, in lockfile order

	devOnly     []bool      // cache for DevOnly
	parentCache [2][][]Edge // cache for parents; index 1 is the prod variant
}

// Builder assembles a Graph while keeping its invariants, so parsers never
// touch the slices directly. Node, Root and Edge are idempotent: repeating a
// call returns the same node or adds nothing.
type Builder struct {
	g     *Graph
	ids   map[string]int
	edges map[Edge]bool
}

// NewBuilder starts an empty graph for the given ecosystem and lockfile path.
func NewBuilder(ecosystem, lockfile string) *Builder {
	return &Builder{
		g:     &Graph{Ecosystem: ecosystem, Lockfile: lockfile},
		ids:   map[string]int{},
		edges: map[Edge]bool{},
	}
}

// add returns the node with the given ID, creating it on first use.
func (b *Builder) add(id, name, version string, root bool) int {
	if i, ok := b.ids[id]; ok {
		return i
	}
	i := len(b.g.Nodes)
	b.ids[id] = i
	b.g.Nodes = append(b.g.Nodes, Node{ID: id, Name: name, Version: version, Ecosystem: b.g.Ecosystem, Root: root})
	b.g.Out = append(b.g.Out, nil)
	b.g.In = append(b.g.In, nil)
	if root {
		b.g.Roots = append(b.g.Roots, i)
	}
	return i
}

// Node gets or creates a package node.
func (b *Builder) Node(name, version string) int {
	return b.add(name+"@"+version, name, version, false)
}

// Root gets or creates a project/workspace-member node.
func (b *Builder) Root(label, version string) int {
	return b.add("root:"+label, label, version, true)
}

// Edge adds from→to, ignoring exact duplicates.
func (b *Builder) Edge(from, to int, k Kind) {
	e := Edge{from, to, k}
	if b.edges[e] {
		return
	}
	b.edges[e] = true
	b.g.Out[from] = append(b.g.Out[from], e)
	b.g.In[to] = append(b.g.In[to], e)
}

// Build returns the finished graph, or an error if no root was ever added.
func (b *Builder) Build() (*Graph, error) {
	if len(b.g.Roots) == 0 {
		return nil, errors.New("no root packages found")
	}
	return b.g, nil
}
