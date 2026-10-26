package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMonorepoFlags(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--across", "shared"}, "--across needs --dupes"},
		{[]string{"--across", "--remove", "shared"}, "--across needs --dupes"},
		{[]string{"--across", "--heaviest"}, "--across needs --dupes"},
		{[]string{"-r", "-l", "package-lock.json", "shared"}, "--recursive cannot be combined with --lockfile"},
	} {
		code, out, errs := run(tt.args...)
		if code != 2 || out != "" || !strings.Contains(errs, tt.want) {
			t.Errorf("%v: got %d %q %q", tt.args, code, out, errs)
		}
	}
}

func TestMonorepoGolden(t *testing.T) {
	dir := stageFixture(t, project("monorepo"))
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"why", []string{"-r", "shared"}},
		{"why_json", []string{"--recursive", "--json", "shared"}},
		{"why_all_depth", []string{"-r", "--all", "--depth", "1", "shared"}},
		{"remove", []string{"-r", "--remove", "tool"}},
		{"remove_json", []string{"-r", "--remove", "--json", "tool"}},
		{"dupes", []string{"-r", "--dupes"}},
		{"dupes_json", []string{"-r", "--dupes", "--json"}},
		{"across", []string{"-r", "--dupes", "--across"}},
		{"across_json", []string{"-r", "--dupes", "--across", "--json"}},
		{"across_prod", []string{"-r", "--dupes", "--across", "--prod"}},
		{"across_prod_json", []string{"-r", "--dupes", "--across", "--prod", "--json"}},
		{"ecosystem", []string{"-r", "-e", "cargo", "shared"}},
		{"across_empty", []string{"-r", "-e", "python", "--dupes", "--across"}},
		{"across_empty_json", []string{"-r", "-e", "cargo", "--dupes", "--across", "--json"}},
		{"explicit", []string{"--dupes", "--across", "-l", filepath.Join(dir, "web", "package-lock.json"), "-l", filepath.Join(dir, "admin", "package-lock.json")}},
		{"explicit_json", []string{"--dupes", "--across", "--json", "-l", filepath.Join(dir, "web", "package-lock.json"), "-l", filepath.Join(dir, "admin", "package-lock.json")}},
		{"not_found", []string{"-r", "--json", "missing"}},
		{"heaviest", []string{"-r", "--heaviest"}},
		{"heaviest_json", []string{"-r", "--heaviest", "--json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"-C", dir, "--no-color"}, tt.args...)
			code, out, errs := run(args...)
			got := fmt.Sprintf("%s\n--- stderr\n%s\n--- exit=%d\n", out, strings.ReplaceAll(errs, dir, "<dir>"), code)
			checkGolden(t, "monorepo__"+tt.name+".txt", got)
			for i := 0; i < 3; i++ {
				code2, out2, errs2 := run(args...)
				if code2 != code || out2 != out || errs2 != errs {
					t.Fatal("nondeterministic output")
				}
			}
		})
	}
}

func TestRecursiveSizes(t *testing.T) {
	dir := stageFixture(t, project("monorepo"))
	for name, content := range map[string]string{
		"web/node_modules/tool/data":          "tool",
		"web/node_modules/shared/data":        "shared",
		"web/node_modules/only-across/data":   "only",
		"admin/node_modules/only-across/data": "admin-size",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out, errs := run("-C", dir, "-r", "--remove", "--json", "tool")
	if code != 0 || errs != "" || !strings.Contains(out, `"freed_size_bytes": 10`) {
		t.Fatalf("remove sizes = %d %s %s", code, out, errs)
	}
	code, out, errs = run("-C", dir, "-r", "--heaviest", "--json")
	if code != 0 || errs != "" || !strings.Contains(out, `"freed_size_bytes": 10`) || !strings.Contains(out, `"freed_size_bytes": 4`) {
		t.Fatalf("heaviest sizes = %d %s %s", code, out, errs)
	}
}

func TestRecursiveErrors(t *testing.T) {
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		code, out, errs := run("-C", dir, "-r", "shared")
		if code != 2 || out != "" || !strings.Contains(errs, "depwhy:") {
			t.Fatalf("got %d %q %q", code, out, errs)
		}
	}
}

func TestRecursiveWarningAndParsePaths(t *testing.T) {
	dir := stageFixture(t, project("monorepo"))
	// Detection priority warnings and parser warnings must identify the subproject.
	if err := os.WriteFile(filepath.Join(dir, "web", "bun.lock"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := run("-C", dir, "-r", "--json", "shared")
	if code != 0 || !strings.Contains(errs, "web: both bun.lock and package-lock.json") || !strings.Contains(out, `"warnings": [`) {
		t.Fatalf("priority warning = %d %s %s", code, out, errs)
	}
	if err := os.WriteFile(filepath.Join(dir, "web", "bun.lock"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs = run("-C", dir, "-r", "shared")
	if code != 2 || out != "" || !strings.Contains(errs, filepath.Join("web", "bun.lock")) {
		t.Fatalf("parse error = %d %q %s", code, out, errs)
	}
}

func TestAcrossOnlineAndOffline(t *testing.T) {
	f := useFakeOnline(t)
	dir := stageFixture(t, project("monorepo"))
	code, out, errs := run("-C", dir, "-r", "--dupes", "--across")
	if code != 0 || errs != "" || f.requests.Load() != 0 || strings.Contains(out, "advisories") {
		t.Fatalf("offline = %d %s %s", code, out, errs)
	}
	code, out, errs = run("-C", dir, "-r", "--dupes", "--across", "--online", "--json")
	if code != 0 || errs != "" || f.requests.Load() == 0 || !strings.Contains(out, `"advisories": []`) || !strings.Contains(out, `"across": true`) {
		t.Fatalf("online = %d %s %s", code, out, errs)
	}
	for _, name := range []string{"tool", "dev-tool"} {
		if _, ok := f.queried.Load(name); ok {
			t.Errorf("online queried non-duplicated lead %s", name)
		}
	}
	for _, name := range []string{"shared", "only-across"} {
		if _, ok := f.queried.Load(name); !ok {
			t.Errorf("online missed duplicated package %s", name)
		}
	}
	// Refresh also exercises the text path with fresh findings.
	code, out, errs = run("-C", dir, "-r", "--dupes", "--across", "--online", "--refresh")
	if code != 0 || errs != "" || !strings.Contains(out, "3 versions across 2 lockfiles") {
		t.Fatalf("online text = %d %s %s", code, out, errs)
	}
}
