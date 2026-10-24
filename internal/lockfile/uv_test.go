package lockfile

import (
	"reflect"
	"testing"
)

// Same logical graph as testdata/projects/uv/uv.lock (c is single-version in python).
const uvMain = `
version = 1
revision = 2
requires-python = ">=3.12"

[[package]]
name = "app"
version = "0.1.0"
source = { virtual = "." }
dependencies = [{ name = "a" }]

[package.dev-dependencies]
dev = [{ name = "b" }]

[[package]]
name = "a"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }
dependencies = [{ name = "c" }, { name = "d" }]

[[package]]
name = "b"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }
dependencies = [{ name = "c" }, { name = "d" }]

[[package]]
name = "c"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "d"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }
`

func TestParseUVMain(t *testing.T) {
	g, warns, err := ParseUV("uv.lock", []byte(uvMain))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app"}) {
		t.Fatalf("roots = %v", got)
	}
	want := []string{
		"a@1.0.0 -> c@1.0.0 prod",
		"a@1.0.0 -> d@1.0.0 prod",
		"b@1.0.0 -> c@1.0.0 prod",
		"b@1.0.0 -> d@1.0.0 prod",
		"root:app -> a@1.0.0 prod",
		"root:app -> b@1.0.0 dev",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
	if g.Ecosystem != "python" {
		t.Fatal(g.Ecosystem)
	}
}

func TestParseUVWorkspaceMembers(t *testing.T) {
	data := `
version = 1
[manifest]
members = ["m1", "m2"]

[[package]]
name = "m1"
version = "0.1.0"
source = { editable = "m1" }
dependencies = [{ name = "m2" }, { name = "x" }]
[package.optional-dependencies]
extra = [{ name = "y" }]

[[package]]
name = "m2"
version = "0.1.0"
source = { editable = "m2" }
dependencies = [{ name = "x" }]

[[package]]
name = "x"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "y"
version = "2.0.0"
source = { registry = "https://pypi.org/simple" }
`
	g, warns, err := ParseUV("uv.lock", []byte(data))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:m1", "root:m2"}) {
		t.Fatalf("roots = %v", got)
	}
	want := []string{
		"root:m1 -> root:m2 prod",
		"root:m1 -> x@1.0.0 prod",
		"root:m1 -> y@2.0.0 optional",
		"root:m2 -> x@1.0.0 prod",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
}

func TestParseUVSameNameResolvesByVersion(t *testing.T) {
	data := `
version = 1
[[package]]
name = "app"
version = "0.1.0"
source = { virtual = "." }
dependencies = [{ name = "c", version = "2.0.0" }, { name = "z" }]

[[package]]
name = "z"
version = "1.0.0"
source = { registry = "r" }
dependencies = [{ name = "c", version = "1.0.0" }]

[[package]]
name = "c"
version = "1.0.0"
source = { registry = "r" }

[[package]]
name = "c"
version = "2.0.0"
source = { registry = "r" }
`
	g, warns, err := ParseUV("uv.lock", []byte(data))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	want := []string{"root:app -> c@2.0.0 prod", "root:app -> z@1.0.0 prod", "z@1.0.0 -> c@1.0.0 prod"}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
}

func TestParseUVAmbiguousAndVersionWarning(t *testing.T) {
	data := `
version = 2
[[package]]
name = "app"
version = "0.1.0"
source = { virtual = "." }
dependencies = [{ name = "c" }]

[[package]]
name = "c"
version = "1.0.0"
source = { registry = "r" }

[[package]]
name = "c"
version = "2.0.0"
source = { registry = "r" }
`
	g, warns, err := ParseUV("uv.lock", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	wantW := []string{"unsupported uv.lock version 2; results may be wrong", "ambiguous dependency c of app; picked 1.0.0", "c@2.0.0 is not reachable from any root"}
	if !reflect.DeepEqual(warns, wantW) {
		t.Fatalf("warns = %v", warns)
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, []string{"root:app -> c@1.0.0 prod"}) {
		t.Fatalf("edges = %v", got)
	}
}

func TestParseUVNoRoots(t *testing.T) {
	_, _, err := ParseUV("uv.lock", []byte("version = 1\n[[package]]\nname = \"x\"\nversion = \"1\"\nsource = { registry = \"r\" }\n"))
	if err == nil {
		t.Fatal("want error")
	}
}
