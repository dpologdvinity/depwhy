package lockfile

import (
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

func TestParseGoModDirect(t *testing.T) {
	path := "../../testdata/edge/gomod/go.mod"
	g, warns, err := ParseGoMod(path, readFixture(t, path), false)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if g.Ecosystem != "go" {
		t.Errorf("ecosystem = %s, want go", g.Ecosystem)
	}

	roots := g.Roots
	if len(roots) != 1 || g.Nodes[roots[0]].Name != "example.com/demo" {
		t.Fatalf("roots = %v, want [example.com/demo]", roots)
	}

	var rootDeps = map[string]graph.Kind{}
	for _, e := range g.Out[roots[0]] {
		rootDeps[g.Nodes[e.To].Name] = e.Kind
	}

	if len(rootDeps) != 2 {
		t.Fatalf("rootDeps = %v, want 2 edges", rootDeps)
	}
	if rootDeps["github.com/pkg/errors"] != graph.Prod {
		t.Errorf("pkg/errors kind = %v, want Prod", rootDeps["github.com/pkg/errors"])
	}
	if rootDeps["golang.org/x/sync"] != graph.Indirect {
		t.Errorf("x/sync kind = %v, want Indirect", rootDeps["golang.org/x/sync"])
	}
}

func TestParseGoModReplace(t *testing.T) {
	data := []byte(`
module test/mod

go 1.23

require (
	example.com/old v1.0.0
)

replace example.com/old => example.com/new v1.2.0
`)
	g, warns, err := ParseGoMod("go.mod", data, false)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(g.Roots) != 1 {
		t.Fatalf("roots = %v", g.Roots)
	}
	root := g.Roots[0]
	if len(g.Out[root]) != 1 {
		t.Fatalf("out edges = %d, want 1", len(g.Out[root]))
	}
	targetNode := g.Nodes[g.Out[root][0].To]
	if targetNode.Name != "example.com/new" || targetNode.Version != "v1.2.0" {
		t.Errorf("replaced target = %s@%s, want example.com/new@v1.2.0", targetNode.Name, targetNode.Version)
	}
}

func TestParseGoModInvalid(t *testing.T) {
	data := []byte(`invalid go mod content {{{`)
	_, _, err := ParseGoMod("go.mod", data, false)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}
