package cli

import (
	"github.com/dpologdvinity/depwhy/internal/graph"
	"github.com/dpologdvinity/depwhy/internal/render"
)

// across runs --dupes --across and looks up only versions in its final results.
func (s *session) across() int {
	ds := graph.DupesAcross(s.graphs, s.o.prod)
	shown := map[*graph.Graph][]int{}
	for _, d := range ds {
		for _, v := range d.Versions {
			for _, src := range v.Sources {
				shown[src.Graph] = append(shown[src.Graph], src.Node)
			}
		}
	}
	notes := s.lookup(shown)
	doc := render.NewDoc("dupes", nil)
	doc.Across = true
	if s.o.json {
		for _, d := range ds {
			doc.Add(render.AcrossJSON(d, notes))
		}
	} else {
		render.AcrossText(s.stdout, ds, s.style, notes)
	}
	return s.finishJSON(doc)
}
