package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// notesFor builds Notes for the npm fixture from node IDs.
func notesFor(t *testing.T, g *graph.Graph, byID map[string]Note) Notes {
	t.Helper()
	n := Notes{}
	for id, note := range byID {
		n[find(t, g, id)] = note
	}
	return n
}

// TestWhyTextShowsNotes checks the spec's example layout: findings follow the
// package after two spaces, on the heading and on tree lines, and roots are never
// annotated.
func TestWhyTextShowsNotes(t *testing.T) {
	g := npmGraph(t)
	s := Style{Notes: notesFor(t, g, map[string]Note{
		"d@1.0.0": {Advisories: []string{"GHSA-1", "GHSA-2"}},
		"a@1.0.0": {Deprecated: "use b instead"},
		"b@1.0.0": {Yanked: true, Advisories: []string{"RUSTSEC-9"}},
	})}
	var buf bytes.Buffer
	WhyText(&buf, g, g.Why(find(t, g, "d@1.0.0"), graph.WhyOpts{}), s)
	want := `d@1.0.0  npm · package-lock.json  ⚠ GHSA-1, GHSA-2
├── a@1.0.0  [deprecated: use b instead]
│   └── app (root, prod)
└── b@1.0.0  ⚠ RUSTSEC-9  [yanked]
    └── app (root, dev)
pulled in by 2 direct dependencies: a (prod), b (dev)
`
	if buf.String() != want {
		t.Fatalf("got:\n%s", buf.String())
	}
}

// TestNoteTextIsTidied checks that a long or multi-line deprecation message is
// collapsed to one short line.
func TestNoteTextIsTidied(t *testing.T) {
	long := "line one\n\n  line   two " + strings.Repeat("x", 200)
	got := Style{Notes: Notes{0: {Deprecated: long}}}.suffix(0)
	if strings.ContainsAny(got, "\n\t") || !strings.HasPrefix(got, "  [deprecated: line one line two x") || !strings.HasSuffix(got, "…]") {
		t.Fatalf("got %q", got)
	}
	if n := len([]rune(got)); n > 120 {
		t.Fatalf("suffix too long (%d runes)", n)
	}
	if (Style{}).suffix(0) != "" || (Style{Notes: Notes{}}).suffix(0) != "" {
		t.Fatal("no notes must give no suffix")
	}
}

// TestNotesColor checks advisories are red and deprecated/yanked are yellow.
func TestNotesColor(t *testing.T) {
	s := Style{Color: true, Notes: Notes{0: {Advisories: []string{"GHSA-1"}, Deprecated: "old", Yanked: true}}}
	got := s.suffix(0)
	for _, want := range []string{"\x1b[31m⚠ GHSA-1\x1b[0m", "\x1b[33m[deprecated: old]\x1b[0m", "\x1b[33m[yanked]\x1b[0m"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

// TestRemoveAndDupesTextShowNotes checks that freed, kept and duplicate-version
// lines carry notes.
func TestRemoveAndDupesTextShowNotes(t *testing.T) {
	g := npmGraph(t)
	s := Style{Notes: notesFor(t, g, map[string]Note{
		"a@1.0.0": {Deprecated: "gone"}, "d@1.0.0": {Advisories: []string{"GHSA-d"}}, "c@2.0.0": {Yanked: true},
	})}
	var buf bytes.Buffer
	RemoveText(&buf, g, g.Remove(find(t, g, "a@1.0.0"), false), s)
	for _, want := range []string{"  a@1.0.0  [deprecated: gone]\n", "  d@1.0.0  ⚠ GHSA-d  via b@1.0.0\n"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("remove output missing %q:\n%s", want, buf.String())
		}
	}
	buf.Reset()
	DupesText(&buf, g, g.Dupes(false), s)
	if !strings.Contains(buf.String(), "  2.0.0  [yanked]  via b (dev)\n") {
		t.Fatalf("dupes output:\n%s", buf.String())
	}
}

// TestJSONHealthFields checks the online JSON additions: advisories always
// present as an array, deprecated null or a string, yanked a bool; and that they
// are absent when offline (nil Notes).
func TestJSONHealthFields(t *testing.T) {
	g := npmGraph(t)
	notes := notesFor(t, g, map[string]Note{"a@1.0.0": {Advisories: []string{"GHSA-a"}, Deprecated: "old"}, "d@1.0.0": {Advisories: []string{}}, "c@1.0.0": {Advisories: []string{}}})
	encode := func(n Notes) string {
		doc := NewDoc("remove", &Query{Name: "a"})
		doc.Add(RemoveJSON(g, g.Remove(find(t, g, "a@1.0.0"), false), n))
		var buf bytes.Buffer
		if err := doc.Encode(&buf); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	var online struct {
		Results []struct {
			Package struct {
				Advisories []string
				Deprecated *string
				Yanked     *bool
			}
			Freed []struct {
				Name       string
				Advisories []string
				Deprecated *string
			}
			Kept []struct {
				Name       string
				Advisories []string
				Via        map[string]any
			}
		}
	}
	if err := json.Unmarshal([]byte(encode(notes)), &online); err != nil {
		t.Fatal(err)
	}
	r := online.Results[0]
	if len(r.Package.Advisories) != 1 || r.Package.Deprecated == nil || *r.Package.Deprecated != "old" || r.Package.Yanked == nil {
		t.Fatalf("package = %+v", r.Package)
	}
	if r.Freed[1].Name != "c" || r.Freed[1].Advisories == nil || len(r.Freed[1].Advisories) != 0 || r.Freed[1].Deprecated != nil {
		t.Fatalf("freed c must show empty advisories and null deprecated: %+v", r.Freed)
	}
	if _, has := r.Kept[0].Via["advisories"]; has {
		t.Fatal("via is a reference and carries no findings")
	}
	if !strings.Contains(encode(notes), `"advisories": []`) {
		t.Fatal("a checked, clean package must print advisories: []")
	}

	offline := encode(nil)
	for _, key := range []string{"advisories", "deprecated", "yanked"} {
		if strings.Contains(offline, key) {
			t.Errorf("offline JSON contains %q:\n%s", key, offline)
		}
	}
}

// TestJSONAdvisoriesAreNullWhenUnknown checks that a package that could not be
// checked (no entry, or advisories unknown) reports null, not an empty list, so
// scripts cannot mistake "unchecked" for "clean".
func TestJSONAdvisoriesAreNullWhenUnknown(t *testing.T) {
	g := npmGraph(t)
	doc := NewDoc("why", &Query{Name: "d"})
	doc.Add(WhyJSON(g, g.Why(find(t, g, "d@1.0.0"), graph.WhyOpts{}), Notes{}))
	var buf bytes.Buffer
	if err := doc.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"advisories": null`) {
		t.Fatalf("want advisories: null:\n%s", buf.String())
	}
}

// TestNoteTextDropsControlCharacters checks that registry-supplied text cannot
// inject terminal escape sequences.
func TestNoteTextDropsControlCharacters(t *testing.T) {
	evil := Note{Advisories: []string{"GHSA-\x1b[2Jx"}, Deprecated: "bad \x1b]0;title\x07 \u202e text \x7f end"}
	got := Style{Notes: Notes{0: evil}}.suffix(0)
	for _, r := range got {
		if r == 0x1b || r == 0x07 || r == 0x7f || r == 0x202e {
			t.Fatalf("control character %U survived: %q", r, got)
		}
	}
	if !strings.Contains(got, "GHSA-[2Jx") || !strings.Contains(got, "bad ]0;title text end") {
		t.Fatalf("printable text should remain: %q", got)
	}
}
