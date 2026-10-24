package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

// run calls Run with the given arguments and returns the exit code, stdout and stderr.
func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// TestVersion checks that --version prints the build version and exits 0.
func TestVersion(t *testing.T) {
	code, out, _ := run("--version")
	if code != 0 || out != "depwhy dev\n" {
		t.Fatalf("got %d %q", code, out)
	}
}

// TestUsageErrors checks that invalid invocations exit 2 with a "depwhy:" message.
func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--remove", "--dupes"},
		{"--remove"},
		{"--dupes", "x"},
		{"a", "b"},
		{"--bogus"},
		{"-e", "ruby", "x"},
	} {
		code, _, errs := run(args...)
		if code != 2 || !strings.Contains(errs, "depwhy:") {
			t.Errorf("%v: got %d %q", args, code, errs)
		}
	}
}

// update rewrites the golden files: go test ./internal/cli -update.
var update = flag.Bool("update", false, "rewrite golden files")

// nonAlnum strips punctuation from arguments when deriving golden file names.
var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// checkGolden compares got with testdata/golden/<name>, or rewrites the file
// when -update is set.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("..", "..", "testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden (run with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// stageFixture copies a fixture dir into a temp dir. A trailing ".fixture" is
// dropped, so lockfiles can be stored as uv.lock.fixture and Cargo.lock.fixture.
func stageFixture(t *testing.T, src string) string {
	t.Helper()
	return stageFixtures(t, src)
}

// stageFixtures is stageFixture for several fixture dirs merged into one temp
// dir, which makes a polyglot project.
func stageFixtures(t *testing.T, srcs ...string) string {
	t.Helper()
	dst := t.TempDir()
	for _, src := range srcs {
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			name := strings.TrimSuffix(e.Name(), ".fixture")
			if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dst
}

// TestGolden runs the CLI end to end against every fixture project and compares
// stdout, stderr and the exit code with the files in testdata/golden. The goldens
// are the executable form of the spec, so review them by hand after -update.
func TestGolden(t *testing.T) {
	cases := [][]string{
		{"c"}, {"d"}, {"c@1.0.0"}, {"--remove", "a"}, {"--remove", "c"}, {"--dupes"},
		{"--prod", "c"}, {"--json", "d"}, {"zzz"}, {"--depth", "1", "d"},
	}
	for _, fixture := range []string{"npm", "pnpm", "uv", "cargo"} {
		src := filepath.Join("..", "..", "testdata", "projects", fixture)
		if _, err := os.Stat(src); err != nil {
			t.Fatalf("missing fixture %s", fixture)
		}
		dir := stageFixture(t, src)
		for _, args := range cases {
			var parts []string
			for _, a := range args {
				parts = append(parts, nonAlnum.ReplaceAllString(a, ""))
			}
			name := fixture + "__" + strings.Join(parts, "_") + ".txt"
			t.Run(name, func(t *testing.T) {
				code, out, errs := run(append([]string{"-C", dir, "--no-color"}, args...)...)
				got := fmt.Sprintf("%s\n--- stderr\n%s\n--- exit=%d\n", out, errs, code)
				checkGolden(t, name, got)
			})
		}
	}
}

// TestResolveVersion checks the order of preference for the reported version:
// ldflags, then the module version embedded by go install, then "dev".
func TestResolveVersion(t *testing.T) {
	info := func(v string) *debug.BuildInfo { return &debug.BuildInfo{Main: debug.Module{Version: v}} }
	cases := []struct {
		ldflags string
		info    *debug.BuildInfo
		want    string
	}{
		{"1.2.3", info("v9.9.9"), "1.2.3"}, // release build: ldflags win
		{"dev", info("v0.1.0"), "v0.1.0"},  // go install pkg@version
		{"dev", info("(devel)"), "dev"},    // local build
		{"dev", info(""), "dev"},
		{"dev", nil, "dev"},
	}
	for _, c := range cases {
		if got := resolveVersion(c.ldflags, c.info); got != c.want {
			t.Errorf("resolveVersion(%q, %v) = %q, want %q", c.ldflags, c.info, got, c.want)
		}
	}
}

// project returns the path of a fixture project under testdata/projects.
func project(name string) string {
	return filepath.Join("..", "..", "testdata", "projects", name)
}

// TestHelp checks that --help prints usage and the cargo/marker notes and exits 0.
func TestHelp(t *testing.T) {
	code, out, _ := run("--help")
	if code != 0 || !strings.Contains(out, "usage:") || !strings.Contains(out, "notes:") || !strings.Contains(out, "--remove") {
		t.Fatalf("got %d %q", code, out)
	}
}

// TestNoLockfile checks exit 2 and the message when a directory has no lockfile.
func TestNoLockfile(t *testing.T) {
	code, _, errs := run("-C", t.TempDir(), "x")
	if code != 2 || !strings.Contains(errs, "depwhy: no supported lockfile in") {
		t.Fatalf("got %d %q", code, errs)
	}
}

// TestUnparsableLockfile checks that a broken lockfile is fatal (exit 2) and the
// error names the file.
func TestUnparsableLockfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := run("-C", dir, "x")
	if code != 2 || !strings.Contains(errs, "parse ") || !strings.Contains(errs, "package-lock.json") {
		t.Fatalf("got %d %q", code, errs)
	}
}

