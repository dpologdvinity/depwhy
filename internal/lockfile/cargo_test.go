package lockfile

import (
	"reflect"
	"testing"
)

// TestParseCargoMain checks the shared fixture: roots, edges, and that every edge
// is prod because Cargo.lock has no dev information.
func TestParseCargoMain(t *testing.T) {
	g, warns, err := ParseCargo("Cargo.lock", readFixture(t, "../../testdata/projects/cargo/Cargo.lock.fixture"))
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

// TestParseCargoAmbiguousName checks that a dependency without a version warns and
// picks the first matching package.
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

// TestParseCargoSourceDisambiguates checks that the source in a dependency string
// selects between packages with the same name and version.
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

// TestParseCargoMissingVersionKeyAndOldFormat checks that a lockfile with no
// version key, and packages with no version, still parse.
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

// TestParseCargoUnknownVersionWarns checks that an unknown lockfile version warns
// but still parses.
func TestParseCargoUnknownVersionWarns(t *testing.T) {
	_, warns, err := ParseCargo("Cargo.lock", []byte("version = 9\n[[package]]\nname = \"app\"\nversion = \"1\"\n"))
	if err != nil || !reflect.DeepEqual(warns, []string{"unsupported Cargo.lock version 9; results may be wrong"}) {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
}

func TestParseCargoMetadata(t *testing.T) {
	data := []byte(`{
  "packages": [
    {
      "name": "my-app",
      "version": "0.1.0",
      "id": "my-app 0.1.0 (path+file:///my-app)",
      "source": null
    },
    {
      "name": "serde",
      "version": "1.0.197",
      "id": "serde 1.0.197 (registry+https://github.com/rust-lang/crates.io-index)",
      "source": "registry+https://github.com/rust-lang/crates.io-index"
    },
    {
      "name": "criterion",
      "version": "0.5.1",
      "id": "criterion 0.5.1 (registry+https://github.com/rust-lang/crates.io-index)",
      "source": "registry+https://github.com/rust-lang/crates.io-index"
    }
  ],
  "workspace_members": [
    "my-app 0.1.0 (path+file:///my-app)"
  ],
  "resolve": {
    "nodes": [
      {
        "id": "my-app 0.1.0 (path+file:///my-app)",
        "deps": [
          {
            "name": "serde",
            "pkg": "serde 1.0.197 (registry+https://github.com/rust-lang/crates.io-index)",
            "dep_kinds": [
              {
                "kind": null
              }
            ]
          },
          {
            "name": "criterion",
            "pkg": "criterion 0.5.1 (registry+https://github.com/rust-lang/crates.io-index)",
            "dep_kinds": [
              {
                "kind": "dev"
              }
            ]
          }
        ]
      },
      {
        "id": "serde 1.0.197 (registry+https://github.com/rust-lang/crates.io-index)",
        "deps": []
      },
      {
        "id": "criterion 0.5.1 (registry+https://github.com/rust-lang/crates.io-index)",
        "deps": []
      }
    ]
  }
}`)

	g, warns, err := ParseCargoMetadata("Cargo.lock", data)
	if err != nil {
		t.Fatalf("ParseCargoMetadata: %v", err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if g.Ecosystem != "cargo" {
		t.Errorf("ecosystem = %s, want cargo", g.Ecosystem)
	}
	if len(g.Roots) != 1 || g.Nodes[g.Roots[0]].Name != "my-app" {
		t.Fatalf("roots = %v, want [my-app]", g.Roots)
	}

	root := g.Roots[0]
	outKinds := map[string]string{}
	for _, e := range g.Out[root] {
		outKinds[g.Nodes[e.To].Name] = e.Kind.String()
	}

	if outKinds["serde"] != "prod" {
		t.Errorf("serde kind = %s, want prod", outKinds["serde"])
	}
	if outKinds["criterion"] != "dev" {
		t.Errorf("criterion kind = %s, want dev", outKinds["criterion"])
	}
}
