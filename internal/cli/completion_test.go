package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain lets the test binary stand in for the depwhy command: the shell tests
// below put a copy of it on PATH as "depwhy", and with DEPWHY_AS_CLI set it runs
// the CLI instead of the tests.
func TestMain(m *testing.M) {
	if os.Getenv("DEPWHY_AS_CLI") == "1" {
		os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// TestCompletionScripts checks that every script offers every visible flag and
// none of the hidden ones.
func TestCompletionScripts(t *testing.T) {
	for _, shell := range shells {
		code, out, errs := run("completion", shell)
		if code != 0 || errs != "" {
			t.Fatalf("%s: code=%d stderr=%q", shell, code, errs)
		}
		for _, flag := range []string{"dupes", "remove", "ecosystem", "lockfile", "online", "heaviest", "recursive", "version"} {
			if !strings.Contains(out, "--"+flag) && !strings.Contains(out, "-l "+flag) {
				t.Errorf("%s script lacks --%s", shell, flag)
			}
		}
		if strings.Count(out, completeFlag) != 1 {
			t.Errorf("%s script should call --%s exactly once:\n%s", shell, completeFlag, out)
		}
	}
}

// TestCompletionUsage checks the completion argument handling: an unknown shell
// is a usage error, and a lone "completion" is still a package query.
func TestCompletionUsage(t *testing.T) {
	code, _, errs := run("completion", "tcsh")
	if code != 2 || !strings.Contains(errs, "unknown shell tcsh (want bash, zsh, fish)") {
		t.Fatalf("unknown shell: code=%d stderr=%q", code, errs)
	}
	code, _, errs = run("-C", project("npm"), "completion")
	if code != 1 || !strings.Contains(errs, "completion not found") {
		t.Fatalf("lone completion: code=%d stderr=%q", code, errs)
	}
	_, help, _ := run("--help")
	if !strings.Contains(help, "depwhy completion bash|zsh|fish") || strings.Contains(help, completeFlag) {
		t.Fatalf("help should list the completion command and hide --%s:\n%s", completeFlag, help)
	}
}

// TestCompletePackages checks the hidden lister: names are sorted and unique,
// roots are left out, and the mode flags and arguments typed before the cursor
// are ignored, including --online, which must not reach the network.
func TestCompletePackages(t *testing.T) {
	f := useFakeOnline(t)
	code, out, errs := run("--"+completeFlag, "-C", project("npm"), "--online", "--remove", "--json", "a", "b")
	if code != 0 || errs != "" {
		t.Fatalf("code=%d stderr=%q", code, errs)
	}
	if out != "a\nb\nc\nd\n" {
		t.Fatalf("names: %q", out)
	}
	if f.requests.Load() != 0 {
		t.Fatalf("--%s made %d network requests", completeFlag, f.requests.Load())
	}
	if code, _, _ := run("--"+completeFlag, "-C", t.TempDir()); code != 2 {
		t.Fatalf("no lockfile: code=%d, want 2", code)
	}
}

// lookShell finds a shell for the completion tests. A missing shell skips the
// test, unless DEPWHY_REQUIRE_SHELLS=1 (set by CI on Linux, which installs all
// three), where a skip would silently hide the coverage.
func lookShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		if os.Getenv("DEPWHY_REQUIRE_SHELLS") == "1" {
			t.Fatalf("%s not installed, but DEPWHY_REQUIRE_SHELLS=1", name)
		}
		t.Skipf("%s not installed", name)
	}
	return path
}

// hostileLock is a package-lock.json whose package names would run a command if
// a completion script evaluated them.
const hostileLock = `{"name":"app","version":"1.0.0","lockfileVersion":3,"packages":{
"":{"name":"app","version":"1.0.0","dependencies":{"lodash":"1.0.0","@babel/core":"7.0.0","$(touch PWNED)":"1.0.0"}},
"node_modules/lodash":{"version":"1.0.0"},
"node_modules/@babel/core":{"version":"7.0.0"},
"node_modules/$(touch PWNED)":{"version":"1.0.0"}}}`

// shellProject writes hostileLock into a temp dir, puts the test binary on PATH as
// depwhy, and returns the project dir and the environment for the shell.
func shellProject(t *testing.T) (string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the completion scripts are for Unix shells")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(hostileLock), 0o644); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(self, filepath.Join(bin, "depwhy")); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "DEPWHY_AS_CLI=1", "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, env
}

// runShell runs script with the given shell in dir and fails the test if it fails
// or if the hostile package name was ever executed.
func runShell(t *testing.T, dir string, env []string, shell string, args ...string) string {
	t.Helper()
	cmd := exec.Command(shell, args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", shell, err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
		t.Fatalf("%s executed a package name from the lockfile", shell)
	}
	return string(out)
}

// TestBashCompletion sources the bash script and calls its completion function
// the way bash does on Tab.
func TestBashCompletion(t *testing.T) {
	bash := lookShell(t, "bash")
	dir, env := shellProject(t)
	_, script, _ := run("completion", "bash")
	if err := os.WriteFile(filepath.Join(dir, "depwhy.bash"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runShell(t, dir, env, bash, "-c", `
source ./depwhy.bash
comp() { COMP_WORDS=("$@"); COMP_CWORD=$(( $# - 1 )); COMPREPLY=(); _depwhy; echo "${COMPREPLY[*]}"; }
comp depwhy lo
comp depwhy @
comp depwhy ""
comp depwhy --dup
comp depwhy -e c
comp depwhy -C "$PWD" --json lo
`)
	want := `lodash
@babel/core
\$\(touch\ PWNED\) @babel/core lodash
--dupes
cargo
lodash
`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// TestZshCompletionSyntax checks that zsh parses the zsh script. Driving zsh's
// completion system needs a terminal, so the behavior is checked by hand.
func TestZshCompletionSyntax(t *testing.T) {
	zsh := lookShell(t, "zsh")
	dir, env := shellProject(t)
	_, script, _ := run("completion", "zsh")
	if err := os.WriteFile(filepath.Join(dir, "_depwhy"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	runShell(t, dir, env, zsh, "-n", "_depwhy")
}

// TestFishCompletion loads the fish script and asks fish what it would complete.
func TestFishCompletion(t *testing.T) {
	fish := lookShell(t, "fish")
	dir, env := shellProject(t)
	_, script, _ := run("completion", "fish")
	if err := os.WriteFile(filepath.Join(dir, "depwhy.fish"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runShell(t, dir, env, fish, "-c", `
source ./depwhy.fish
complete -C 'depwhy lo'
complete -C 'depwhy @'
complete -C 'depwhy --dup' | string replace -r '\t.*' ''
`)
	want := "lodash\n@babel/core\n--dupes\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}
