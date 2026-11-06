package lockfile

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

// TestParseGoModReplace checks that a replace directive never renames a
// dependency: the node keeps the required module path. A version-specific
// replace applies only to that version and beats a path-wide one, and only a
// same-path replacement (a version pin) changes the node's version.
func TestParseGoModReplace(t *testing.T) {
	data := []byte(`
module test/mod

go 1.23

require (
	example.com/local v1.0.0
	example.com/old v1.0.0
	example.com/pin v1.0.0 // indirect
	example.com/wide v1.0.0
)

replace example.com/local v1.0.0 => ./local

replace example.com/old => example.com/new v1.2.0

replace example.com/pin v1.0.0 => example.com/pin v1.0.1

replace example.com/pin v0.9.0 => example.com/pin v0.9.9

replace example.com/wide v1.0.0 => example.com/wide v1.4.0

replace example.com/wide => example.com/wide v1.5.0
`)
	g, warns, err := ParseGoMod("go.mod", data, false)
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	want := []string{
		"root:test/mod -> example.com/local@v1.0.0 prod",
		"root:test/mod -> example.com/old@v1.0.0 prod",
		"root:test/mod -> example.com/pin@v1.0.1 indirect",
		"root:test/mod -> example.com/wide@v1.4.0 prod",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
	if len(g.Find("example.com/local", "")) != 1 {
		t.Fatal("a locally replaced module must be found by its module path")
	}
}

func TestParseGoModInvalid(t *testing.T) {
	data := []byte(`invalid go mod content {{{`)
	_, _, err := ParseGoMod("go.mod", data, false)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestParseGoModWithGraph(t *testing.T) {
	path := "../../go.mod"
	data := readFixture(t, path)
	g, _, err := ParseGoMod(path, data, true)
	if err != nil {
		t.Fatalf("ParseGoMod with graph: %v", err)
	}
	if len(g.Roots) != 1 || g.Nodes[g.Roots[0]].Name != "github.com/dpologdvinity/depwhy" {
		t.Fatalf("roots = %v", g.Roots)
	}
	if len(g.Nodes) == 0 {
		t.Fatal("expected nodes")
	}
}

// fakeGoModGraph makes ParseGoMod see out as the output of `go mod graph`
// (or err as its failure) for the rest of the test.
func fakeGoModGraph(t *testing.T, out string, err error) {
	t.Helper()
	orig := goModGraph
	goModGraph = func(string) ([]byte, error) { return []byte(out), err }
	t.Cleanup(func() { goModGraph = orig })
}

// TestParseGoModGraphSelected checks that --go-graph keeps only the versions MVS
// selects: go@ and toolchain@ pseudo-entries are dropped, requirements of
// unselected versions are dropped, edges to unselected versions point at the
// selected one, and modules that only unselected versions need are left out
// rather than kept as orphans.
func TestParseGoModGraphSelected(t *testing.T) {
	fakeGoModGraph(t, `example.com/demo example.com/b@v1.0.0
example.com/demo example.com/c@v1.1.0
example.com/demo go@1.22
example.com/demo toolchain@go1.22.0
example.com/b@v1.0.0 example.com/c@v1.0.0
example.com/b@v1.0.0 go@1.22
example.com/c@v1.0.0 example.com/d@v1.0.0
example.com/d@v1.0.0 example.com/e@v1.0.0
example.com/c@v1.1.0 go@1.22
go@1.22 toolchain@go1.22
malformed line here
`, nil)
	data := []byte(`module example.com/demo

go 1.22

require (
	example.com/b v1.0.0
	example.com/c v1.1.0 // indirect
)

replace example.com/b v1.0.0 => ./b
`)
	g, warns, err := ParseGoMod("go.mod", data, true)
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	want := []string{
		"example.com/b@v1.0.0 -> example.com/c@v1.1.0 prod",
		"root:example.com/demo -> example.com/b@v1.0.0 prod",
		"root:example.com/demo -> example.com/c@v1.1.0 indirect",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("nodes = %v", g.Nodes)
	}
	if d := g.Dupes(false); len(d) != 0 {
		t.Fatalf("dupes = %v", d)
	}
}

// TestParseGoModGraphFails checks the fallback to go.mod's own requirements,
// with a warning, when `go mod graph` fails.
func TestParseGoModGraphFails(t *testing.T) {
	fakeGoModGraph(t, "", errors.New("boom"))
	data := []byte("module m\n\nrequire example.com/x v1.0.0\n")
	g, warns, err := ParseGoMod("go.mod", data, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "go mod graph failed: boom") {
		t.Fatalf("warns = %v", warns)
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, []string{"root:m -> example.com/x@v1.0.0 prod"}) {
		t.Fatalf("edges = %v", got)
	}
}

// requireTool skips the test when name is not on PATH.
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH", name)
	}
}

// TestParseGoModGraphRealGo runs the real `go mod graph` on a fixture whose
// dependencies are all local replacements, so it needs no network, and checks
// the same selection rules and that go.mod is left byte-identical.
func TestParseGoModGraphRealGo(t *testing.T) {
	requireTool(t, "go")
	path := "../../testdata/edge/gomod-graph/go.mod"
	data := readFixture(t, path)
	g, warns, err := ParseGoMod(path, data, true)
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	want := []string{
		"example.com/b@v1.0.0 -> example.com/c@v1.1.0 prod",
		"root:example.com/demo -> example.com/b@v1.0.0 prod",
		"root:example.com/demo -> example.com/c@v1.1.0 indirect",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
	if !bytes.Equal(readFixture(t, path), data) {
		t.Fatal("go mod graph modified go.mod")
	}
}

// TestParseGoModGraphOffline checks that `go mod graph` runs with the proxy off:
// a requirement missing from the module cache makes it fail instead of
// downloading, and depwhy falls back to go.mod with a warning.
func TestParseGoModGraphOffline(t *testing.T) {
	requireTool(t, "go")
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	data := []byte("module m\n\ngo 1.22\n\nrequire depwhy.invalid/x v1.0.0\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	g, warns, err := ParseGoMod(path, data, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "go mod graph failed") {
		t.Fatalf("warns = %v", warns)
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, []string{"root:m -> depwhy.invalid/x@v1.0.0 prod"}) {
		t.Fatalf("edges = %v", got)
	}
	if !bytes.Equal(readFixture(t, path), data) {
		t.Fatal("go mod graph modified go.mod")
	}
}

// TestRunToolTimeout checks that an external tool is stopped after toolTimeout.
func TestRunToolTimeout(t *testing.T) {
	requireTool(t, "go")
	orig := toolTimeout
	toolTimeout = time.Nanosecond
	t.Cleanup(func() { toolTimeout = orig })
	if _, err := runTool("", nil, "go", "version"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}
