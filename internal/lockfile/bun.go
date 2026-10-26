package lockfile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

type bunPackage struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

type bunWorkspace struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

type bunLock struct {
	LockfileVersion int                     `json:"lockfileVersion"`
	Workspaces      map[string]bunWorkspace `json:"workspaces"`
	Packages        map[string]bunPackage   `json:"packages"`
}

// ParseBun parses a bun.lock file (Bun >= 1.2 JSONC format).
func ParseBun(path string, data []byte) (*graph.Graph, []string, error) {
	clean := stripJSONComments(data)
	var lock bunLock
	if err := json.Unmarshal(clean, &lock); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	b := graph.NewBuilder("npm", path)

	// Sort workspace keys
	wsKeys := make([]string, 0, len(lock.Workspaces))
	for k := range lock.Workspaces {
		wsKeys = append(wsKeys, k)
	}
	sort.Strings(wsKeys)

	// Sort package keys
	pkgKeys := make([]string, 0, len(lock.Packages))
	for k := range lock.Packages {
		pkgKeys = append(pkgKeys, k)
	}
	sort.Strings(pkgKeys)

	wsNodes := map[string]int{}
	pkgNodes := map[string]int{}
	descNodes := map[string]int{}

	for _, k := range wsKeys {
		ws := lock.Workspaces[k]
		label := ws.Name
		if label == "" {
			if k == "" {
				label = "app"
			} else {
				label = k
			}
		}
		idx := b.Root(label, ws.Version)
		wsNodes[k] = idx
	}

	// Fallback if no workspaces defined
	if len(wsNodes) == 0 {
		wsNodes[""] = b.Root("app", "")
		wsKeys = append(wsKeys, "")
	}

	for _, k := range pkgKeys {
		p := lock.Packages[k]
		name := p.Name
		version := p.Version
		if name == "" || version == "" {
			n, v := parseDescriptor(k)
			if name == "" {
				name = n
			}
			if version == "" {
				version = v
			}
		}
		idx := b.Node(name, version)
		pkgNodes[k] = idx
		descNodes[k] = idx
		descNodes[name+"@"+version] = idx
		descNodes[name] = idx
	}

	var warns []string

	// Edges from workspaces
	for _, k := range wsKeys {
		ws := lock.Workspaces[k]
		from := wsNodes[k]
		fromLabel := ws.Name
		if fromLabel == "" {
			fromLabel = "app"
		}

		type depKind struct {
			deps map[string]string
			kind graph.Kind
		}
		kinds := []depKind{
			{ws.Dependencies, graph.Prod},
			{ws.DevDependencies, graph.Dev},
			{ws.OptionalDependencies, graph.Optional},
			{ws.PeerDependencies, graph.Peer},
		}

		for _, dk := range kinds {
			for depName, spec := range dk.deps {
				to, ok := resolveBun(descNodes, depName, spec)
				if ok {
					b.Edge(from, to, dk.kind)
				} else if dk.kind == graph.Prod || dk.kind == graph.Dev {
					warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", depName, fromLabel))
				}
			}
		}
	}

	// Edges between packages
	for _, k := range pkgKeys {
		p := lock.Packages[k]
		from := pkgNodes[k]
		fromName := p.Name
		if fromName == "" {
			fromName, _ = parseDescriptor(k)
		}

		type depKind struct {
			deps map[string]string
			kind graph.Kind
		}
		kinds := []depKind{
			{p.Dependencies, graph.Prod},
			{p.OptionalDependencies, graph.Optional},
			{p.PeerDependencies, graph.Peer},
		}

		for _, dk := range kinds {
			for depName, spec := range dk.deps {
				to, ok := resolveBun(descNodes, depName, spec)
				if ok {
					b.Edge(from, to, dk.kind)
				} else if dk.kind == graph.Prod {
					warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", depName, fromName))
				}
			}
		}
	}

	g, err := b.Build()
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return g, warns, nil
}

func resolveBun(nodes map[string]int, depName, spec string) (int, bool) {
	if idx, ok := nodes[spec]; ok {
		return idx, true
	}
	target := depName + "@" + spec
	if idx, ok := nodes[target]; ok {
		return idx, true
	}
	prefix := depName + "@"
	for k, idx := range nodes {
		if strings.HasPrefix(k, prefix) {
			return idx, true
		}
	}
	if idx, ok := nodes[depName]; ok {
		return idx, true
	}
	return 0, false
}

func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escape := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(data); i++ {
		b := data[i]

		if inLineComment {
			if b == '\n' {
				inLineComment = false
				out = append(out, b)
			}
			continue
		}

		if inBlockComment {
			if b == '*' && i+1 < len(data) && data[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			out = append(out, b)
			if escape {
				escape = false
			} else if b == '\\' {
				escape = true
			} else if b == '"' {
				inString = false
			}
			continue
		}

		if b == '"' {
			inString = true
			out = append(out, b)
			continue
		}

		if b == '/' && i+1 < len(data) {
			if data[i+1] == '/' {
				inLineComment = true
				i++
				continue
			} else if data[i+1] == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		out = append(out, b)
	}
	return out
}
