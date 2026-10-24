package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := run("--version")
	if code != 0 || out != "depwhy dev\n" {
		t.Fatalf("got %d %q", code, out)
	}
}

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

var update = flag.Bool("update", false, "rewrite golden files")

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

func TestGolden(t *testing.T) {
	cases := [][]string{
		{"c"}, {"d"}, {"c@1.0.0"}, {"--remove", "a"}, {"--remove", "c"}, {"--dupes"},
		{"--prod", "c"}, {"--json", "d"}, {"zzz"}, {"--depth", "1", "d"},
	}
	for _, fixture := range []string{"npm", "pnpm", "uv", "cargo"} {
		dir := filepath.Join("..", "..", "testdata", "projects", fixture)
		if _, err := os.Stat(dir); err != nil {
			t.Logf("skipping %s: no fixture dir", fixture)
			continue
		}
		if entries, _ := os.ReadDir(dir); len(entries) == 0 {
			continue
		}
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
					os.MkdirAll(filepath.Dir(golden), 0o755)
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
