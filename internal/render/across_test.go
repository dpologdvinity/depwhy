package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

func TestAcrossRendering(t *testing.T) {
	a, b := npmGraph(t), npmGraph(t)
	a.Lockfile, b.Lockfile = "web/package-lock.json", "api/package-lock.json"
	a.LockfileLabel, b.LockfileLabel = a.Lockfile, b.Lockfile
	ds := graph.DupesAcross([]*graph.Graph{a, b}, false)
	var out bytes.Buffer
	AcrossText(&out, ds, Style{}, nil)
	want := "c  2 versions across 2 lockfiles\n" +
		"  1.0.0  in api/package-lock.json via a (prod)\n" +
		"  1.0.0  in web/package-lock.json via a (prod)\n" +
		"  2.0.0  in api/package-lock.json via b (dev)\n" +
		"  2.0.0  in web/package-lock.json via b (dev)\n"
	if out.String() != want {
		t.Fatalf("text = %s", &out)
	}
	notes := map[*graph.Graph]Notes{a: {find(t, a, "c@1.0.0"): {Deprecated: "old", Advisories: []string{"GHSA-test"}}}}
	out.Reset()
	AcrossText(&out, ds, Style{Color: true}, notes)
	if !strings.Contains(out.String(), "GHSA-test") || !strings.Contains(out.String(), "deprecated") {
		t.Fatalf("online text = %s", &out)
	}
	doc := NewDoc("dupes", nil)
	doc.Across = true
	for _, d := range ds {
		doc.Add(AcrossJSON(d, notes))
	}
	out.Reset()
	if err := doc.Encode(&out); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Across  bool
		Results []struct {
			Name, Ecosystem string
			Versions        []struct {
				Version string
				Sources []struct {
					Lockfile, Version string
					Roots             []string
					DirectDeps        []leadJSON `json:"direct_deps"`
					Deprecated        string
				}
			}
		}
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Across || len(decoded.Results) != 1 || decoded.Results[0].Name != "c" || decoded.Results[0].Ecosystem != "npm" {
		t.Fatalf("json = %s", &out)
	}
	s := decoded.Results[0].Versions[0].Sources[1]
	if s.Lockfile != a.Lockfile || s.Version != "1.0.0" || len(s.Roots) != 1 || s.Roots[0] != "app" || len(s.DirectDeps) != 1 || s.DirectDeps[0].Kind != "prod" || s.Deprecated != "old" {
		t.Fatalf("source = %+v", s)
	}
	out.Reset()
	AcrossText(&out, nil, Style{}, nil)
	if out.String() != "no duplicate packages across lockfiles\n" {
		t.Fatalf("empty text = %s", &out)
	}
}

func TestRelativeLockfileRendering(t *testing.T) {
	g := npmGraph(t)
	g.LockfileLabel = "packages/web/package-lock.json"
	c := find(t, g, "c@1.0.0")
	var out bytes.Buffer
	WhyText(&out, g, g.Why(c, graph.WhyOpts{}), Style{})
	RemoveText(&out, g, g.Remove(c, false), Style{})
	DupesText(&out, g, g.Dupes(false), Style{})
	HeaviestText(&out, g, nil, Style{})
	if strings.Count(out.String(), g.LockfileLabel) != 4 {
		t.Fatalf("relative text = %s", &out)
	}
	for _, result := range []any{
		WhyJSON(g, g.Why(c, graph.WhyOpts{}), nil),
		RemoveJSON(g, g.Remove(c, false), nil),
		DupesJSON(g, g.Dupes(false), nil),
		HeaviestJSON(g, nil),
	} {
		data, err := json.Marshal(result)
		if err != nil || !strings.Contains(string(data), `"lockfile":"`+g.LockfileLabel+`"`) {
			t.Fatalf("json = %s, %v", data, err)
		}
	}
}
