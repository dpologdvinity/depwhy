package lockfile

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// wantEdges fails the test for every "from -> to kind" string (see edgeStrings)
// that is missing from g.
func wantEdges(t *testing.T, g *graph.Graph, want ...string) {
	t.Helper()
	got := edgeStrings(g)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("missing edge %q", w)
		}
	}
}

// edgesTo lists the edges (as edgeStrings renders them) that end at the node id.
func edgesTo(g *graph.Graph, id string) []string {
	var out []string
	for _, e := range edgeStrings(g) {
		if strings.Contains(e, " -> "+id+" ") {
			out = append(out, e)
		}
	}
	return out
}

// parseYarnFiles writes files (relative path -> content) to a temp directory and
// parses the yarn.lock among them, so package.json lookups see the files.
func parseYarnFiles(t *testing.T, files map[string]string) (*graph.Graph, []string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lock := filepath.Join(dir, "yarn.lock")
	g, warns, err := ParseYarn(lock, []byte(files["yarn.lock"]))
	if err != nil {
		t.Fatalf("ParseYarn: %v", err)
	}
	return g, warns
}

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
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app"}) {
		t.Fatalf("roots = %v, want [root:app]", got)
	}
	wantEdges(t, g,
		"root:app -> a@1.0.0 prod",
		"root:app -> b@1.0.0 peer",
		"a@1.0.0 -> c@1.0.0 prod",
		"a@1.0.0 -> d@1.0.0 prod",
	)
}

// TestParseYarnBerryWorkspaces checks a real Yarn 4.9 lockfile: multi-descriptor
// keys, scoped names, an npm: alias, the builtin typescript and fsevents compat
// patches, an optional dependency, workspace members with a workspace:
// dependency, devDependencies told apart through package.json, and a peer
// dependency provided by the parent.
func TestParseYarnBerryWorkspaces(t *testing.T) {
	path := "../../testdata/edge/yarn-berry-workspaces/yarn.lock"
	g, warns, err := ParseYarn(path, readFixture(t, path))
	if err != nil {
		t.Fatalf("ParseYarn: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app", "root:left", "root:right"}) {
		t.Fatalf("roots = %v", got)
	}
	if v := g.Nodes[g.Roots[2]].Version; v != "2.0.0" {
		t.Errorf("right version = %q, want 2.0.0 from package.json", v)
	}
	for _, name := range []string{"is-number", "typescript", "fsevents", "ms", "normalize-path", "@sindresorhus/is", "@isaacs/fs-minipass"} {
		if n := len(g.Find(name, "")); n != 1 {
			t.Errorf("Find(%q) = %d nodes, want 1", name, n)
		}
	}
	wantEdges(t, g,
		"root:app -> chokidar@3.6.0 prod",
		"root:app -> is-number@7.0.0 prod",
		"root:app -> ms@2.1.3 prod", // my-ms: npm:ms@2.1.3
		"root:app -> typescript@5.9.3 dev",
		"root:app -> @sindresorhus/is@4.6.0 dev",
		"root:left -> is-number@7.0.0 prod",
		"root:left -> ms@2.1.3 dev",
		"root:left -> root:right prod",
		"root:right -> ms@2.1.3 prod",
		"chokidar@3.6.0 -> fsevents@2.3.3 optional",
		"chokidar@3.6.0 -> normalize-path@3.0.0 prod",
		"anymatch@3.1.3 -> normalize-path@3.0.0 prod",
		"tar@7.5.22 -> @isaacs/fs-minipass@4.0.1 prod",
		"fdir@6.5.0 -> picomatch@4.0.7 peer",
	)
	if got := edgesTo(g, "picomatch@2.3.2"); slices.Contains(got, "fdir@6.5.0 -> picomatch@2.3.2 peer") {
		t.Errorf("fdir peer resolved to picomatch 2: %v", got)
	}
}

