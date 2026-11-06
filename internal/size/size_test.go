package size

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{44145050, "42.1 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tc := range tests {
		got := Format(tc.bytes)
		if got != tc.want {
			t.Errorf("Format(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

// write creates base/rel (slash-separated) with the given content, making
// parent directories as needed.
func write(t *testing.T, base, rel, content string) {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// symlink creates link -> target, skipping the test where symlinks are not
// permitted (unprivileged Windows).
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not permitted: %v", err)
	}
}

// manifest returns a package.json for name@version.
func manifest(name, version string) string {
	return `{"name":"` + name + `","version":"` + version + `"}`
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "hello")      // 5 bytes
	write(t, dir, "sub/b.txt", "world!") // 6 bytes

	s := NewScanner()
	got, err := s.DirSize(dir)
	if err != nil || got != 11 {
		t.Fatalf("DirSize = %d, %v; want 11", got, err)
	}
	// Repeated call hits the cache.
	if got, err := s.DirSize(dir); err != nil || got != 11 {
		t.Fatalf("cached DirSize = %d, %v", got, err)
	}
	if _, err := s.DirSize(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Fatal("expected error for non-existent directory")
	}
	// A regular file measures as itself.
	if got, err := s.DirSize(filepath.Join(dir, "a.txt")); err != nil || got != 5 {
		t.Fatalf("file DirSize = %d, %v", got, err)
	}
}

func TestDirSizeIgnoresSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "huge.bin", strings.Repeat("x", 10000))
	write(t, dir, "a.txt", "abc")
	symlink(t, outside, filepath.Join(dir, "link-outside"))

	s := NewScanner()
	if got, err := s.DirSize(dir); err != nil || got != 3 {
		t.Fatalf("DirSize with symlink = %d, %v; want 3", got, err)
	}
	// A symlink as the measured path is never measured as its link text
	// (the pnpm bug that reported "32 B" for every package).
	if got, err := s.DirSize(filepath.Join(dir, "link-outside")); err == nil {
		t.Fatalf("DirSize(symlink) = %d, want an error", got)
	}
}

// TestNPMSizeInstallPath checks measurement by package-lock.json key: each
// version is measured at its own install path, and a package's nested
// node_modules (other packages) is excluded so freed totals never count a
// nested package twice.
func TestNPMSizeInstallPath(t *testing.T) {
	proj := t.TempDir()
	write(t, proj, "node_modules/a/package.json", manifest("a", "1.0.0"))
	write(t, proj, "node_modules/a/index.js", "0123456789")
	write(t, proj, "node_modules/a/node_modules/c/package.json", manifest("c", "1.0.0"))
	write(t, proj, "node_modules/a/node_modules/c/big.js", strings.Repeat("x", 100))
	write(t, proj, "node_modules/c/package.json", manifest("c", "2.0.0"))
	write(t, proj, "node_modules/c/index.js", "x")

	s := NewScanner()
	pj := int64(len(manifest("a", "1.0.0")))
	if got, err := s.NPMSize(proj, "node_modules/a", "a", "1.0.0"); err != nil || got != pj+10 {
		t.Fatalf("a = %d, %v; want %d (nested node_modules excluded)", got, err, pj+10)
	}
	if got, err := s.NPMSize(proj, "node_modules/a/node_modules/c", "c", "1.0.0"); err != nil || got != pj+100 {
		t.Fatalf("nested c = %d, %v; want %d", got, err, pj+100)
	}
	// Without an install path, node_modules/<name> is used only when its
	// manifest is the requested version: the hoisted c@2.0.0 is not c@1.0.0.
	if got, err := s.NPMSize(proj, "", "c", "1.0.0"); err == nil {
		t.Fatalf("c@1.0.0 by name = %d, want unknown", got)
	}
	if got, err := s.NPMSize(proj, "", "c", "2.0.0"); err != nil || got != pj+1 {
		t.Fatalf("c@2.0.0 by name = %d, %v", got, err)
	}
	// A stale install (lockfile says another version) is unknown, not wrong.
	if _, err := s.NPMSize(proj, "node_modules/c", "c", "3.0.0"); err == nil {
		t.Fatal("version mismatch must be unknown")
	}
	if _, err := s.NPMSize(proj, "node_modules/missing", "missing", "1.0.0"); err == nil {
		t.Fatal("missing package must be unknown")
	}
}

