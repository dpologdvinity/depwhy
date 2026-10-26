package lockfile

import (
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

func TestParseBun(t *testing.T) {
	path := "../../testdata/edge/bun/bun.lock"
	g, warns, err := ParseBun(path, readFixture(t, path))
	if err != nil {
		t.Fatalf("ParseBun: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if g.Ecosystem != "npm" {
		t.Errorf("ecosystem = %s, want npm", g.Ecosystem)
	}

	roots := g.Roots
	if len(roots) != 1 || g.Nodes[roots[0]].Name != "app" {
		t.Fatalf("roots = %v, want [app]", roots)
	}

	// Verify edges from root: a (prod), b (dev)
	var rootDeps []struct {
		name string
		kind graph.Kind
	}
	for _, e := range g.Out[roots[0]] {
		rootDeps = append(rootDeps, struct {
			name string
			kind graph.Kind
		}{g.Nodes[e.To].Name, e.Kind})
	}
	if len(rootDeps) != 2 {
		t.Fatalf("rootDeps = %v, want 2 edges", rootDeps)
	}

	// Find a and check out edges
	aNodes := g.Find("a", "")
	if len(aNodes) != 1 {
		t.Fatalf("find a = %v", aNodes)
	}
	var aDeps []string
	for _, e := range g.Out[aNodes[0]] {
		aDeps = append(aDeps, g.Nodes[e.To].Name)
	}
	if !strings.Contains(strings.Join(aDeps, ","), "c") || !strings.Contains(strings.Join(aDeps, ","), "d") {
		t.Errorf("aDeps = %v, want c and d", aDeps)
	}
}

func TestParseBunCommentsAndUnresolved(t *testing.T) {
	data := []byte(`// Bun lockfile v1
{
  /* comment */
  "lockfileVersion": 1,
  "workspaces": {
    "": {
      "dependencies": {
        "missing": "^1.0.0"
      }
    }
  },
  "packages": {}
}
`)
	g, warns, err := ParseBun("bun.lock", data)
	if err != nil {
		t.Fatalf("ParseBun with comments: %v", err)
	}
	if len(g.Roots) != 1 {
		t.Fatalf("roots = %v, want 1", g.Roots)
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "unresolved dependency missing") {
		t.Fatalf("expected unresolved warning, got %v", warns)
	}
}