// TestParseYarnBerryUserPatch checks a real lockfile written by yarn patch-commit:
// the patched package and its npm: base are one node, root optional and peer
// declarations are read, and a dev-and-peer dependency counts as dev.
func TestParseYarnBerryUserPatch(t *testing.T) {
	g, warns := parseYarnFiles(t, map[string]string{
		"package.json": `{"name": "app", "version": "1.2.3",
  "dependencies": {"is-number": "^7.0.0", "ms": "patch:ms@npm%3A2.1.3#~/.yarn/patches/ms-npm-2.1.3-81ff3cfac1.patch"},
  "optionalDependencies": {"is-extglob": "^2.1.1"},
  "peerDependencies": {"is-glob": "^4.0.0"},
  "devDependencies": {"is-glob": "^4.0.3"}}`,
		"yarn.lock": `__metadata:
  version: 8
  cacheKey: 10c0

"app@workspace:.":
  version: 0.0.0-use.local
  resolution: "app@workspace:."
  dependencies:
    is-extglob: "npm:^2.1.1"
    is-glob: "npm:^4.0.3"
    is-number: "npm:^7.0.0"
    ms: "patch:ms@npm%3A2.1.3#~/.yarn/patches/ms-npm-2.1.3-81ff3cfac1.patch"
  peerDependencies:
    is-glob: ^4.0.0
  dependenciesMeta:
    is-extglob:
      optional: true
  languageName: unknown
  linkType: soft

"is-extglob@npm:^2.1.1":
  version: 2.1.1
  resolution: "is-extglob@npm:2.1.1"
  languageName: node
  linkType: hard

"is-glob@npm:^4.0.3":
  version: 4.0.3
  resolution: "is-glob@npm:4.0.3"
  dependencies:
    is-extglob: "npm:^2.1.1"
  languageName: node
  linkType: hard

"is-number@npm:^7.0.0":
  version: 7.0.0
  resolution: "is-number@npm:7.0.0"
  languageName: node
  linkType: hard

"ms@npm:2.1.3":
  version: 2.1.3
  resolution: "ms@npm:2.1.3"
  languageName: node
  linkType: hard

"ms@patch:ms@npm%3A2.1.3#~/.yarn/patches/ms-npm-2.1.3-81ff3cfac1.patch":
  version: 2.1.3
  resolution: "ms@patch:ms@npm%3A2.1.3#~/.yarn/patches/ms-npm-2.1.3-81ff3cfac1.patch::version=2.1.3&hash=8932e0"
  languageName: node
  linkType: hard
`,
	})
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if v := g.Nodes[g.Roots[0]].Version; v != "1.2.3" {
		t.Errorf("root version = %q, want 1.2.3", v)
	}
	want := []string{
		"is-glob@4.0.3 -> is-extglob@2.1.1 prod",
		"root:app -> is-extglob@2.1.1 optional",
		"root:app -> is-glob@4.0.3 dev",
		"root:app -> is-number@7.0.0 prod",
		"root:app -> ms@2.1.3 prod",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Errorf("edges =\n%v\nwant\n%v", got, want)
	}
}

// TestParseYarnBerryFallback checks dependencies whose descriptor has no entry,
// as happens with package.json resolutions overrides: a single candidate is used
// silently, several give the first in sorted order and a warning, none warn.
// An unreadable workspace package.json is warned about too.
func TestParseYarnBerryFallback(t *testing.T) {
	lock := `__metadata:
  version: 8

"app@workspace:.":
  version: 0.0.0-use.local
  resolution: "app@workspace:."
  dependencies:
    one: "npm:^7.0.0"
    two: "npm:^3.0.0"
    gone: "npm:^1.0.0"

"one@npm:6.0.0":
  version: 6.0.0
  resolution: "one@npm:6.0.0"

"two@npm:2.0.0":
  version: 2.0.0
  resolution: "two@npm:2.0.0"

"two@npm:1.0.0":
  version: 1.0.0
  resolution: "two@npm:1.0.0"
`
	for range 5 { // map order must never leak into the result
		g, warns := parseYarnFiles(t, map[string]string{"yarn.lock": lock, "package.json": "{"})
		wantEdges(t, g, "root:app -> one@6.0.0 prod", "root:app -> two@1.0.0 prod")
		if got := edgesTo(g, "two@2.0.0"); len(got) != 0 {
			t.Errorf("edges to two@2.0.0 = %v", got)
		}
		if len(warns) == 0 || !strings.HasSuffix(warns[0], "package.json: unexpected end of JSON input") {
			t.Fatalf("warns = %q, want a package.json error first", warns)
		}
		warns = warns[1:]
		want := []string{
			"unresolved dependency gone of app",
			"two@npm:^3.0.0 of app matches several entries; using two@npm:1.0.0",
			"two@2.0.0 is not reachable from any root",
		}
		if !reflect.DeepEqual(warns, want) {
			t.Fatalf("warns = %q, want %q", warns, want)
		}
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
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app"}) {
		t.Fatalf("roots = %v, want [root:app]", got)
	}
	wantEdges(t, g, "root:app -> a@1.0.0 prod", "root:app -> b@1.0.0 dev")
	if n := len(g.Out[g.Roots[0]]); n != 2 {
		t.Fatalf("root has %d edges, want 2", n)
	}
}