// TestNPMSizeHostileNames checks that lockfile-controlled names and install
// paths can never make the scanner measure anything outside the project.
func TestNPMSizeHostileNames(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	write(t, proj, "package-lock.json", "{}")
	write(t, root, "secret/package.json", manifest("../../secret", "1.0.0"))
	write(t, root, "secret/key", "top secret")

	s := NewScanner()
	for _, tc := range []struct{ path, name string }{
		{"", "../../secret"},
		{"", "../../../../../../../../.."},
		{"", "/etc"},
		{"", ""},
		{"", "."},
		{"node_modules/../../secret", "../../secret"},
		{"../secret", "../../secret"},
		{"/etc", "x"},
	} {
		if got, err := s.NPMSize(proj, tc.path, tc.name, "1.0.0"); err == nil {
			t.Errorf("NPMSize(%q, %q) = %d, want an error", tc.path, tc.name, got)
		}
	}
}

// TestNPMSizeSymlinkEscape checks that a symlink inside node_modules that
// points outside the project is not followed, even with a matching manifest.
func TestNPMSizeSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	write(t, root, "outside/package.json", manifest("evil", "1.0.0"))
	symlink(t, filepath.Join(root, "outside"), filepath.Join(proj, "node_modules", "evil"))

	s := NewScanner()
	if got, err := s.NPMSize(proj, "node_modules/evil", "evil", "1.0.0"); err == nil {
		t.Fatalf("escaping symlink measured %d", got)
	}
	if got, err := s.NPMSize(proj, "", "evil", "1.0.0"); err == nil {
		t.Fatalf("escaping symlink measured %d by name", got)
	}
}

// TestNPMSizePnpm checks pnpm's layout: node_modules/<name> is a symlink into
// the virtual store, and transitive packages exist only in the store.
func TestNPMSizePnpm(t *testing.T) {
	proj := t.TempDir()
	write(t, proj, "node_modules/.pnpm/big@1.0.0/node_modules/big/package.json", manifest("big", "1.0.0"))
	write(t, proj, "node_modules/.pnpm/big@1.0.0/node_modules/big/index.js", strings.Repeat("x", 1000))
	write(t, proj, "node_modules/.pnpm/@s+x@2.0.0_react@18.2.0/node_modules/@s/x/package.json", manifest("@s/x", "2.0.0"))
	write(t, proj, "node_modules/.pnpm/@s+x@2.0.0_react@18.2.0/node_modules/@s/x/a.js", "abc")
	write(t, proj, "node_modules/.pnpm/big@1.0.0-beta/node_modules/big/package.json", manifest("big", "1.0.0-beta"))
	symlink(t, filepath.Join(".pnpm", "big@1.0.0", "node_modules", "big"), filepath.Join(proj, "node_modules", "big"))

	s := NewScanner()
	want := int64(len(manifest("big", "1.0.0")) + 1000)
	if got, err := s.NPMSize(proj, "", "big", "1.0.0"); err != nil || got != want {
		t.Fatalf("big = %d, %v; want %d", got, err, want)
	}
	// Transitive, scoped, with a peer suffix: found only in the store.
	if got, err := s.NPMSize(proj, "", "@s/x", "2.0.0"); err != nil || got != int64(len(manifest("@s/x", "2.0.0"))+3) {
		t.Fatalf("@s/x = %d, %v", got, err)
	}
	if _, err := s.NPMSize(proj, "", "big", "1.0.1"); err == nil {
		t.Fatal("uninstalled version must be unknown")
	}
	// The top-level symlink alone is enough when the store layout is unknown.
	proj2 := t.TempDir()
	write(t, proj2, "vendor/lib/package.json", manifest("lib", "1.0.0"))
	symlink(t, filepath.Join("..", "vendor", "lib"), filepath.Join(proj2, "node_modules", "lib"))
	if got, err := s.NPMSize(proj2, "", "lib", "1.0.0"); err != nil || got != int64(len(manifest("lib", "1.0.0"))) {
		t.Fatalf("linked lib = %d, %v", got, err)
	}
}

// TestNPMSizeRelativeBase checks the default invocation (project dir ".")
// with an absolute symlink that stays inside the project, as Windows pnpm
// junctions and some tools create.
func TestNPMSizeRelativeBase(t *testing.T) {
	proj := t.TempDir()
	write(t, proj, "vendor/x/package.json", manifest("x", "1.0.0"))
	symlink(t, filepath.Join(proj, "vendor", "x"), filepath.Join(proj, "node_modules", "x"))
	t.Chdir(proj)
	if got, err := NewScanner().NPMSize(".", "", "x", "1.0.0"); err != nil || got != int64(len(manifest("x", "1.0.0"))) {
		t.Fatalf("x = %d, %v", got, err)
	}
}

