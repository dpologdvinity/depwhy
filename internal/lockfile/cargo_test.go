package lockfile

import (
	"reflect"
	"testing"
)

// Same logical graph as testdata/projects/cargo/Cargo.lock.
const cargoMain = `
version = 4

[[package]]
name = "a"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
dependencies = ["c 1.0.0", "d"]

[[package]]
name = "app"
version = "0.1.0"
dependencies = ["a", "b"]

[[package]]
name = "b"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
dependencies = ["c 2.0.0", "d"]

[[package]]
name = "c"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "c"
version = "2.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "d"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
`

func TestParseCargoMain(t *testing.T) {
	g, warns, err := ParseCargo("Cargo.lock", []byte(cargoMain))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if got := rootIDs(g); !reflect.DeepEqual(got, []string{"root:app"}) {
		t.Fatalf("roots = %v", got)
	}
	want := []string{
		"a@1.0.0 -> c@1.0.0 prod",
		"a@1.0.0 -> d@1.0.0 prod",
		"b@1.0.0 -> c@2.0.0 prod",
		"b@1.0.0 -> d@1.0.0 prod",
		"root:app -> a@1.0.0 prod",
		"root:app -> b@1.0.0 prod",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges:\n%v", got)
	}
	if g.Ecosystem != "cargo" {
		t.Fatal(g.Ecosystem)
	}
}

func TestParseCargoAmbiguousName(t *testing.T) {
	data := `
[[package]]
name = "app"
version = "0.1.0"
dependencies = ["c"]

[[package]]
name = "c"
version = "1.0.0"
source = "registry+x"

[[package]]
name = "c"
version = "2.0.0"
source = "registry+x"
`
	g, warns, err := ParseCargo("Cargo.lock", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	wantW := []string{"ambiguous dependency c of app; picked 1.0.0", "c@2.0.0 is not reachable from any root"}
	if !reflect.DeepEqual(warns, wantW) {
		t.Fatalf("warns = %v", warns)
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, []string{"root:app -> c@1.0.0 prod"}) {
		t.Fatalf("edges = %v", got)
	}
}

func TestParseCargoSourceDisambiguates(t *testing.T) {
	data := `
[[package]]
name = "app"
version = "0.1.0"
dependencies = ["c 1.0.0 (git+https://example.com/c)"]

[[package]]
name = "c"
version = "1.0.0"
source = "registry+x"

[[package]]
name = "c"
version = "1.0.0"
source = "git+https://example.com/c"
`
	g, warns, err := ParseCargo("Cargo.lock", []byte(data))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, []string{"root:app -> c@1.0.0 prod"}) {
		t.Fatalf("edges = %v", got)
	}
}

func TestParseCargoMissingVersionKeyAndOldFormat(t *testing.T) {
	data := `
[[package]]
name = "app"
dependencies = ["x"]

[[package]]
name = "x"
source = "registry+x"
`
	g, warns, err := ParseCargo("Cargo.lock", []byte(data))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, []string{"root:app -> x@ prod"}) {
		t.Fatalf("edges = %v", got)
	}
}

func TestParseCargoUnknownVersionWarns(t *testing.T) {
	_, warns, err := ParseCargo("Cargo.lock", []byte("version = 9\n[[package]]\nname = \"app\"\nversion = \"1\"\n"))
	if err != nil || !reflect.DeepEqual(warns, []string{"unsupported Cargo.lock version 9; results may be wrong"}) {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
}
