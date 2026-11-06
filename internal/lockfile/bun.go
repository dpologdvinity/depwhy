package lockfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// bunDeps holds the dependency maps shared by a workspace entry and the
// metadata object of a package entry.
type bunDeps struct {
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

// bunWorkspace is one value of the "workspaces" map, keyed by the member's
// directory ("" is the project itself).
type bunWorkspace struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	bunDeps
}

// bunLock is the subset of bun.lock depwhy reads. Each "packages" value is an
// array whose first element is "name@version" (the real name, also for npm
// aliases); the first object after it, when present, holds the dependency
// maps. Its position depends on the source: index 2 for registry packages,
// index 1 for tarball, file and git ones, absent for workspace members.
type bunLock struct {
	Workspaces map[string]bunWorkspace      `json:"workspaces"`
	Packages   map[string][]json.RawMessage `json:"packages"`
}

// bunSplitKey splits a packages key (an install path such as "a/@s/e/c") into
// package names, keeping "@scope/name" together.
func bunSplitKey(key string) []string {
	var out []string
	for _, p := range strings.Split(key, "/") {
		if n := len(out); n > 0 && strings.HasPrefix(out[n-1], "@") && !strings.Contains(out[n-1], "/") {
			out[n-1] += "/" + p
			continue
		}
		out = append(out, p)
	}
	return out
}

// bunResolve returns the packages key that dependency `name` of the package at
// key `from` resolves to, like node_modules lookup: try from/name, then drop
// one package from the path at a time, ending with the top-level key. from is
// "" for the project root. It reports false when no key matches.
func bunResolve(pkgs map[string][]json.RawMessage, from, name string) (string, bool) {
	var parents []string
	if from != "" {
		parents = bunSplitKey(from)
	}
	for i := len(parents); i >= 0; i-- {
		key := strings.Join(append(parents[:i:i], name), "/")
		if _, ok := pkgs[key]; ok {
			return key, true
		}
	}
	return "", false
}

// bunEntry decodes one packages value into its name, version and dependency
// maps. The name ends at the first "@" after index 0, so scoped names work.
func bunEntry(raw []json.RawMessage) (name, version string, deps bunDeps, err error) {
	var ident string
	if len(raw) == 0 || json.Unmarshal(raw[0], &ident) != nil || ident == "" {
		return "", "", deps, fmt.Errorf("entry does not start with a \"name@version\" string")
	}
	name, version = ident, ""
	if i := strings.Index(ident[1:], "@"); i >= 0 {
		name, version = ident[:i+1], ident[i+2:]
	}
	for _, r := range raw[1:] {
		if t := bytes.TrimSpace(r); len(t) > 0 && t[0] == '{' {
			err = json.Unmarshal(t, &deps)
			break
		}
	}
	return name, version, deps, err
}

// ParseBun parses a bun.lock file (Bun >= 1.2 text lockfile, JSONC: comments
// and trailing commas allowed).
//
// The project and every workspace member become roots. Packages keys are
// install paths ("parent/child" for a nested copy) and dependencies resolve
// through them like node_modules lookup. Edge kinds follow the npm parser:
// only roots have dev edges, and unresolvable prod or dev dependencies produce
// warnings while optional and peer ones are skipped silently.
func ParseBun(path string, data []byte) (*graph.Graph, []string, error) {
	var lock bunLock
	if err := json.Unmarshal(stripJSONC(data), &lock); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	b := graph.NewBuilder("npm", path)
	wsKeys := sortedKeys(lock.Workspaces)
	label := func(k string) string {
		switch ws := lock.Workspaces[k]; {
		case ws.Name != "":
			return ws.Name
		case k == "":
			return "app"
		}
		return k
	}
	wsNodes := map[string]int{}
	for _, k := range wsKeys {
		wsNodes[k] = b.Root(label(k), lock.Workspaces[k].Version)
	}
	if len(wsNodes) == 0 {
		wsNodes[""] = b.Root("app", "")
		wsKeys = []string{""}
	}

	// Package nodes. A workspace member's entry ("lib@workspace:packages/lib")
	// is its root node, and its key is where its nested dependencies live.
	pkgKeys := sortedKeys(lock.Packages)
	idx := map[string]int{}
	names := map[string]string{}
	deps := map[string]bunDeps{}
	wsFrom := map[string]string{"": ""}
	for _, k := range pkgKeys {
		name, version, d, err := bunEntry(lock.Packages[k])
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: packages[%q]: %w", path, k, err)
		}
		if dir, ok := strings.CutPrefix(version, "workspace:"); ok {
			if root, ok := wsNodes[dir]; ok {
				idx[k], wsFrom[dir] = root, k
				continue
			}
		}
		idx[k], names[k], deps[k] = b.Node(name, version), name, d
	}

	var warns []string
	addEdges := func(from int, fromKey, fromLabel string, d bunDeps, root bool) {
		kinds := []struct {
			deps map[string]string
			kind graph.Kind
		}{
			{d.Dependencies, graph.Prod},
			{d.OptionalDependencies, graph.Optional},
			{d.PeerDependencies, graph.Peer},
		}
		if root {
			kinds = append(kinds, struct {
				deps map[string]string
				kind graph.Kind
			}{d.DevDependencies, graph.Dev})
		}
		for _, kd := range kinds {
			for _, n := range sortedKeys(kd.deps) {
				target, ok := bunResolve(lock.Packages, fromKey, n)
				if !ok {
					if kd.kind == graph.Prod || kd.kind == graph.Dev {
						warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", n, fromLabel))
					}
					continue
				}
				b.Edge(from, idx[target], kd.kind)
			}
		}
	}
	for _, k := range wsKeys {
		// A member with no packages entry resolves from the top level ("").
		addEdges(wsNodes[k], wsFrom[k], label(k), lock.Workspaces[k].bunDeps, true)
	}
	for _, k := range pkgKeys {
		if d, ok := deps[k]; ok {
			addEdges(idx[k], k, names[k], d, false)
		}
	}
	return finish(b, warns)
}

// stripJSONC turns JSONC into JSON: it removes // and /* */ comments and
// trailing commas before } or ], leaving the contents of strings untouched.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case c == '"':
			// Copy the whole string, honouring backslash escapes.
			j := i + 1
			for j < len(data) && data[j] != '"' {
				if data[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j, len(data)-1)
			out = append(out, data[i:j+1]...)
			i = j
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			i-- // keep the newline
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			end := bytes.Index(data[i+2:], []byte("*/"))
			if end < 0 {
				return out
			}
			i += end + 3
		case c == '}' || c == ']':
			// Drop a trailing comma, ignoring whitespace and comments already
			// stripped from out.
			t := bytes.TrimRight(out, " \t\r\n")
			if len(t) > 0 && t[len(t)-1] == ',' {
				out = append(t[:len(t)-1], out[len(t):]...)
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}