// TestPythonSizeRelativeVenv checks RECORD containment with a relative venv
// root whose site-packages is reached through an absolute symlink.
func TestPythonSizeRelativeVenv(t *testing.T) {
	root, site := venv(t)
	write(t, site, "demo-1.0.dist-info/RECORD", "demo.py,,\n")
	write(t, site, "demo.py", "abc")
	symlink(t, site, filepath.Join(root, "site-link"))
	t.Chdir(filepath.Dir(root))
	got, err := NewScanner().PythonSize(filepath.Join(".venv", "site-link"), ".venv", "demo", "1.0")
	if err != nil || got != 3 {
		t.Fatalf("demo = %d, %v; want 3", got, err)
	}
}

// TestPythonSizeEmptyRecord checks that a RECORD with nothing countable is
// unknown, not "0 B".
func TestPythonSizeEmptyRecord(t *testing.T) {
	root, site := venv(t)
	write(t, site, "gone-1.0.dist-info/RECORD", "gone/missing.py,,\n")
	if got, err := NewScanner().PythonSize(site, root, "gone", "1.0"); err == nil {
		t.Fatalf("gone = %d, want unknown", got)
	}
}

// venv creates <dir>/.venv with a site-packages dir and returns both paths.
func venv(t *testing.T) (root, site string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), ".venv")
	site = filepath.Join(root, "lib", "python3.12", "site-packages")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, site
}

// TestPythonSizeNormalizedNames checks that dist-info directories are matched
// by PEP 503 normalized name, whatever case and separators they were installed
// with.
func TestPythonSizeNormalizedNames(t *testing.T) {
	root, site := venv(t)
	write(t, site, "PyYAML-6.0.1.dist-info/RECORD", "yaml/__init__.py,sha256=x,4\nPyYAML-6.0.1.dist-info/RECORD,,\n")
	write(t, site, "yaml/__init__.py", "abcd")
	write(t, site, "zope.interface-6.0.dist-info/RECORD", "zope/interface/__init__.py,,\n")
	write(t, site, "zope/interface/__init__.py", "12345")
	write(t, site, "typing_extensions-4.9.0.dist-info/RECORD", "typing_extensions.py,,\n")
	write(t, site, "typing_extensions.py", "1234567")

	s := NewScanner()
	recordLen := int64(len("yaml/__init__.py,sha256=x,4\nPyYAML-6.0.1.dist-info/RECORD,,\n"))
	for _, tc := range []struct {
		name, version string
		want          int64
	}{
		{"pyyaml", "6.0.1", 4 + recordLen},
		{"PyYAML", "6.0.1", 4 + recordLen},
		{"zope-interface", "6.0", 5},
		{"zope.interface", "6.0", 5},
		{"typing-extensions", "4.9.0", 7},
	} {
		if got, err := s.PythonSize(site, root, tc.name, tc.version); err != nil || got != tc.want {
			t.Errorf("%s@%s = %d, %v; want %d", tc.name, tc.version, got, err, tc.want)
		}
	}
	for _, tc := range [][2]string{{"pyyaml", "5.0"}, {"missing", "1.0"}, {"../../etc", "1.0"}, {"", ""}} {
		if got, err := s.PythonSize(site, root, tc[0], tc[1]); err == nil {
			t.Errorf("%s@%s = %d, want unknown", tc[0], tc[1], got)
		}
	}
}

