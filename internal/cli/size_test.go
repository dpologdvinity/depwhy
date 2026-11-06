package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// install writes an installed npm package into dir at rel (slash-separated):
// a package.json for name@version plus a payload file of n bytes.
func install(t *testing.T, dir, rel, name, version string, n int) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	pj := fmt.Sprintf(`{"name":%q,"version":%q}`, name, version)
	if err := os.WriteFile(filepath.Join(p, "package.json"), []byte(pj), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "payload"), make([]byte, n-len(pj)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// installedNPM stages the npm fixture with every package installed:
// a 1 KB, b 512 B, c@1.0.0 2 KB (hoisted), c@2.0.0 300 B (nested under b),
// d 100 B.
func installedNPM(t *testing.T) string {
	t.Helper()
	dir := stageFixture(t, project("npm"))
	install(t, dir, "node_modules/a", "a", "1.0.0", 1024)
	install(t, dir, "node_modules/b", "b", "1.0.0", 512)
	install(t, dir, "node_modules/b/node_modules/c", "c", "2.0.0", 300)
	install(t, dir, "node_modules/c", "c", "1.0.0", 2048)
	install(t, dir, "node_modules/d", "d", "1.0.0", 100)
	return dir
}

// TestSizeGolden pins the text and JSON size output of every mode on an
// installed npm project. Each version is measured at its own install path and
// b's nested node_modules (c@2.0.0) is not counted in b's own size.
func TestSizeGolden(t *testing.T) {
	dir := installedNPM(t)
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"why", []string{"c"}},
		{"why_json", []string{"--json", "c"}},
		{"remove", []string{"--remove", "b"}},
		{"remove_json", []string{"--json", "--remove", "a"}},
		{"heaviest", []string{"--heaviest"}},
		{"heaviest_json", []string{"--heaviest", "--json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errs := run(append([]string{"-C", dir, "--no-color"}, tt.args...)...)
			checkGolden(t, "sizes__"+tt.name+".txt", fmt.Sprintf("%s\n--- stderr\n%s\n--- exit=%d\n", out, errs, code))
		})
	}
}

// TestSizeUninstalledOptional checks that a platform-specific optional package
// that is not installed (fsevents on Linux) neither hides its parent from
// --heaviest nor the freed total from --remove: it is counted as not measured.
func TestSizeUninstalledOptional(t *testing.T) {
	dir := t.TempDir()
	lock := `{"lockfileVersion":3,"packages":{
		"":{"name":"app","dependencies":{"chokidar":"^3"}},
		"node_modules/chokidar":{"version":"3.6.0","optionalDependencies":{"fsevents":"~2.3.2"}},
		"node_modules/fsevents":{"version":"2.3.3","optional":true,"os":["darwin"]}}}`
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	install(t, dir, "node_modules/chokidar", "chokidar", "3.6.0", 2048)

	_, out, _ := run("-C", dir, "--no-color", "--heaviest")
	if !strings.Contains(out, "chokidar@3.6.0") || !strings.Contains(out, "~2.0 KB") || !strings.Contains(out, "(freed: 2 packages, 1 not measured)") {
		t.Fatalf("heaviest:\n%s", out)
	}
	_, out, _ = run("-C", dir, "--json", "--heaviest")
	if !strings.Contains(out, `"freed_size_bytes": 2048`) || !strings.Contains(out, `"freed_unmeasured": 1`) {
		t.Fatalf("heaviest json:\n%s", out)
	}
	_, out, _ = run("-C", dir, "--no-color", "--remove", "chokidar")
	if !strings.Contains(out, "would remove 2 packages (freeing ~2.0 KB; 1 not measured):") {
		t.Fatalf("remove:\n%s", out)
	}
}

// TestSizeNotDirect checks that a --remove of a package that is not a direct
// dependency carries no freed size: nothing was analyzed, so 0 would be wrong.
func TestSizeNotDirect(t *testing.T) {
	_, out, _ := run("-C", installedNPM(t), "--json", "--remove", "c")
	if strings.Contains(out, "freed_size_bytes") || strings.Contains(out, "freed_unmeasured") {
		t.Fatalf("not-direct remove has a freed size:\n%s", out)
	}
}

// TestSizeLockfileDir checks that with -l the sizes come from the lockfile's
// directory, not from --dir.
func TestSizeLockfileDir(t *testing.T) {
	dir := installedNPM(t)
	code, out, errs := run("-C", t.TempDir(), "--no-color", "-l", filepath.Join(dir, "package-lock.json"), "a")
	if code != 0 || !strings.Contains(out, "a@1.0.0 (1.0 KB)") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}

// TestSizeHostileLockfile checks that a lockfile naming paths outside the
// project never gets them measured.
func TestSizeHostileLockfile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	install(t, root, "secret", "../secret", "1.0.0", 4096)
	lock := `{"lockfileVersion":3,"packages":{
		"":{"name":"app","dependencies":{"x":"*","y":"*"}},
		"node_modules/x":{"name":"../secret","version":"1.0.0"},
		"node_modules/../../secret":{"name":"y","version":"1.0.0"}}}`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"../secret", "y"} {
		_, out, _ := run("-C", dir, "--json", q)
		if strings.Contains(out, "size_bytes") {
			t.Fatalf("%s measured outside the project:\n%s", q, out)
		}
	}
	_, out, _ := run("-C", dir, "--no-color", "--heaviest")
	if !strings.Contains(out, "no direct dependencies found with size data") {
		t.Fatalf("heaviest:\n%s", out)
	}
}

// TestSizePython checks that Python sizes are found through a dist-info
// directory whose name differs from the lockfile's only by normalization.
func TestSizePython(t *testing.T) {
	dir := stageFixture(t, project("uv"))
	site := filepath.Join(dir, ".venv", "lib", "python3.12", "site-packages")
	for rel, content := range map[string]string{
		"A-1.0.0.dist-info/RECORD": "a/__init__.py,,\n",
		"a/__init__.py":            strings.Repeat("x", 1536),
	} {
		p := filepath.Join(site, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, out, _ := run("-C", dir, "--no-color", "a")
	if !strings.Contains(out, "a@1.0.0 (1.5 KB)") {
		t.Fatalf("why:\n%s", out)
	}
}
