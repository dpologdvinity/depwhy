package render

import (
	"fmt"
	"io"
	"sort"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// AcrossText prints conflicting versions with their lockfiles and direct leads.
func AcrossText(w io.Writer, ds []graph.AcrossDupe, style Style, notes map[*graph.Graph]Notes) {
	if len(ds) == 0 {
		fmt.Fprintln(w, "no duplicate packages across lockfiles")
		return
	}
	for _, d := range ds {
		fmt.Fprintln(w, style.Bold(fmt.Sprintf("%s  %s across %s", d.Name, plural(len(d.Versions), "version"), plural(d.Lockfiles, "lockfile"))))
		for _, v := range d.Versions {
			for _, src := range v.Sources {
				s := style
				s.Notes = notes[src.Graph]
				via := leadText(src.Graph, src.Leads, s, directOf)
				if len(src.Leads) == 0 {
					via = "(no direct dependency)"
				}
				fmt.Fprintf(w, "  %s%s  in %s via %s\n", v.Version, s.suffixOnline(src.Node), lockfileName(src.Graph), via)
			}
		}
	}
}

type acrossSourceJSON struct {
	Lockfile   string     `json:"lockfile"`
	Version    string     `json:"version"`
	Roots      []string   `json:"roots"`
	DirectDeps []leadJSON `json:"direct_deps"`
	*healthJSON
}

type acrossVersionJSON struct {
	Version string             `json:"version"`
	Sources []acrossSourceJSON `json:"sources"`
}

type acrossDupeJSON struct {
	Ecosystem string              `json:"ecosystem"`
	Name      string              `json:"name"`
	Versions  []acrossVersionJSON `json:"versions"`
}

// AcrossJSON builds one package's cross-lockfile result, preserving source-local
// leads, workspace roots and optional health findings.
func AcrossJSON(d graph.AcrossDupe, notes map[*graph.Graph]Notes) any {
	out := acrossDupeJSON{Ecosystem: d.Ecosystem, Name: d.Name, Versions: []acrossVersionJSON{}}
	for _, v := range d.Versions {
		version := acrossVersionJSON{Version: v.Version, Sources: []acrossSourceJSON{}}
		for _, src := range v.Sources {
			roots := []string{}
			seen := map[string]bool{}
			for _, lead := range src.Leads {
				name := src.Graph.Nodes[lead.Root].Name
				if !seen[name] {
					roots = append(roots, name)
					seen[name] = true
				}
			}
			sort.Strings(roots)
			version.Sources = append(version.Sources, acrossSourceJSON{
				Lockfile: lockfileName(src.Graph), Version: v.Version, Roots: roots,
				DirectDeps: leadsJSON(src.Graph, src.Leads), healthJSON: notes[src.Graph].health(src.Node),
			})
		}
		out.Versions = append(out.Versions, version)
	}
	return out
}
