// Package graph holds the dependency graph model and the queries over it.
package graph

import "errors"

type Kind uint8

const (
	Prod     Kind = iota // regular dependency
	Dev                  // devDependencies / dev groups
	Optional             // optionalDependencies / extras
	Peer                 // npm peerDependencies
)

func (k Kind) String() string {
	return [...]string{"prod", "dev", "optional", "peer"}[k]
}

type Node struct {
	ID        string // "<name>@<version>", or "root:<label>" for roots
	Name      string
	Version   string
	Ecosystem string // "npm" | "python" | "cargo"
	Root      bool
}

type Edge struct {
	From, To int
	Kind     Kind
}

type Graph struct {
	Ecosystem string
	Lockfile  string
	Nodes     []Node
	Out       [][]Edge
	In        [][]Edge
	Roots     []int

	devOnly []bool
}

// Builder assembles a Graph while keeping its invariants.
type Builder struct {
	g     *Graph
	ids   map[string]int
	edges map[Edge]bool
}

func NewBuilder(ecosystem, lockfile string) *Builder {
	return &Builder{
		g:     &Graph{Ecosystem: ecosystem, Lockfile: lockfile},
		ids:   map[string]int{},
		edges: map[Edge]bool{},
	}
}

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

func (b *Builder) Build() (*Graph, error) {
	if len(b.g.Roots) == 0 {
		return nil, errors.New("no root packages found")
	}
	return b.g, nil
}
