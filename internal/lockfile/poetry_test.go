package lockfile

import (
	"os"
	"path/filepath"
	"reflect"
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
	// PEP 621 extras are optional, not dev (Poetry has no dev concept there).
	assertKinds(t, rootKinds(t, g), map[string]graph.Kind{"requests": graph.Prod, "pytest": graph.Optional})
}

// rootKinds maps each direct dependency of the single root to its lowest edge kind.
func rootKinds(t *testing.T, g *graph.Graph) map[string]graph.Kind {
	t.Helper()
	if len(g.Roots) != 1 {
		t.Fatalf("roots = %v, want 1", g.Roots)
	}
	out := map[string]graph.Kind{}
	for _, e := range g.Out[g.Roots[0]] {
		if k, ok := out[g.Nodes[e.To].Name]; !ok || e.Kind < k {
			out[g.Nodes[e.To].Name] = e.Kind
		}
	}
	return out
}

func assertKinds(t *testing.T, got, want map[string]graph.Kind) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("root deps = %v, want %v", got, want)
	}
	for name, k := range want {
		if got[name] != k {
			t.Errorf("%s kind = %v, want %v", name, got[name], k)
		}
	}
}

// Real Poetry 2.x lockfile: PEP 621 deps and extras plus dev, lint and optional docs groups.
func TestParsePoetryGroups(t *testing.T) {
	want := map[string]graph.Kind{
		"six": graph.Prod, "typing-extensions": graph.Optional,
		"toml": graph.Dev, "idna": graph.Dev, "certifi": graph.Dev,
	}
	path := "../../testdata/edge/poetry-groups/poetry.lock"
	g, warns, err := ParsePoetry(path, readFixture(t, path))
	if err != nil || len(warns) != 0 {
		t.Fatalf("ParsePoetry: err=%v warns=%v", err, warns)
	}
	assertKinds(t, rootKinds(t, g), want)

	// Without pyproject.toml, the lockfile's per-package groups decide.
	g, warns, err = ParsePoetry(filepath.Join(t.TempDir(), "poetry.lock"), readFixture(t, path))
	if err != nil || len(warns) != 0 {
		t.Fatalf("ParsePoetry without pyproject: err=%v warns=%v", err, warns)
	}
	assertKinds(t, rootKinds(t, g), want)
}

// Real Poetry 1.8 lockfile (no category or groups): only pyproject.toml classifies.
func TestParsePoetryExtras(t *testing.T) {
	path := "../../testdata/edge/poetry-extras/poetry.lock"
	g, warns, err := ParsePoetry(path, readFixture(t, path))
	if err != nil || len(warns) != 0 {
		t.Fatalf("ParsePoetry: err=%v warns=%v", err, warns)
	}
	assertKinds(t, rootKinds(t, g), map[string]graph.Kind{
		"six": graph.Prod, "typing-extensions": graph.Optional, "toml": graph.Dev, "idna": graph.Dev,
	})
}

func TestParsePoetryMainGroupAndLegacyOptional(t *testing.T) {
	d := t.TempDir()
	// Multiple-constraint dependencies come as an inline array or an array of tables.
	pyproject := `
[tool.poetry.dependencies]
c = [{ version = "<2", python = "<3.10", optional = true }, { version = ">=2", python = ">=3.10", optional = true }]
[[tool.poetry.dependencies.d]]
version = "1.0"
optional = true
[tool.poetry.group.main.dependencies]
a = "*"
[tool.poetry.group.docs.dependencies]
b = "*"
`
	lock := `
[[package]]
name = "a"
version = "1.0"
[[package]]
name = "b"
version = "1.0"
[[package]]
name = "c"
version = "1.0"
[[package]]
name = "d"
version = "1.0"
`
	if err := os.WriteFile(filepath.Join(d, "pyproject.toml"), []byte(pyproject), 0o644); err != nil {
		t.Fatal(err)
	}
	g, _, err := ParsePoetry(filepath.Join(d, "poetry.lock"), []byte(lock))
	if err != nil {
		t.Fatal(err)
	}
	assertKinds(t, rootKinds(t, g), map[string]graph.Kind{
		"a": graph.Prod, "b": graph.Dev, "c": graph.Optional, "d": graph.Optional,
	})

	// Legacy category lockfile without pyproject.toml: optional main packages are extras.
	legacy := `
[[package]]
name = "x"
version = "1.0"
category = "main"
optional = true
[[package]]
name = "y"
version = "1.0"
category = "dev"
optional = false
`
	g, _, err = ParsePoetry(filepath.Join(t.TempDir(), "poetry.lock"), []byte(legacy))
	if err != nil {
		t.Fatal(err)
	}
	assertKinds(t, rootKinds(t, g), map[string]graph.Kind{"x": graph.Optional, "y": graph.Dev})
}

// Real Poetry 2.5 lockfile with PEP 735 [dependency-groups]: every group is
// dev, and {include-group = "lint"} pulls lint's requirements into dev.
func TestParsePoetryDependencyGroups(t *testing.T) {
	path := "../../testdata/edge/poetry-dependency-groups/poetry.lock"
	g, warns, err := ParsePoetry(path, readFixture(t, path))
	if err != nil || len(warns) != 0 {
		t.Fatalf("ParsePoetry: err=%v warns=%v", err, warns)
	}
	assertKinds(t, rootKinds(t, g), map[string]graph.Kind{
		"six": graph.Prod, "idna": graph.Optional, "certifi": graph.Dev, "toml": graph.Dev,
	})
}

// TestParsePoetryGroupIncludeCycleOrphanAndSelfExtra checks that an
// include-group cycle terminates, that a self-referencing extra ("proj[x]")
// is not reported as unresolved, and that a locked package no root reaches is
// warned about.
func TestParsePoetryGroupIncludeCycleOrphanAndSelfExtra(t *testing.T) {
	d := t.TempDir()
	pyproject := `
[project]
name = "Proj"
dependencies = []
[project.optional-dependencies]
x = ["idna"]
all = ["proj[x]"]
[dependency-groups]
a = ["toml ; python_version >= '3.9'", {include-group = "b"}]
b = [{include-group = "a"}, {include-group = "missing"}]
`
	lock := `
[[package]]
name = "idna"
version = "3.0"
[[package]]
name = "toml"
version = "0.10.2"
[[package]]
name = "orphan"
version = "1.0"
`
	if err := os.WriteFile(filepath.Join(d, "pyproject.toml"), []byte(pyproject), 0o644); err != nil {
		t.Fatal(err)
	}
	g, warns, err := ParsePoetry(filepath.Join(d, "poetry.lock"), []byte(lock))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"orphan@1.0 is not reachable from any root"}; !reflect.DeepEqual(warns, want) {
		t.Fatalf("warns = %v, want %v", warns, want)
	}
	assertKinds(t, rootKinds(t, g), map[string]graph.Kind{"idna": graph.Optional, "toml": graph.Dev})
}
