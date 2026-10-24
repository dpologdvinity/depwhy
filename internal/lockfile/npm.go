package lockfile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// npmLock is the subset of package-lock.json (lockfileVersion 2 and 3) depwhy
// reads. The legacy "dependencies" tree is ignored.
type npmLock struct {
	LockfileVersion int                 `json:"lockfileVersion"`
	Packages        map[string]npmEntry `json:"packages"`
}

// npmEntry is one value of the "packages" map. The key (not the entry) holds
// the install path; Name is only present for aliased packages and workspace
// members.
type npmEntry struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Link                 bool              `json:"link"`
	Resolved             string            `json:"resolved"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

// npmResolve returns the packages-map key that `name` resolves to from the entry
// at key `from`, using Node's lookup rule: try from/node_modules/name, then walk
// up one node_modules level at a time. A symlink entry ("link": true) is
// followed to the workspace directory it points at. It reports false when the
// dependency is not installed.
func npmResolve(pkgs map[string]npmEntry, from, name string) (string, bool) {
	for p := from; ; {
		key := "node_modules/" + name
		if p != "" {
			key = p + "/" + key
		}
		if e, ok := pkgs[key]; ok {
			if e.Link {
				_, ok := pkgs[e.Resolved]
				return e.Resolved, ok
			}
			return key, true
		}
		if p == "" {
			return "", false
		}
		if i := strings.LastIndex(p, "node_modules/"); i >= 0 {
			p = strings.TrimSuffix(p[:i], "/")
		} else {
			p = ""
		}
	}
}

// npmIsRoot reports whether a packages-map key is the project ("") or a
// workspace member directory, rather than an installed package.
func npmIsRoot(key string) bool { return !strings.Contains(key, "node_modules/") }

// npmName returns the package name for an installed entry: its "name" field, or
// the path after the last node_modules/ (which keeps @scope/name together).
func npmName(key string, e npmEntry) string {
	if e.Name != "" {
		return e.Name
	}
	return key[strings.LastIndex(key, "node_modules/")+len("node_modules/"):]
}

// ParseNPM parses package-lock.json (lockfileVersion 2 or 3, SPEC §8.1).
//
// The project and workspace members become roots; everything else is a package
// node. Edge kinds come from the field that declared the edge, and only roots
// may have dev dependencies. Keys are processed in sorted order so the graph
// is identical on every run. Unresolvable prod or dev dependencies produce
// warnings; unresolvable optional and peer ones are skipped silently.
func ParseNPM(path string, data []byte) (*graph.Graph, []string, error) {
	var lock npmLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if lock.LockfileVersion != 2 && lock.LockfileVersion != 3 {
		return nil, nil, fmt.Errorf("parse %s: package-lock.json lockfileVersion %d is not supported; regenerate it with npm >= 7 (npm install)", path, lock.LockfileVersion)
	}
	keys := make([]string, 0, len(lock.Packages))
	for k := range lock.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b := graph.NewBuilder("npm", path)
	idx := map[string]int{}
	label := func(key string, e npmEntry) string {
		switch {
		case e.Name != "":
			return e.Name
		case key == "":
			return "."
		}
		return key
	}
	for _, k := range keys {
		e := lock.Packages[k]
		switch {
		case e.Link:
		case npmIsRoot(k):
			idx[k] = b.Root(label(k, e), e.Version)
		default:
			idx[k] = b.Node(npmName(k, e), e.Version)
		}
	}

	var warns []string
	for _, k := range keys {
		e := lock.Packages[k]
		from, ok := idx[k]
		if e.Link || !ok {
			continue
		}
		kinds := []struct {
			deps map[string]string
			kind graph.Kind
		}{
			{e.Dependencies, graph.Prod},
			{e.OptionalDependencies, graph.Optional},
			{e.PeerDependencies, graph.Peer},
		}
		if npmIsRoot(k) {
			kinds = append(kinds, struct {
				deps map[string]string
				kind graph.Kind
			}{e.DevDependencies, graph.Dev})
		}
		for _, kd := range kinds {
			names := make([]string, 0, len(kd.deps))
			for n := range kd.deps {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				target, ok := npmResolve(lock.Packages, k, n)
				to, found := idx[target]
				if !ok || !found {
					if kd.kind == graph.Prod || kd.kind == graph.Dev {
						warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", n, label(k, e)))
					}
					continue
				}
				b.Edge(from, to, kd.kind)
			}
		}
	}
	return finish(b, warns)
}
