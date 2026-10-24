package lockfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func touch(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func formats(fs []Found) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Format)
	}
	return out
}

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

func TestDetectNothing(t *testing.T) {
	d := t.TempDir()
	touch(t, d, "uv.lock")
	_, _, err := Detect(d, "cargo")
	want := "no supported lockfile in " + d + " (looked for pnpm-lock.yaml, package-lock.json, uv.lock, Cargo.lock)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
}

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

func TestLoadDispatches(t *testing.T) {
	g, _, err := Load(Found{Path: "../../testdata/projects/npm/package-lock.json", Ecosystem: "npm", Format: "npm"})
	if err != nil || len(g.Nodes) != 6 {
		t.Fatalf("err=%v", err)
	}
	g, _, err = Load(Found{Path: "../../testdata/projects/pnpm/pnpm-lock.yaml", Ecosystem: "npm", Format: "pnpm"})
	if err != nil || len(g.Nodes) != 6 {
		t.Fatalf("err=%v", err)
	}
	if _, _, err := Load(Found{Path: "nope/uv.lock", Format: "uv"}); err == nil {
		t.Fatal("want read error")
	}
}
