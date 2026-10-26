package lockfile

import (
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

func TestParseYarnBerry(t *testing.T) {
	path := "../../testdata/edge/yarn-berry/yarn.lock"
	g, warns, err := ParseYarn(path, readFixture(t, path))
	if err != nil {
		t.Fatalf("ParseYarn: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if g.Ecosystem != "npm" {
		t.Errorf("ecosystem = %s, want npm", g.Ecosystem)
	}

	// Verify roots
	roots := g.Roots
	if len(roots) != 1 || g.Nodes[roots[0]].Name != "app" {
		t.Fatalf("roots = %v, want [app]", roots)
	}

	// Verify target nodes exist
	aNodes := g.Find("a", "")
	if len(aNodes) != 1 || g.Nodes[aNodes[0]].Version != "1.0.0" {
		t.Fatalf("find a = %v", aNodes)
	}

	// a -> c (prod), a -> d (prod)
	var aDeps []string
	for _, e := range g.Out[aNodes[0]] {
		aDeps = append(aDeps, g.Nodes[e.To].Name)
	}
	if !strings.Contains(strings.Join(aDeps, ","), "c") || !strings.Contains(strings.Join(aDeps, ","), "d") {
		t.Errorf("aDeps = %v, want c and d", aDeps)
	}
}

func TestParseYarnClassic(t *testing.T) {
	path := "../../testdata/edge/yarn-classic/yarn.lock"
	g, warns, err := ParseYarn(path, readFixture(t, path))
	if err != nil {
		t.Fatalf("ParseYarn: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}

	// Root from package.json: app
	roots := g.Roots
	if len(roots) != 1 || g.Nodes[roots[0]].Name != "app" {
		t.Fatalf("roots = %v, want [app]", roots)
	}

	// Direct dependencies: a (prod), b (dev)
	var rootEdges []struct {
		name string
		kind graph.Kind
	}
	for _, e := range g.Out[roots[0]] {
		rootEdges = append(rootEdges, struct {
			name string
			kind graph.Kind
		}{g.Nodes[e.To].Name, e.Kind})
	}
	if len(rootEdges) != 2 {
		t.Fatalf("rootEdges = %v, want 2 edges (a and b)", rootEdges)
	}
}

func TestParseYarnUnresolved(t *testing.T) {
	data := []byte(`# yarn lockfile v1

"a@^1.0.0":
  version "1.0.0"
  dependencies:
    missing "^1.0.0"
`)
	_, warns, err := ParseYarn("yarn.lock", data)
	if err != nil {
		t.Fatalf("ParseYarn: %v", err)
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "unresolved dependency missing of a") {
		t.Fatalf("expected unresolved warning, got %v", warns)
	}
}