// TestUnreadableExplicitLockfile checks that a missing -l file exits 2.
func TestUnreadableExplicitLockfile(t *testing.T) {
	code, _, errs := run("-l", filepath.Join(t.TempDir(), "uv.lock"), "x")
	if code != 2 || !strings.Contains(errs, "depwhy:") {
		t.Fatalf("got %d %q", code, errs)
	}
	if code, _, errs = run("-l", "whatever/yarn.lock", "x"); code != 2 || !strings.Contains(errs, "unrecognized lockfile name: yarn.lock") {
		t.Fatalf("got %d %q", code, errs)
	}
}

// TestExplicitLockfile checks that -l bypasses detection and the working directory.
func TestExplicitLockfile(t *testing.T) {
	dir := stageFixture(t, project("npm"))
	code, out, _ := run("-l", filepath.Join(dir, "package-lock.json"), "a")
	if code != 0 || !strings.Contains(out, "direct dependency of app (prod)") {
		t.Fatalf("got %d %q", code, out)
	}
}

// TestPolyglotLoadsEveryEcosystem checks that a directory with several lockfiles
// is searched in full, in load order, and that -e narrows it.
func TestPolyglotLoadsEveryEcosystem(t *testing.T) {
	dir := stageFixtures(t, project("npm"), project("uv"), project("cargo"))
	code, out, _ := run("-C", dir, "--no-color", "d")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	i, j, k := strings.Index(out, "npm · package-lock.json"), strings.Index(out, "python · uv.lock"), strings.Index(out, "cargo · Cargo.lock")
	if i < 0 || j < i || k < j {
		t.Fatalf("want npm, python, cargo in order:\n%s", out)
	}
	_, out, _ = run("-C", dir, "--no-color", "-e", "cargo", "d")
	if strings.Contains(out, "npm ·") || !strings.Contains(out, "cargo · Cargo.lock") {
		t.Fatalf("ecosystem filter failed:\n%s", out)
	}
	if code, _, _ = run("-C", dir, "-e", "python", "--dupes"); code != 0 {
		t.Fatalf("dupes exit %d", code)
	}
}

// TestPnpmPreferredWithWarning checks the pnpm-over-npm rule and that warnings go
// to stderr without changing the exit code.
func TestPnpmPreferredWithWarning(t *testing.T) {
	dir := stageFixtures(t, project("npm"), project("pnpm"))
	code, out, errs := run("-C", dir, "--no-color", "a")
	if code != 0 || !strings.Contains(out, "pnpm-lock.yaml") || strings.Contains(out, "package-lock.json") {
		t.Fatalf("got %d %q", code, out)
	}
	if !strings.Contains(errs, "depwhy: warning: both pnpm-lock.yaml and package-lock.json found; using pnpm-lock.yaml") {
		t.Fatalf("stderr = %q", errs)
	}
}

// TestParserWarningsArePrefixedWithTheFile checks that a parser warning names its
// lockfile, so output stays clear when several are loaded.
func TestParserWarningsArePrefixedWithTheFile(t *testing.T) {
	dir := t.TempDir()
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"gone":"*","a":"*"}},"node_modules/a":{"version":"1.0.0"}}}`
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := run("-C", dir, "a")
	if code != 0 || !strings.Contains(errs, "depwhy: warning: package-lock.json: unresolved dependency gone of .") {
		t.Fatalf("got %d %q", code, errs)
	}
}

// TestNotFoundSuggests checks exit 1, the message, and the did-you-mean line.
func TestNotFoundSuggests(t *testing.T) {
	code, out, errs := run("-C", project("npm"), "cc")
	if code != 1 || out != "" || !strings.Contains(errs, "depwhy: cc not found in package-lock.json") || !strings.Contains(errs, "did you mean: c, a, b") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}

// TestNotFoundJSON checks that a not-found run in JSON mode still emits a
// document, with suggestions, and exits 1.
func TestNotFoundJSON(t *testing.T) {
	code, out, _ := run("-C", project("npm"), "--json", "cc")
	if code != 1 || !strings.Contains(out, `"results": []`) || !strings.Contains(out, `"suggestions": [`) {
		t.Fatalf("got %d %q", code, out)
	}
}

// TestProdHidesDevOnlyTarget checks that --prod treats a dev-only package as not
// found and says why.
func TestProdHidesDevOnlyTarget(t *testing.T) {
	code, _, errs := run("-C", project("npm"), "--prod", "b")
	if code != 1 || !strings.Contains(errs, "(found only via dev dependencies; drop --prod)") {
		t.Fatalf("got %d %q", code, errs)
	}
	if code, _, _ = run("-C", project("npm"), "--prod", "--remove", "b"); code != 1 {
		t.Fatalf("--remove exit %d", code)
	}
}

// TestJSONModes checks that remove and dupes JSON documents are produced.
func TestJSONModes(t *testing.T) {
	_, out, _ := run("-C", project("npm"), "--json", "--remove", "a")
	if !strings.Contains(out, `"mode": "remove"`) || !strings.Contains(out, `"freed"`) {
		t.Fatalf("remove: %s", out)
	}
	_, out, _ = run("-C", project("npm"), "--json", "--dupes")
	if !strings.Contains(out, `"mode": "dupes"`) || !strings.Contains(out, `"query": null`) {
		t.Fatalf("dupes: %s", out)
	}
}

// TestRepeatedFlagsAndQueryAfterFlags checks that flags may follow the package
// argument (GNU style).
func TestRepeatedFlagsAndQueryAfterFlags(t *testing.T) {
	code, out, _ := run("d", "-C", project("npm"), "--no-color", "--depth", "1")
	if code != 0 || !strings.Contains(out, "…") {
		t.Fatalf("got %d %q", code, out)
	}
}
