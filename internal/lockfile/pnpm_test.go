package lockfile

import (
	"reflect"
	"strings"
	"testing"
)

// TestPnpmSplitKey checks name/version splitting, including scoped names and peer suffixes.
func TestPnpmSplitKey(t *testing.T) {
	cases := []struct{ in, name, version string }{
		{"@babel/core@7.24.0(@types/node@20.0.0)", "@babel/core", "7.24.0"},
		{"react-dom@18.2.0(react@18.2.0)", "react-dom", "18.2.0"},
		{"a@1.0.0", "a", "1.0.0"},
		{"@x/y", "@x/y", ""},
	}
	for _, c := range cases {
		n, v := pnpmSplitKey(c.in)
		if n != c.name || v != c.version {
			t.Errorf("%q = %q,%q", c.in, n, v)
		}
	}
}

// TestParsePnpmMain checks the shared fixture, including the root label read from
// package.json.
func TestParsePnpmMain(t *testing.T) {
	g, warns, err := ParsePnpm("../../testdata/projects/pnpm/pnpm-lock.yaml", readFixture(t, "../../testdata/projects/pnpm/pnpm-lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	// Label comes from package.json next to the lockfile.
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app"}) {
		t.Fatalf("roots = %v", got)
	}
	want := []string{
		"a@1.0.0 -> c@1.0.0 prod",
		"a@1.0.0 -> d@1.0.0 prod",
		"b@1.0.0 -> c@2.0.0 prod",
		"b@1.0.0 -> d@1.0.0 prod",
		"root:app -> a@1.0.0 prod",
		"root:app -> b@1.0.0 dev",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
}

// TestParsePnpmPeersAndAliases checks that peer variants merge into one node and
// that an alias resolves to the real package.
func TestParsePnpmPeersAndAliases(t *testing.T) {
	p := "../../testdata/edge/pnpm-peers/pnpm-lock.yaml"
	g, warns, err := ParsePnpm(p, readFixture(t, p))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	want := []string{
		"react-dom@18.2.0 -> react@18.2.0 prod",
		"root:. -> react-dom@18.2.0 prod",
		"root:. -> react@18.2.0 prod",
		"root:. -> wrap-ansi@8.1.0 prod",
		"wrap-ansi@8.1.0 -> string-width@4.2.3 prod",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
}

// TestParsePnpmOldVersion checks the error for lockfile versions before 9.
func TestParsePnpmOldVersion(t *testing.T) {
	_, _, err := ParsePnpm("pnpm-lock.yaml", []byte("lockfileVersion: '6.0'\n"))
	want := "parse pnpm-lock.yaml: pnpm-lock.yaml lockfileVersion 6.0 is not supported; need 9.x (pnpm >= 9)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
}

// TestParsePnpmLinkAndFileRefs checks link: refs to other importers, file: refs,
// and the warnings for a bad link and a missing snapshot.
func TestParsePnpmLinkAndFileRefs(t *testing.T) {
	data := `lockfileVersion: '9.0'
importers:
  .:
    dependencies:
      lib:
        specifier: workspace:*
        version: link:packages/lib
      gone:
        specifier: workspace:*
        version: link:nowhere
      loc:
        specifier: file:../loc
        version: file:../loc
  packages/lib:
    dependencies:
      x:
        specifier: ^1
        version: 1.0.0
packages:
  x@1.0.0: {}
snapshots:
  x@1.0.0:
    dependencies:
      missing: 2.0.0
`
	g, warns, err := ParsePnpm("pnpm-lock.yaml", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:.", "root:packages/lib"}) {
		t.Fatalf("roots = %v", got)
	}
	want := []string{
		"root:. -> loc@file:../loc prod",
		"root:. -> root:packages/lib prod",
		"root:packages/lib -> x@1.0.0 prod",
		"x@1.0.0 -> missing@2.0.0 prod",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "link:nowhere") || !strings.Contains(joined, "missing@2.0.0") {
		t.Fatalf("warnings = %v", warns)
	}
}
