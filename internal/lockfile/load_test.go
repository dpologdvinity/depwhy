package lockfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// touch creates empty files in dir; Detect only checks that files exist.
func touch(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// formats lists the format ids of the found lockfiles.
func formats(fs []Found) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Format)
	}
	return out
}

// TestDetectOrderAndEcosystems checks the output order and the ecosystem filter.
func TestDetectOrderAndEcosystems(t *testing.T) {
	d := t.TempDir()
	touch(t, d, "Cargo.lock", "uv.lock", "package-lock.json")
	fs, warns, err := Detect(d, "")
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if got := formats(fs); !reflect.DeepEqual(got, []string{"npm", "uv", "cargo"}) {
		t.Fatalf("formats = %v", got)
	}
	if fs[1].Ecosystem != "python" || fs[1].Path != filepath.Join(d, "uv.lock") {
		t.Fatalf("found = %+v", fs[1])
	}
	fs, _, _ = Detect(d, "cargo")
	if got := formats(fs); !reflect.DeepEqual(got, []string{"cargo"}) {
		t.Fatalf("filtered = %v", got)
	}
}

// TestDetectPnpmWinsOverNpm checks that pnpm-lock.yaml is preferred over
// package-lock.json, with a warning.
func TestDetectPnpmWinsOverNpm(t *testing.T) {
	d := t.TempDir()
	touch(t, d, "pnpm-lock.yaml", "package-lock.json")
	fs, warns, err := Detect(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := formats(fs); !reflect.DeepEqual(got, []string{"pnpm"}) {
		t.Fatalf("formats = %v", got)
	}
	want := []string{"both pnpm-lock.yaml and package-lock.json found; using pnpm-lock.yaml"}
	if !reflect.DeepEqual(warns, want) {
		t.Fatalf("warns = %v", warns)
	}
}

// TestDetectNothing checks the error when no lockfile matches.
func TestDetectNothing(t *testing.T) {
	d := t.TempDir()
	touch(t, d, "uv.lock")
	_, _, err := Detect(d, "cargo")
	want := "no supported lockfile in " + d + " (looked for pnpm-lock.yaml, package-lock.json, uv.lock, Cargo.lock)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
}

// TestFromPaths checks load order, the unrecognized-name error, and the error when
// the ecosystem filter leaves nothing.
func TestFromPaths(t *testing.T) {
	fs, err := FromPaths([]string{"x/Cargo.lock", "y/pnpm-lock.yaml", "z/package-lock.json"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Explicit paths skip the one-per-ecosystem rule and load in table order.
	if got := formats(fs); !reflect.DeepEqual(got, []string{"pnpm", "npm", "cargo"}) {
		t.Fatalf("formats = %v", got)
	}
	if _, err := FromPaths([]string{"x/yarn.lock"}, ""); err == nil || err.Error() != "unrecognized lockfile name: yarn.lock" {
		t.Fatalf("err = %v", err)
	}
	if _, err := FromPaths([]string{"x/uv.lock"}, "npm"); err == nil {
		t.Fatal("want error when filter empties the set")
	}
}

// TestLoadDispatches checks that Load picks the parser by format and reports read errors.
func TestLoadDispatches(t *testing.T) {
	g, _, err := Load(Found{Path: "../../testdata/projects/npm/package-lock.json", Ecosystem: "npm", Format: "npm"})
	if err != nil || len(g.Nodes) != 6 {
		t.Fatalf("err=%v", err)
	}
	g, _, err = Load(Found{Path: "../../testdata/projects/pnpm/pnpm-lock.yaml", Ecosystem: "npm", Format: "pnpm"})
	if err != nil || len(g.Nodes) != 6 {
		t.Fatalf("err=%v", err)
	}
	g, _, err = Load(Found{Path: "../../testdata/edge/yarn-classic/yarn.lock", Ecosystem: "npm", Format: "yarn"})
	if err != nil || len(g.Nodes) != 5 {
		t.Fatalf("err=%v", err)
	}
	g, _, err = Load(Found{Path: "../../testdata/edge/bun/bun.lock", Ecosystem: "npm", Format: "bun"})
	if err != nil || len(g.Nodes) != 5 {
		t.Fatalf("err=%v", err)
	}
	if _, _, err := Load(Found{Path: "nope/uv.lock", Format: "uv"}); err == nil {
		t.Fatal("want read error")
	}
}

// TestLoadUnknownFormat checks the error for a format id that has no parser.
func TestLoadUnknownFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(Found{Path: p, Format: "bogus"}); err == nil {
		t.Fatal("want error")
	}
}
