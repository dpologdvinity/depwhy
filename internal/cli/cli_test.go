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

// stageFixture copies a fixture dir into a temp dir. A trailing ".fixture" is
// dropped, so lockfiles can be stored as uv.lock.fixture and Cargo.lock.fixture.
func stageFixture(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
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
					t.Errorf("golden mismatch for %v\n--- got\n%s\n--- want\n%s", args, got, want)
				}
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
