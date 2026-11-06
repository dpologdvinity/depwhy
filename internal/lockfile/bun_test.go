package lockfile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestParseBunSharedFixture checks that the real `bun install` output in
// testdata/projects/bun encodes the same logical graph as the npm fixture.
func TestParseBunSharedFixture(t *testing.T) {
	const lock = "../../testdata/projects/bun/bun.lock"
	bg, warns, err := ParseBun(lock, readFixture(t, lock))
	if err != nil {
		t.Fatalf("ParseBun: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	ng, _, err := ParseNPM("package-lock.json", readFixture(t, "../../testdata/projects/npm/package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bg.Ecosystem != "npm" {
		t.Errorf("ecosystem = %s, want npm", bg.Ecosystem)
	}
	if got, want := rootIDs(bg), rootIDs(ng); !reflect.DeepEqual(got, want) {
		t.Fatalf("roots = %v, want %v", got, want)
	}
	if got, want := edgeStrings(bg), edgeStrings(ng); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges = %v\nwant %v", got, want)
	}
	// bun.lock omits the project's own version; it comes from package.json.
	if got := bg.Nodes[bg.Roots[0]].Version; got != "1.0.0" {
		t.Fatalf("root version = %q, want 1.0.0 from package.json", got)
	}
}

// TestParseBunRealEdgeCases parses a real Bun lockfile with a workspace member,
// a scoped package, an npm alias, file: and tarball dependencies, optional and
// peer edges, and three nested copies of an older version.
func TestParseBunRealEdgeCases(t *testing.T) {
	g, warns, err := ParseBun("bun.lock", readFixture(t, "../../testdata/edge/bun/bun.lock"))
	if err != nil {
		t.Fatalf("ParseBun: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:root", "root:lib"}) {
		t.Fatalf("roots = %v", got)
	}
	tb := "a@http://127.0.0.1:4873/a/-/a-1.0.0.tgz"
	want := []string{
		"@s/e@1.0.0 -> c@1.0.0 prod",
		tb + " -> c@1.0.0 prod",
		tb + " -> d@1.0.0 prod",
		"f@1.0.0 -> d@1.0.0 peer",
		"f@1.0.0 -> g@1.0.0 optional",
		"localdep@file:localdep -> d@1.0.0 prod",
		"root:lib -> @s/e@1.0.0 prod",
		"root:lib -> c@1.0.0 prod",
		"root:lib -> d@1.0.0 peer",
		"root:root -> " + tb + " prod",
		"root:root -> c@1.0.0 prod",
		"root:root -> c@2.0.0 prod",
		"root:root -> d@1.0.0 peer",
		"root:root -> f@1.0.0 prod",
		"root:root -> g@1.0.0 optional",
		"root:root -> localdep@file:localdep prod",
		"root:root -> root:lib prod",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestParseBunInline covers JSONC syntax, nested install paths, walking up from
// a scoped parent without matching a sibling scoped package, and deterministic
// warnings for dependencies with no matching key.
func TestParseBunInline(t *testing.T) {
	data := []byte(`// bun.lock
{
  /* block
     comment */
  "lockfileVersion": 1,
  "workspaces": {
    "": {
      "name": "app",
      "dependencies": { "@s/e": "^1.0.0", "z1": "^1", "z2": "^1", "z3": "^1", }, // trailing comma
      "devDependencies": { "zdev": "^1", },
      "optionalDependencies": { "zopt": "^1", },
    },
  },
  "packages": {
    "@s/e": ["@s/e@1.0.0", "", { "dependencies": { "x": "^1.0.0", "y": "^2.0.0", "zmissing": "^1", }, }, "sha512-//,}"],
    "@s/x": ["@s/x@9.0.0", "", {}, "sha512-"],
    "x": ["x@1.0.0", "", {}, "sha512-"],
    "y": ["y@1.0.0", "", {}, "sha512-"],
    "@s/e/y": ["y@2.0.0", "", {}, "sha512-"],
  },
}
`)
	var first []string
	for i := 0; i < 20; i++ {
		g, warns, err := ParseBun("bun.lock", data)
		if err != nil {
			t.Fatalf("ParseBun: %v", err)
		}
		want := []string{
			"@s/e@1.0.0 -> x@1.0.0 prod",
			"@s/e@1.0.0 -> y@2.0.0 prod",
			"root:app -> @s/e@1.0.0 prod",
		}
		if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
			t.Fatalf("edges = %v", got)
		}
		if i == 0 {
			first = warns
			continue
		}
		if !reflect.DeepEqual(warns, first) {
			t.Fatalf("warnings changed between runs: %v vs %v", warns, first)
		}
	}
	want := []string{
		"unresolved dependency z1 of app",
		"unresolved dependency z2 of app",
		"unresolved dependency z3 of app",
		"unresolved dependency zdev of app",
		"unresolved dependency zmissing of @s/e",
		"@s/x@9.0.0 is not reachable from any root",
		"y@1.0.0 is not reachable from any root",
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("warnings =\n%s\nwant\n%s", strings.Join(first, "\n"), strings.Join(want, "\n"))
	}
}

// TestStripJSONC checks that comments and trailing commas are removed only
// outside strings, so "//", "/*" and ",}" inside values survive.
func TestStripJSONC(t *testing.T) {
	in := `{"a": "x // y", "b": "/* not */", "c": "q\",}", "d": [1, 2, ], /* c */ "e": "\\", // end
}`
	var got map[string]any
	if err := json.Unmarshal(stripJSONC([]byte(in)), &got); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, stripJSONC([]byte(in)))
	}
	want := map[string]any{"a": "x // y", "b": "/* not */", "c": `q",}`, "d": []any{1.0, 2.0}, "e": `\`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestParseBunMalformed checks errors for invalid JSON and package entries that
// are not arrays starting with a "name@version" string, and the fallback root
// for a lockfile with no workspaces.
func TestParseBunMalformed(t *testing.T) {
	for _, in := range []string{
		`not json`,
		`{"packages": {"a": {"name": "a"}}}`,
		`{"packages": {"a": []}}`,
		`{"packages": {"a": [1]}}`,
	} {
		if _, _, err := ParseBun("bun.lock", []byte(in)); err == nil || !strings.Contains(err.Error(), "bun.lock") {
			t.Errorf("%s: err = %v", in, err)
		}
	}
	g, _, err := ParseBun("bun.lock", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app"}) {
		t.Fatalf("roots = %v", got)
	}
}
