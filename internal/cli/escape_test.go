package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// evilLock is a package-lock.json whose names and versions carry terminal
// control sequences: a screen clear (CSI), a window title and a clipboard write
// (OSC, ended by BEL), a carriage return and a right-to-left override.
const evilLock = `{"name":"app","version":"1.0.0","lockfileVersion":3,"packages":{
"":{"name":"app","version":"1.0.0","dependencies":{"evil":"*","a":"*","gone\u001b]0;title\u0007":"*"}},
"node_modules/evil":{"version":"1.0.0\u001b[2Jx\r\u202e"},
"node_modules/a":{"version":"1.0.0","dependencies":{"evil":"*"}},
"node_modules/a/node_modules/evil":{"version":"2.0.0"},
"node_modules/orphan\u001b]52;c;aGk=\u0007":{"version":"1.0.0"}}}`

// TestLockfileTextIsEscaped checks that lockfile-derived text cannot drive the
// terminal: control and invisible characters in names, versions, lockfile labels
// and warnings are printed as visible escapes in every text mode and in the
// stderr lines, while JSON keeps the exact strings.
func TestLockfileTextIsEscaped(t *testing.T) {
	root := t.TempDir()
	// Windows forbids control characters in file names, so there the nested
	// project's directory label carries no escape; its lockfile still does.
	subName := "sub\x1b[2J"
	if runtime.GOOS == "windows" {
		subName = "sub"
	}
	sub := filepath.Join(root, subName)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{root, sub} {
		if err := os.WriteFile(filepath.Join(d, "package-lock.json"), []byte(evilLock), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"evil"},
		{"--remove", "evil"},
		{"--dupes"},
		{"-r", "--dupes"},
		{"-r", "--dupes", "--across"},
		{"--heaviest"},
		{"evil@9"},
		{"evi"},
		{"orphan"},
	} {
		_, out, errs := run(append([]string{"-C", root, "--no-color"}, args...)...)
		all := out + errs
		for _, bad := range []string{"\x1b", "\a", "\r", "\u202e"} {
			if strings.Contains(all, bad) {
				t.Errorf("%v printed %q:\n%s", args, bad, all)
			}
		}
		if !strings.Contains(all, `\x1b`) {
			t.Errorf("%v shows no visible escape:\n%s", args, all)
		}
	}
	_, out, _ := run("-C", root, "evil")
	if !strings.Contains(out, `evil@1.0.0\x1b[2Jx\r\u202e`) {
		t.Errorf("version not escaped visibly:\n%s", out)
	}
	_, out, _ = run("-C", root, "--json", "evil")
	if !strings.Contains(out, "\"1.0.0\\u001b[2Jx\\r\u202e\"") || !strings.Contains(out, `orphan\u001b]52;c;aGk=\u0007@1.0.0 is not reachable`) {
		t.Errorf("JSON should keep the exact strings:\n%s", out)
	}
}