// TestPythonSizeRecord checks RECORD parsing: CSV quoting (paths with commas),
// files outside site-packages but inside the venv (scripts) counted, files
// outside the venv never counted, and a missing RECORD reported as unknown.
func TestPythonSizeRecord(t *testing.T) {
	root, site := venv(t)
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	write(t, filepath.Dir(root), "outside.txt", strings.Repeat("o", 1000))
	record := strings.Join([]string{
		`"demo/a,b.py",sha256=x,3`,
		`demo/__init__.py,sha256=y,999999`, // recorded sizes are not trusted
		`../../../bin/demo,,`,
		`../../../../outside.txt,,`,
		filepath.ToSlash(outside) + `,,`,
		`demo/missing.py,,`,
		`demo/__init__.py,,`, // listed twice, counted once
		``,
	}, "\n")
	write(t, site, "demo-1.0.dist-info/RECORD", record)
	write(t, site, "demo/a,b.py", "abc")
	write(t, site, "demo/__init__.py", "x")
	write(t, root, "bin/demo", "#!python\n")

	s := NewScanner()
	want := int64(3 + 1 + len("#!python\n"))
	if got, err := s.PythonSize(site, root, "demo", "1.0"); err != nil || got != want {
		t.Fatalf("demo = %d, %v; want %d", got, err, want)
	}
	// Cached.
	if got, err := s.PythonSize(site, root, "demo", "1.0"); err != nil || got != want {
		t.Fatalf("cached demo = %d, %v", got, err)
	}

	write(t, site, "norecord-1.0.dist-info/METADATA", "x")
	if _, err := s.PythonSize(site, root, "norecord", "1.0"); err == nil {
		t.Fatal("dist-info without RECORD must be unknown")
	}
	write(t, site, "broken-1.0.dist-info/RECORD", "\"unterminated\n")
	if _, err := s.PythonSize(site, root, "broken", "1.0"); err == nil {
		t.Fatal("malformed RECORD must be unknown")
	}
}

// TestPythonSizeRecordSymlinkEscape checks that a RECORD entry reached through
// a directory symlink pointing outside the venv is not counted.
func TestPythonSizeRecordSymlinkEscape(t *testing.T) {
	root, site := venv(t)
	out := t.TempDir()
	write(t, out, "big.bin", strings.Repeat("b", 500))
	symlink(t, out, filepath.Join(site, "escape"))
	write(t, site, "demo-1.0.dist-info/RECORD", "escape/big.bin,,\ndemo.py,,\n")
	write(t, site, "demo.py", "ab")

	if got, err := NewScanner().PythonSize(site, root, "demo", "1.0"); err != nil || got != 2 {
		t.Fatalf("demo = %d, %v; want 2", got, err)
	}
}

func TestFindPythonSitePackages(t *testing.T) {
	projectDir := t.TempDir()
	venvRoot := filepath.Join(projectDir, ".venv")
	venvSite := filepath.Join(venvRoot, "lib", "python3.11", "site-packages")
	if err := os.MkdirAll(venvSite, 0755); err != nil {
		t.Fatal(err)
	}

	found, gotRoot, err := FindPythonSitePackages(projectDir, "")
	if err != nil || found != venvSite || gotRoot != venvRoot {
		t.Fatalf("FindPythonSitePackages = %q, %q, %v; want %q, %q", found, gotRoot, err, venvSite, venvRoot)
	}

	// Custom override
	customVenv := t.TempDir()
	customSite := filepath.Join(customVenv, "lib", "python3.10", "site-packages")
	if err := os.MkdirAll(customSite, 0755); err != nil {
		t.Fatal(err)
	}
	foundCustom, customRoot, err := FindPythonSitePackages(projectDir, customVenv)
	if err != nil || foundCustom != customSite || customRoot != customVenv {
		t.Fatalf("custom = %q, %q, %v", foundCustom, customRoot, err)
	}

	// Windows fallback (Lib/site-packages)
	winVenv := t.TempDir()
	winSite := filepath.Join(winVenv, "Lib", "site-packages")
	if err := os.MkdirAll(winSite, 0755); err != nil {
		t.Fatal(err)
	}
	foundWin, _, err := FindPythonSitePackages(projectDir, winVenv)
	if err != nil || !strings.EqualFold(foundWin, winSite) {
		t.Fatalf("foundWin %q, want %q (err: %v)", foundWin, winSite, err)
	}

	// Lib fallback without pythonX.Y (lib/site-packages)
	altVenv := t.TempDir()
	altSite := filepath.Join(altVenv, "lib", "site-packages")
	if err := os.MkdirAll(altSite, 0755); err != nil {
		t.Fatal(err)
	}
	foundAlt, _, err := FindPythonSitePackages(projectDir, altVenv)
	if err != nil || !strings.EqualFold(foundAlt, altSite) {
		t.Fatalf("foundAlt %q, want %q (err: %v)", foundAlt, altSite, err)
	}

	// Missing venv error
	if _, _, err := FindPythonSitePackages(projectDir, filepath.Join(projectDir, "nonexistent")); err == nil {
		t.Fatal("expected error for nonexistent venv")
	}

	// No site-packages in venv error
	emptyVenv := t.TempDir()
	if _, _, err := FindPythonSitePackages(projectDir, emptyVenv); err == nil {
		t.Fatal("expected error for venv with no site-packages")
	}
}