// TestParseYarnClassicWorkspaces checks a real Yarn 1.22 lockfile: optional
// dependencies, unquoted and multi-descriptor headers, a scoped package, an
// npm: alias, and workspace members found through package.json globs.
func TestParseYarnClassicWorkspaces(t *testing.T) {
	path := "../../testdata/edge/yarn-classic-workspaces/yarn.lock"
	g, warns, err := ParseYarn(path, readFixture(t, path))
	if err != nil {
		t.Fatalf("ParseYarn: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app", "root:left", "root:right"}) {
		t.Fatalf("roots = %v", got)
	}
	if n := len(g.Find("ms", "")); n != 1 {
		t.Errorf("Find(ms) = %d nodes, want 1 (alias merged)", n)
	}
	wantEdges(t, g,
		"root:app -> chokidar@3.6.0 prod",
		"root:app -> ms@2.1.3 prod", // my-ms: npm:ms@2.1.3
		"root:app -> @sindresorhus/is@4.6.0 dev",
		"chokidar@3.6.0 -> fsevents@2.3.3 optional",
		"root:left -> is-number@7.0.0 prod",
		"root:left -> ms@2.1.3 dev",
		"root:left -> root:right prod",
		"root:right -> ms@2.1.3 prod",
	)
}

// TestParseYarnClassicWorkspacesObject checks the {"packages": [...]} form of
// workspaces, that a member without a name is labelled by its path, and that an
// unreadable member package.json is warned about and skipped.
func TestParseYarnClassicWorkspacesObject(t *testing.T) {
	g, warns := parseYarnFiles(t, map[string]string{
		"package.json":             `{"name": "app", "workspaces": {"packages": ["libs/*", "missing/*"]}}`,
		"libs/x/package.json":      `{"dependencies": {"a": "^1.0.0"}}`,
		"libs/notes.txt":           "not a workspace",
		"libs/broken/package.json": `{`,
		"yarn.lock": `# yarn lockfile v1

a@^1.0.0:
  version "1.0.0"
`,
	})
	if len(warns) != 1 || !strings.Contains(warns[0], filepath.Join("libs", "broken", "package.json")) {
		t.Fatalf("warns = %v, want one about libs/broken/package.json", warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app", "root:libs/x"}) {
		t.Fatalf("roots = %v", got)
	}
	wantEdges(t, g, "root:libs/x -> a@1.0.0 prod")
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

func TestYarnPackageName(t *testing.T) {
	cases := map[string]string{
		"a@npm:^1.0.0":       "a",
		"@scope/pkg@npm:1.0": "@scope/pkg",
		"typescript@patch:typescript@npm%3A5.9.3#optional!builtin<compat/typescript>::version=5.9.3&hash=5786d5": "typescript",
		"my-ms@npm:ms@2.1.3":         "ms",
		"alias@npm:@scope/real@^2":   "@scope/real",
		"left@workspace:packages/x":  "left",
		"plain":                      "plain",
		`"quoted@^1.0.0"`:            "quoted",
		"classic-alias@npm:ms@2.1.3": "ms",
	}
	for in, want := range cases {
		if got := yarnPackageName(in); got != want {
			t.Errorf("yarnPackageName(%q) = %q, want %q", in, got, want)
		}
	}
}
