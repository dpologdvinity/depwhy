package lockfile

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// edgeStrings renders every edge as "from -> to kind", sorted.
func edgeStrings(g *graph.Graph) []string {
	var out []string
	for _, es := range g.Out {
		for _, e := range es {
			out = append(out, g.Nodes[e.From].ID+" -> "+g.Nodes[e.To].ID+" "+e.Kind.String())
		}
	}
	sort.Strings(out)
	return out
}

// rootIDs lists the IDs of the graph's roots in order.
func rootIDs(g *graph.Graph) []string {
	var out []string
	for _, r := range g.Roots {
		out = append(out, g.Nodes[r].ID)
	}
	return out
}

// readFixture reads a fixture file or fails the test.
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNPMResolve checks Node-style resolution: nested installs, hoisted installs,
// scoped names, walking up from a scoped parent, and following workspace links.
func TestNPMResolve(t *testing.T) {
	pkgs := map[string]npmEntry{
		"":                                 {},
		"node_modules/a":                   {},
		"node_modules/b":                   {},
		"node_modules/b/node_modules/c":    {},
		"node_modules/c":                   {},
		"node_modules/@x/y":                {},
		"node_modules/@x/y/node_modules/z": {},
		"node_modules/lib":                 {Link: true, Resolved: "packages/lib"},
		"packages/lib":                     {},
	}
	cases := []struct{ from, name, want string }{
		{"node_modules/b", "c", "node_modules/b/node_modules/c"},
		{"node_modules/a", "c", "node_modules/c"},
		{"", "@x/y", "node_modules/@x/y"},
		{"node_modules/@x/y/node_modules/z", "c", "node_modules/c"},
		{"node_modules/@x/y/node_modules/z", "a", "node_modules/a"},
		{"", "lib", "packages/lib"},
		{"packages/lib", "a", "node_modules/a"},
	}
	for _, c := range cases {
		got, ok := npmResolve(pkgs, c.from, c.name)
		if !ok || got != c.want {
			t.Errorf("resolve(%q,%q) = %q,%v want %q", c.from, c.name, got, ok, c.want)
		}
	}
	if _, ok := npmResolve(pkgs, "node_modules/a", "nope"); ok {
		t.Error("expected unresolved")
	}
}

// TestParseNPMMain checks the shared fixture: roots, edges and edge kinds.
func TestParseNPMMain(t *testing.T) {
	g, warns, err := ParseNPM("package-lock.json", readFixture(t, "../../testdata/projects/npm/package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app"}) {
		t.Fatalf("roots = %v", got)
	}
	want := []string{
		"a@1.0.0 -> c@1.0.0 prod",
		"a@1.0.0 -> d@1.0.0 prod",
		"b@1.0.0 -> c@2.0.0 prod",
		"b@1.0.0 -> d@1.0.0 prod",
		"root:app -> a@1.0.0 prod",
		"root:app -> b@1.0.0 dev",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v\nwant\n%v", got, want)
	}
	if len(g.Nodes) != 6 || g.Ecosystem != "npm" || g.Lockfile != "package-lock.json" {
		t.Fatalf("nodes = %d eco=%s lock=%s", len(g.Nodes), g.Ecosystem, g.Lockfile)
	}
}

// TestParseNPMWorkspace checks that workspace members become roots and a link
// entry resolves to the member it points at.
func TestParseNPMWorkspace(t *testing.T) {
	g, warns, err := ParseNPM("p", readFixture(t, "../../testdata/edge/npm-workspace/package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:mono", "root:lib"}) {
		t.Fatalf("roots = %v", got)
	}
	want := []string{
		"root:lib -> @x/y@3.0.0 prod",
		"root:mono -> root:lib prod",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges = %v", got)
	}
}

// TestParseNPMV1Unsupported checks the error for lockfileVersion 1 or missing.
func TestParseNPMV1Unsupported(t *testing.T) {
	_, _, err := ParseNPM("package-lock.json", readFixture(t, "../../testdata/edge/npm-v1/package-lock.json"))
	if err == nil || !strings.Contains(err.Error(), "package-lock.json lockfileVersion 1 is not supported; regenerate it with npm >= 7 (npm install)") {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(err.Error(), "parse package-lock.json: ") {
		t.Fatalf("err not wrapped: %v", err)
	}

	_, _, err = ParseNPM("package-lock.json", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "package-lock.json lockfileVersion 1 is not supported; regenerate it with npm >= 7 (npm install)") {
		t.Fatalf("err for missing lockfileVersion = %v", err)
	}
}

// TestParseNPMCycle checks that a dependency cycle parses without hanging.
func TestParseNPMCycle(t *testing.T) {
	g, _, err := ParseNPM("p", readFixture(t, "../../testdata/edge/cycle/package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a@1.0.0 -> b@1.0.0 prod", "b@1.0.0 -> a@1.0.0 prod", "root:cyc -> a@1.0.0 prod"}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges = %v", got)
	}
}

// TestParseNPMUnresolvedAndKinds checks the unresolved-dependency warning for prod
// edges, silence for optional and peer, and the optional edge kind.
func TestParseNPMUnresolvedAndKinds(t *testing.T) {
	data := []byte(`{"lockfileVersion":3,"packages":{
	  "":{"dependencies":{"gone":"*"},"optionalDependencies":{"opt":"*","missingopt":"*"},"peerDependencies":{"nopeer":"*"}},
	  "node_modules/opt":{"version":"1.0.0"}}}`)
	g, warns, err := ParseNPM("p", data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(warns, []string{"unresolved dependency gone of ."}) {
		t.Fatalf("warnings = %v", warns)
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, []string{"root:. -> opt@1.0.0 optional"}) {
		t.Fatalf("edges = %v", got)
	}
}

// TestParseNPMAliasedNameAndInvalidJSON checks that a "name" field overrides the
// path-derived name (npm aliases) and that malformed JSON is a wrapped error.
func TestParseNPMAliasedNameAndInvalidJSON(t *testing.T) {
	data := []byte(`{"lockfileVersion":3,"packages":{"":{"name":"p","dependencies":{"alias":"*"}},"node_modules/alias":{"name":"real","version":"2.0.0"}}}`)
	g, _, err := ParseNPM("p", data)
	if err != nil {
		t.Fatal(err)
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, []string{"root:p -> real@2.0.0 prod"}) {
		t.Fatalf("edges = %v", got)
	}
	if _, _, err := ParseNPM("bad.json", []byte("{")); err == nil || !strings.HasPrefix(err.Error(), "parse bad.json: ") {
		t.Fatalf("err = %v", err)
	}
}
