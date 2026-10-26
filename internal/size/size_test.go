package size

import (
	"os"
	"path/filepath"
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

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644); err != nil { // 5 bytes
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("world!"), 0644); err != nil { // 6 bytes
		t.Fatal(err)
	}

	s := NewScanner()
	got, err := s.DirSize(dir)
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	if got != 11 {
		t.Fatalf("DirSize = %d, want 11", got)
	}

	// Repeated call hits cache
	got2, err := s.DirSize(dir)
	if err != nil || got2 != 11 {
		t.Fatalf("cached DirSize = %d, err = %v", got2, err)
	}

	// Non-existent directory returns error
	_, err = s.DirSize(filepath.Join(dir, "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestDirSizeIgnoresSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "huge.bin"), make([]byte, 10000), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("abc"), 0644); err != nil { // 3 bytes
		t.Fatal(err)
	}
	// Symlink pointing outside
	link := filepath.Join(dir, "link-outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	s := NewScanner()
	got, err := s.DirSize(dir)
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	if got != 3 {
		t.Fatalf("DirSize with symlink = %d, want 3", got)
	}
}

func TestNPMSize(t *testing.T) {
	projectDir := t.TempDir()
	pkgDir := filepath.Join(projectDir, "node_modules", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte("console.log('hi');"), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner()
	size, err := s.NPMSize(projectDir, "node_modules/foo")
	if err != nil {
		t.Fatalf("NPMSize: %v", err)
	}
	if size != 18 {
		t.Fatalf("NPMSize = %d, want 18", size)
	}

	// Missing package returns error
	_, err = s.NPMSize(projectDir, "node_modules/bar")
	if err == nil {
		t.Fatal("expected error for missing npm package")
	}
}

func TestPythonSizeWithRecord(t *testing.T) {
	sitePackages := t.TempDir()
	distInfo := filepath.Join(sitePackages, "demo_pkg-1.0.0.dist-info")
	pkgDir := filepath.Join(sitePackages, "demo_pkg")
	if err := os.MkdirAll(distInfo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	file1 := filepath.Join(pkgDir, "__init__.py")
	file2 := filepath.Join(distInfo, "METADATA")
	if err := os.WriteFile(file1, []byte("x = 1\n"), 0644); err != nil { // 6 bytes
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("Metadata-Version: 2.1\n"), 0644); err != nil { // 22 bytes
		t.Fatal(err)
	}

	// RECORD lists relative paths from site-packages
	recordContent := "demo_pkg/__init__.py,sha256=...,6\ndemo_pkg-1.0.0.dist-info/METADATA,sha256=...,22\ndemo_pkg-1.0.0.dist-info/RECORD,,\n"
	recordFile := filepath.Join(distInfo, "RECORD")
	if err := os.WriteFile(recordFile, []byte(recordContent), 0644); err != nil { // len(recordContent)
		t.Fatal(err)
	}

	s := NewScanner()
	size, err := s.PythonSize(sitePackages, "demo-pkg", "1.0.0")
	if err != nil {
		t.Fatalf("PythonSize: %v", err)
	}
	want := int64(6 + 22 + len(recordContent))
	if size != want {
		t.Fatalf("PythonSize = %d, want %d", size, want)
	}
}

func TestFindPythonSitePackages(t *testing.T) {
	projectDir := t.TempDir()
	venvSite := filepath.Join(projectDir, ".venv", "lib", "python3.11", "site-packages")
	if err := os.MkdirAll(venvSite, 0755); err != nil {
		t.Fatal(err)
	}

	found, err := FindPythonSitePackages(projectDir, "")
	if err != nil {
		t.Fatalf("FindPythonSitePackages: %v", err)
	}
	if found != venvSite {
		t.Fatalf("found %q, want %q", found, venvSite)
	}

	// Custom override
	customVenv := t.TempDir()
	customSite := filepath.Join(customVenv, "lib", "python3.10", "site-packages")
	if err := os.MkdirAll(customSite, 0755); err != nil {
		t.Fatal(err)
	}
	foundCustom, err := FindPythonSitePackages(projectDir, customVenv)
	if err != nil {
		t.Fatalf("FindPythonSitePackages custom: %v", err)
	}
	if foundCustom != customSite {
		t.Fatalf("foundCustom %q, want %q", foundCustom, customSite)
	}
}
