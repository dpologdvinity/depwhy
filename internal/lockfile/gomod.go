package lockfile

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// ParseGoMod parses a go.mod file (SPEC §14.2.4).
// If goGraph is true, it executes `go mod graph` in the module directory to refine
// transitive dependency edges.
func ParseGoMod(path string, data []byte, goGraph bool) (*graph.Graph, []string, error) {
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	rootName := "app"
	if f.Module != nil && f.Module.Mod.Path != "" {
		rootName = f.Module.Mod.Path
	} else {
		base := filepath.Base(filepath.Dir(path))
		if base != "." && base != "/" && base != "" {
			rootName = base
		}
	}

	b := graph.NewBuilder("go", path)
	root := b.Root(rootName, "")

	nodeMap := map[string]int{}
	getNode := func(full string) int {
		if idx, ok := nodeMap[full]; ok {
			return idx
		}
		name, ver := splitModVer(full)
		idx := b.Node(name, ver)
		nodeMap[full] = idx
		if ver != "" && nodeMap[name] == 0 {
			nodeMap[name] = idx
		}
		return idx
	}

	// Map replace directives: old path -> new path/version
	replaces := map[string]string{}
	for _, rep := range f.Replace {
		if rep.New.Version != "" {
			replaces[rep.Old.Path] = rep.New.Path + "@" + rep.New.Version
		} else {
			replaces[rep.Old.Path] = rep.New.Path
		}
	}

	// Index direct requirements from go.mod
	directKinds := map[string]graph.Kind{}
	for _, req := range f.Require {
		p := req.Mod.Path
		v := req.Mod.Version
		if rep, ok := replaces[p]; ok {
			rName, rVer := splitModVer(rep)
			p = rName
			if rVer != "" {
				v = rVer
			}
		}
		idx := b.Node(p, v)
		nodeMap[p] = idx
		nodeMap[p+"@"+v] = idx

		k := graph.Prod
		if req.Indirect {
			k = graph.Indirect
		}
		directKinds[p] = k
		directKinds[p+"@"+v] = k
	}

	var warns []string
	graphRefined := false

	if goGraph {
		cmd := exec.Command("go", "mod", "graph")
		cmd.Dir = filepath.Dir(path)
		out, err := cmd.Output()
		if err == nil {
			scanner := bufio.NewScanner(bytes.NewReader(out))
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) != 2 {
					continue
				}
				fromStr, toStr := fields[0], fields[1]

				// Check if from is the root module
				fromName, _ := splitModVer(fromStr)
				if fromName == rootName {
					toIdx := getNode(toStr)
					k := graph.Prod
					if dk, ok := directKinds[toStr]; ok {
						k = dk
					} else if dk, ok := directKinds[toName(toStr)]; ok {
						k = dk
					}
					b.Edge(root, toIdx, k)
				} else {
					fromIdx := getNode(fromStr)
					toIdx := getNode(toStr)
					b.Edge(fromIdx, toIdx, graph.Prod)
				}
			}
			graphRefined = true
		} else {
			warns = append(warns, fmt.Sprintf("go mod graph failed: %v; falling back to direct go.mod requirements", err))
		}
	}

	if !graphRefined {
		for _, req := range f.Require {
			p := req.Mod.Path
			v := req.Mod.Version
			if rep, ok := replaces[p]; ok {
				rName, rVer := splitModVer(rep)
				p = rName
				if rVer != "" {
					v = rVer
				}
			}
			idx := nodeMap[p+"@"+v]
			k := directKinds[p+"@"+v]
			b.Edge(root, idx, k)
		}
	}

	g, err := b.Build()
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return g, warns, nil
}

func splitModVer(s string) (string, string) {
	if i := strings.Index(s, "@"); i != -1 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func toName(s string) string {
	name, _ := splitModVer(s)
	return name
}
