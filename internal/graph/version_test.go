package graph

import (
	"sort"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	// Each row: a < b.
	less := []struct{ a, b string }{
		{"9.0.0", "10.0.0"},
		{"1.2.9", "1.2.10"},
		{"1.0", "1.0.1"},
		{"v1.9.0", "v1.10.0"},                      // Go
		{"v0.0.0-20210101000000-abcdef", "v0.0.0"}, // Go pseudo-version is a prerelease
		{"v0.0.0-20210101000000-abcdef", "v0.0.0-20220101000000-012345"},
		{"1.0.0-alpha", "1.0.0"}, // semver prerelease
		{"1.0.0-alpha.2", "1.0.0-alpha.10"},
		{"1.0.0-1", "1.0.0-alpha"}, // numeric identifiers sort first
		{"1.0.0-alpha", "1.0.0-beta"},
		{"1.0.0rc1", "1.0.0"}, // PEP 440 pre-release
		{"1.0a1", "1.0b1"},
		{"1.0.dev1", "1.0a1"}, // dev release before pre-release
		{"1.0", "1.0.post1"},  // post release after release
		{"1.0.post1", "1.0.1"},
		{"1!0.1", "2!0.0.1"}, // epochs
		{"9999", "1!0.1"},
		{"1.0.0", "1.0.0+build.1"}, // build metadata breaks ties only
		{"1.0", "1.0.0"},           // equal numerically, raw string breaks the tie
		{"abc", "abd"},
	}
	for _, c := range less {
		if got := compareVersions(c.a, c.b); got >= 0 {
			t.Errorf("compareVersions(%q, %q) = %d, want < 0", c.a, c.b, got)
		}
		if got := compareVersions(c.b, c.a); got <= 0 {
			t.Errorf("compareVersions(%q, %q) = %d, want > 0", c.b, c.a, got)
		}
	}
	for _, v := range []string{"", "1.0.0", "v2", "2!1.0rc1+local"} {
		if got := compareVersions(v, v); got != 0 {
			t.Errorf("compareVersions(%q, %q) = %d, want 0", v, v, got)
		}
	}
}

// TestCompareVersionsTotalOrder sorts a mixed list from several start orders and
// expects one result, which a non-transitive comparator would not guarantee.
func TestCompareVersionsTotalOrder(t *testing.T) {
	want := []string{"", "0.1", "1.0.0-1", "1.0.0-rc.1", "1.0.0rc1", "1.0", "1.0.0", "01.0.0", "1.0.0+b", "1.0.post1", "2.0.0", "10.0.0", "1!0.1", "latest"}
	sort.Slice(want, func(i, j int) bool { return compareVersions(want[i], want[j]) < 0 })
	for shift := 1; shift < len(want); shift++ {
		got := append(append([]string{}, want[shift:]...), want[:shift]...)
		sort.Slice(got, func(i, j int) bool { return compareVersions(got[i], got[j]) < 0 })
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("shift %d: got %v, want %v", shift, got, want)
			}
		}
	}
}

func TestVersionOrderInResults(t *testing.T) {
	b := NewBuilder("npm", "package-lock.json")
	r := b.Root("app", "")
	for _, v := range []string{"10.0.0", "9.0.0", "9.10.0"} {
		b.Edge(r, b.Node("a", v), Prod)
	}
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"9.0.0", "9.10.0", "10.0.0"}
	check := func(what string, ix []int) {
		t.Helper()
		if len(ix) != len(want) {
			t.Fatalf("%s: %v", what, ix)
		}
		for i, n := range ix {
			if g.Nodes[n].Version != want[i] {
				t.Errorf("%s[%d] = %s, want %s", what, i, g.Nodes[n].Version, want[i])
			}
		}
	}
	var dupes []int
	for _, v := range g.Dupes(false)[0].Versions {
		dupes = append(dupes, v.Node)
	}
	check("Dupes", dupes)
	check("Find", g.Find("a", ""))
	sorted := g.Find("a", "")
	sorted[0], sorted[2] = sorted[2], sorted[0]
	g.sortNodes(sorted)
	check("sortNodes", sorted)
	sorted[0], sorted[2] = sorted[2], sorted[0]
	sort.Slice(sorted, func(i, j int) bool { return g.less(sorted[i], sorted[j]) })
	check("less", sorted)

	var across []string
	for _, v := range DupesAcross([]*Graph{g}, false)[0].Versions {
		across = append(across, v.Version)
	}
	for i, v := range across {
		if v != want[i] {
			t.Errorf("DupesAcross[%d] = %s, want %s", i, v, want[i])
		}
	}
}
