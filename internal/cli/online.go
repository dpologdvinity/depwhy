package cli

import (
	"context"

	"github.com/dpologdvinity/depwhy/internal/graph"
	"github.com/dpologdvinity/depwhy/internal/health"
	"github.com/dpologdvinity/depwhy/internal/render"
)

// healthClient builds the client for --online. Tests replace it to point at
// local fake services, so no test touches the network.
var healthClient = func(refresh bool) *health.Client { return health.New(Version, refresh) }

// lookup fetches online information (SPEC §12) for the given nodes and returns
// it as per-graph render notes. It returns nil without --online, which keeps
// offline output identical to v0.1. Lookup failures become warnings and the
// affected packages appear without information.
func (s *session) lookup(shown map[*graph.Graph][]int) map[*graph.Graph]render.Notes {
	if !s.o.online {
		return nil
	}
	var pkgs []health.Pkg
	for _, g := range s.graphs {
		for _, n := range shown[g] {
			if g.Nodes[n].Root {
				continue
			}
			pkgs = append(pkgs, health.Pkg{Ecosystem: g.Ecosystem, Name: g.Nodes[n].Name, Version: g.Nodes[n].Version})
		}
	}
	infos, warnings := healthClient(s.o.refresh).Lookup(context.Background(), pkgs)
	for _, w := range warnings {
		s.warn(w)
	}

	notes := map[*graph.Graph]render.Notes{}
	for _, g := range s.graphs {
		notes[g] = render.Notes{} // non-nil even when empty: it marks the run as online
		for _, n := range shown[g] {
			info, ok := infos[health.Pkg{Ecosystem: g.Ecosystem, Name: g.Nodes[n].Name, Version: g.Nodes[n].Version}]
			if !ok {
				continue
			}
			notes[g][n] = render.Note{Advisories: info.Advisories, Deprecated: info.Deprecated, Yanked: info.Yanked}
		}
	}
	return notes
}
