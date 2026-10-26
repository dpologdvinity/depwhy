package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

func TestParsePoetry(t *testing.T) {
	path := "../../testdata/edge/poetry/poetry.lock"
	g, warns, err := ParsePoetry(path, readFixture(t, path))
	if err != nil {
		t.Fatalf("ParsePoetry: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if g.Ecosystem != "python" {
		t.Errorf("ecosystem = %s, want python", g.Ecosystem)
	}

	roots := g.Roots
	if len(roots) != 1 || g.Nodes[roots[0]].Name != "my-poetry-app" {
		t.Fatalf("roots = %v, want [my-poetry-app]", roots)
	}

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

	reqNodes := g.Find("requests", "")
	if len(reqNodes) != 1 {
		t.Fatalf("find requests = %v", reqNodes)
	}
	var reqDeps []string
	for _, e := range g.Out[reqNodes[0]] {
		reqDeps = append(reqDeps, g.Nodes[e.To].Name)
	}
	if !strings.Contains(strings.Join(reqDeps, ","), "certifi") || !strings.Contains(strings.Join(reqDeps, ","), "idna") {
		t.Errorf("reqDeps = %v, want certifi and idna", reqDeps)
	}
}

func TestParsePoetryNoPyproject(t *testing.T) {
	data := []byte(`
[[package]]
name = "pkg-a"
version = "1.0.0"
category = "main"

[package.dependencies]
pkg-b = ">=1.0.0"

[[package]]
name = "pkg-b"
version = "1.0.0"
category = "main"

[[package]]
name = "dev-tool"
version = "2.0.0"
category = "dev"
`)
	g, warns, err := ParsePoetry("test-no-pyproject/poetry.lock", data)
	if err != nil {
		t.Fatalf("ParsePoetry: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(g.Roots) != 1 {
		t.Fatalf("roots count = %d, want 1", len(g.Roots))
	}
	root := g.Roots[0]
	var outKinds = map[string]graph.Kind{}
	for _, e := range g.Out[root] {
		outKinds[g.Nodes[e.To].Name] = e.Kind
	}
	if outKinds["pkg-a"] != graph.Prod {
		t.Errorf("pkg-a kind = %v, want Prod", outKinds["pkg-a"])
	}
	if outKinds["dev-tool"] != graph.Dev {
		t.Errorf("dev-tool kind = %v, want Dev", outKinds["dev-tool"])
	}
	if _, ok := outKinds["pkg-b"]; ok {
		t.Errorf("pkg-b should be transitive, not direct from root")
	}
}

func TestParsePoetryUnresolved(t *testing.T) {
	data := []byte(`
[[package]]
name = "foo"
version = "1.0.0"
category = "main"

[package.dependencies]
missing = ">=1.0.0"
`)
	g, warns, err := ParsePoetry("poetry.lock", data)
	if err != nil {
		t.Fatalf("ParsePoetry: %v", err)
	}
	if len(g.Roots) != 1 {
		t.Fatalf("roots = %v", g.Roots)
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "unresolved dependency missing") {
		t.Fatalf("expected unresolved warning, got %v", warns)
	}
}

func TestParsePoetryPEP621(t *testing.T) {
	d := t.TempDir()
	pyprojectContent := `
[project]
name = "pep621-app"
version = "0.2.0"
dependencies = [
    "requests >= 2.0.0",
    "python >= 3.8",
]

[project.optional-dependencies]
dev = [
    "pytest",
]
`
	lockContent := `
[[package]]
name = "requests"
version = "2.28.0"
category = "main"

[[package]]
name = "pytest"
version = "7.0.0"
category = "dev"
`
	if err := os.WriteFile(filepath.Join(d, "pyproject.toml"), []byte(pyprojectContent), 0644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(d, "poetry.lock")
	g, warns, err := ParsePoetry(lockPath, []byte(lockContent))
	if err != nil || len(warns) != 0 {
		t.Fatalf("ParsePoetry PEP621: err=%v warns=%v", err, warns)
	}
	if len(g.Roots) != 1 || g.Nodes[g.Roots[0]].Name != "pep621-app" {
		t.Fatalf("roots = %v", g.Roots)
	}
}
