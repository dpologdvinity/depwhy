package cli

import (
	"bytes"
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
