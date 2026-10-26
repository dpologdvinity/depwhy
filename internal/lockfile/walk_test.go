package lockfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeTree creates files relative to dir for recursive discovery tests.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectRecursive(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.mod": "", "web/package-lock.json": "", "web/yarn.lock": "",
		"web/bun.lock": "", "web/pnpm-lock.yaml": "", "api/Cargo.lock": "",
		"worker/poetry.lock": "", "worker/uv.lock": "",
	})
	for _, skip := range []string{".git", "node_modules", ".venv", "venv", "target", "dist", "build", "vendor", "__pycache__", ".tox"} {
		writeTree(t, dir, map[string]string{"web/" + skip + "/nested/package-lock.json": ""})
	}
	found, warns, err := DetectRecursive(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range found {
		paths = append(paths, f.Path)
	}
	want := []string{"go.mod", "api/Cargo.lock", "web/pnpm-lock.yaml", "worker/uv.lock"}
	if !reflect.DeepEqual(paths, want) || !reflect.DeepEqual(formats(found), []string{"gomod", "cargo", "pnpm", "uv"}) {
		t.Fatalf("found = %+v", found)
	}
	if len(warns) != 4 || !strings.HasPrefix(warns[0], "web: both pnpm-lock.yaml and yarn.lock") || !strings.HasPrefix(warns[3], "worker:") {
		t.Fatalf("warnings = %v", warns)
	}
	found, warns, err = DetectRecursive(dir, "cargo")
	if err != nil || len(warns) != 0 || len(found) != 1 || found[0].Path != "api/Cargo.lock" {
		t.Fatalf("filtered = %+v, %v, %v", found, warns, err)
	}
}

func TestRecursiveGitignore(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".gitignore":                "# comment\nignored/\n*.cache/\n/root-only/\n**/generated/**\nsecret.lock\npackages/*\n!packages/keep/\n\\#hidden/\n\\!hidden/\nspace\\ /\n",
		"ignored/package-lock.json": "", "x.cache/Cargo.lock": "",
		"root-only/go.mod": "", "packages/drop/package-lock.json": "",
		"packages/keep/package-lock.json": "", "packages/keep/generated/a/go.mod": "",
		"#hidden/Cargo.lock": "", "!hidden/go.mod": "", "space /go.mod": "",
		"nested/root-only/go.mod": "", "nested/.gitignore": "Cargo.lock\n!root-only/go.mod\n",
		"nested/Cargo.lock": "", "nested/package-lock.json": "",
		"fallback/.gitignore": "pnpm-lock.yaml\n", "fallback/pnpm-lock.yaml": "",
		"fallback/package-lock.json": "",
		"overrides/.gitignore":       "!Cargo.lock\n", "overrides/Cargo.lock": "",
	})
	// Nested rules can override a parent's file rule, but cannot reopen a pruned directory.
	f, err := os.OpenFile(filepath.Join(dir, ".gitignore"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("Cargo.lock\n!ignored/package-lock.json\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	found, warns, err := DetectRecursive(dir, "")
	if err != nil || len(warns) != 0 {
		t.Fatalf("%v, %v", err, warns)
	}
	var paths []string
	for _, f := range found {
		paths = append(paths, f.Path)
	}
	want := []string{"fallback/package-lock.json", "nested/package-lock.json", "nested/root-only/go.mod", "overrides/Cargo.lock", "packages/keep/package-lock.json"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestRecursiveSymlinksAndErrors(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"real/go.mod": ""})
	if err := os.Symlink(dir, filepath.Join(dir, "real", "loop")); err != nil {
		t.Fatal(err)
	}
	found, _, err := DetectRecursive(dir, "")
	if err != nil || len(found) != 1 {
		t.Fatalf("symlink walk = %v, %v", found, err)
	}
	for _, root := range []string{t.TempDir(), filepath.Join(dir, "missing"), filepath.Join(dir, "real", "go.mod")} {
		if _, _, err := DetectRecursive(root, ""); err == nil {
			t.Errorf("expected error for %s", root)
		}
	}
	bad := t.TempDir()
	if err := os.Mkdir(filepath.Join(bad, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DetectRecursive(bad, ""); err == nil {
		t.Fatal("expected unreadable gitignore error")
	}
}

func TestGitignorePatterns(t *testing.T) {
	for _, tt := range []struct {
		pattern, name string
		dir, want     bool
	}{
		{"cache/", "cache", true, true},
		{"cache/", "cache", false, false},
		{"cache/", "a/cache", true, true},
		{"/cache/", "a/cache", true, false},
		{"/cache/", "cache", true, true},
		{"a/cache", "a/cache", true, true},
		{"a/cache", "other/a/cache", true, false},
		{"**/cache", "cache", true, true},
		{"**/cache", "a/b/cache", true, true},
		{"cache/**", "cache", true, false},
		{"cache/**", "cache/a/b", true, true},
		{"a/**/cache", "a/cache", true, true},
		{"a/**/cache", "a/b/c/cache", true, true},
		{"*.lock", "a/poetry.lock", false, true},
		{"[ab]?.lock", "a1.lock", false, true},
		{"[!ab]*.lock", "c.lock", false, true},
		{"[!ab]*.lock", "a.lock", false, false},
		{"[[:digit:]]/", "1", true, true},
		{"[[:digit:]]/", "a", true, false},
		{"[]a]/", "]", true, true},
		{"[!]]/", "a", true, true},
		{"[!]]/", "]", true, false},
		{`[\d]/`, "d", true, true},
		{`[a\-c]/`, "-", true, true},
		{`[a\-c]/`, "b", true, false},
		{"[broken", "poetry.lock", false, false},
		{"\\#cache", "#cache", true, true},
		{"\\!cache", "!cache", true, true},
		{"cache   \r\n", "cache", true, true},
		{"cache\\ ", "cache ", true, true},
		{"#cache\n\n/\n!", "cache", true, false},
		{"*.lock\n!poetry.lock", "poetry.lock", false, false},
		{"!poetry.lock\n*.lock", "poetry.lock", false, true},
	} {
		t.Run(tt.pattern+":"+tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{".gitignore": tt.pattern})
			rules, err := readIgnore(dir, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := ignored(tt.name, tt.dir, rules); got != tt.want {
				t.Fatalf("ignored(%q, %q, directory=%v) = %v, want %v", tt.pattern, tt.name, tt.dir, got, tt.want)
			}
		})
	}
}

func TestIgnoreWildcardBounded(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{".gitignore": strings.Repeat("**/a/", 10) + "z"})
	rules, err := readIgnore(dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if ignored(strings.Repeat("a/", 32)+"a", true, rules) {
		t.Fatal("nonmatching final segment was ignored")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("wildcard matching took %v; repeated ** must not backtrack exponentially", elapsed)
	}
}

func TestRecursiveUnreadableSubdirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read directories regardless of permissions")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"blocked/go.mod": ""})
	blocked := filepath.Join(dir, "blocked")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o755); err != nil {
			t.Error(err)
		}
	})
	if _, _, err := DetectRecursive(dir, ""); err == nil {
		t.Fatal("expected a traversal error, not partial results")
	}
}
