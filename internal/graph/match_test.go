package graph

import (
	"reflect"
	"testing"
)

func TestSplitQuery(t *testing.T) {
	cases := []struct{ in, name, version string }{
		{"lodash", "lodash", ""},
		{"lodash@4.17.21", "lodash", "4.17.21"},
		{"@babel/core", "@babel/core", ""},
		{"@babel/core@7.24.0", "@babel/core", "7.24.0"},
	}
	for _, c := range cases {
		n, v := SplitQuery(c.in)
		if n != c.name || v != c.version {
			t.Errorf("%q = %q,%q", c.in, n, v)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct{ eco, in, want string }{
		{"npm", "Lodash", "Lodash"},
		{"python", "Foo.Bar_baz", "foo-bar-baz"},
		{"python", "a__b--c..d", "a-b-c-d"},
		{"cargo", "Serde_Json", "serde-json"},
	}
	for _, c := range cases {
		if got := Normalize(c.eco, c.in); got != c.want {
			t.Errorf("%s %q = %q want %q", c.eco, c.in, got, c.want)
		}
	}
}

func buildSimple(t *testing.T, eco string, pkgs ...[2]string) *Graph {
	t.Helper()
	b := NewBuilder(eco, "lock")
	r := b.Root("app", "1")
	for _, p := range pkgs {
		b.Edge(r, b.Node(p[0], p[1]), Prod)
	}
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestFind(t *testing.T) {
	g := buildSimple(t, "python", [2]string{"Foo_Bar", "2.0"}, [2]string{"foo-bar", "1.0"}, [2]string{"other", "1"})
	ids := func(ix []int) []string {
		var out []string
		for _, i := range ix {
			out = append(out, g.Nodes[i].ID)
		}
		return out
	}
	if got := ids(g.Find("foo.bar", "")); !reflect.DeepEqual(got, []string{"foo-bar@1.0", "Foo_Bar@2.0"}) {
		t.Errorf("got %v", got)
	}
	if got := ids(g.Find("foo-bar", "2.0")); !reflect.DeepEqual(got, []string{"Foo_Bar@2.0"}) {
		t.Errorf("got %v", got)
	}
	if got := g.Find("app", ""); len(got) != 0 {
		t.Errorf("roots must not match: %v", got)
	}
	if got := g.Find("nope", ""); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestSuggestOrdering(t *testing.T) {
	g1 := buildSimple(t, "npm", [2]string{"lodash", "1"}, [2]string{"lodash.get", "1"}, [2]string{"lodas", "1"}, [2]string{"unrelated", "1"})
	g2 := buildSimple(t, "python", [2]string{"lodash", "1"}, [2]string{"lodashx", "1"})
	got := Suggest([]*Graph{g1, g2}, "lodahs")
	// All three are distance 2 from lodahs, so they sort by name.
	want := []string{"lodas", "lodash", "lodashx"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := Suggest([]*Graph{g1}, "zzzzzz"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	// Distance 1 beats distance 2, and the list caps at 3.
	g3 := buildSimple(t, "npm", [2]string{"abcd", "1"}, [2]string{"abce", "1"}, [2]string{"abxx", "1"}, [2]string{"abcdef", "1"}, [2]string{"abcf", "1"})
	if got, want := Suggest([]*Graph{g3}, "abcg"), []string{"abcd", "abce", "abcf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
