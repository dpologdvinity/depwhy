package lockfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// ParseYarn parses a yarn.lock file in either Yarn Berry (v2+) or Yarn Classic (v1) format.
func ParseYarn(path string, data []byte) (*graph.Graph, []string, error) {
	if bytes.Contains(data, []byte("__metadata:")) {
		return parseYarnBerry(path, data)
	}
	return parseYarnClassic(path, data)
}

type yarnBerryEntry struct {
	Version          string            `yaml:"version"`
	Resolution       string            `yaml:"resolution"`
	Dependencies     map[string]string `yaml:"dependencies"`
	PeerDependencies map[string]string `yaml:"peerDependencies"`
}

func parseYarnBerry(path string, data []byte) (*graph.Graph, []string, error) {
	var raw map[string]yarnBerryEntry
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		if k == "__metadata" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b := graph.NewBuilder("npm", path)
	nodeMap := map[string]int{} // raw key -> node index
	descMap := map[string]int{} // descriptor -> node index

	for _, k := range keys {
		e := raw[k]
		name, _ := parseDescriptor(k)
		isRoot := strings.Contains(k, "@workspace:.") || strings.Contains(e.Resolution, "workspace:.")
		isMember := strings.Contains(k, "@workspace:") || strings.Contains(e.Resolution, "workspace:")
		var idx int
		if isRoot || isMember {
			idx = b.Root(name, e.Version)
		} else {
			idx = b.Node(name, e.Version)
		}
		nodeMap[k] = idx
		for _, d := range strings.Split(k, ",") {
			descMap[strings.TrimSpace(d)] = idx
		}
	}

	var warns []string
	for _, k := range keys {
		e := raw[k]
		from := nodeMap[k]
		fromName, _ := parseDescriptor(k)

		for depName, spec := range e.Dependencies {
			to, ok := resolveYarnBerry(descMap, depName, spec)
			if ok {
				b.Edge(from, to, graph.Prod)
			} else {
				warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", depName, fromName))
			}
		}
		for depName, spec := range e.PeerDependencies {
			to, ok := resolveYarnBerry(descMap, depName, spec)
			if ok {
				b.Edge(from, to, graph.Peer)
			}
		}
	}

	g, err := b.Build()
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return g, warns, nil
}

func resolveYarnBerry(descMap map[string]int, depName, spec string) (int, bool) {
	// 1. Direct descriptor match: name@spec
	target := depName + "@" + spec
	if idx, ok := descMap[target]; ok {
		return idx, true
	}
	if idx, ok := descMap[spec]; ok {
		return idx, true
	}
	// 2. Prefix match
	prefix := depName + "@"
	for d, idx := range descMap {
		if strings.HasPrefix(d, prefix) {
			return idx, true
		}
	}
	return 0, false
}

type yarnClassicEntry struct {
	descriptors  []string
	name         string
	version      string
	dependencies map[string]string
}

func parseYarnClassic(path string, data []byte) (*graph.Graph, []string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var entries []*yarnClassicEntry
	var curr *yarnClassicEntry
	inDeps := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			rawHeader := strings.TrimSuffix(line, ":")
			var descs []string
			for _, part := range strings.Split(rawHeader, ",") {
				d := strings.Trim(strings.TrimSpace(part), `"' `)
				if d != "" {
					descs = append(descs, d)
				}
			}
			if len(descs) > 0 {
				name, _ := parseDescriptor(descs[0])
				curr = &yarnClassicEntry{
					descriptors:  descs,
					name:         name,
					dependencies: make(map[string]string),
				}
				entries = append(entries, curr)
			}
			inDeps = false
			continue
		}

		if curr == nil {
			continue
		}

		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			if strings.HasPrefix(trimmed, "version ") {
				val := strings.TrimPrefix(trimmed, "version ")
				curr.version = strings.Trim(val, `"' `)
				inDeps = false
			} else if trimmed == "dependencies:" || strings.HasPrefix(trimmed, "dependencies:") {
				inDeps = true
			} else {
				inDeps = false
			}
			continue
		}

		if strings.HasPrefix(line, "    ") && inDeps {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				depName := strings.Trim(parts[0], `"' `)
				depRange := strings.Trim(parts[1], `"' `)
				curr.dependencies[depName] = depRange
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	b := graph.NewBuilder("npm", path)
	nodeMap := map[*yarnClassicEntry]int{}
	descMap := map[string]int{}

	for _, e := range entries {
		idx := b.Node(e.name, e.version)
		nodeMap[e] = idx
		for _, d := range e.descriptors {
			descMap[d] = idx
		}
	}

	// Root creation: read package.json next to yarn.lock
	dir := filepath.Dir(path)
	pkgPath := filepath.Join(dir, "package.json")
	var rootDeps []struct {
		name, spec string
		kind       graph.Kind
	}
	rootLabel := "app"
	rootVersion := ""

	if raw, err := os.ReadFile(pkgPath); err == nil {
		var pj struct {
			Name                 string            `json:"name"`
			Version              string            `json:"version"`
			Dependencies         map[string]string `json:"dependencies"`
			DevDependencies      map[string]string `json:"devDependencies"`
			OptionalDependencies map[string]string `json:"optionalDependencies"`
			PeerDependencies     map[string]string `json:"peerDependencies"`
		}
		if json.Unmarshal(raw, &pj) == nil {
			if pj.Name != "" {
				rootLabel = pj.Name
			}
			rootVersion = pj.Version
			for k, v := range pj.Dependencies {
				rootDeps = append(rootDeps, struct {
					name, spec string
					kind       graph.Kind
				}{k, v, graph.Prod})
			}
			for k, v := range pj.DevDependencies {
				rootDeps = append(rootDeps, struct {
					name, spec string
					kind       graph.Kind
				}{k, v, graph.Dev})
			}
			for k, v := range pj.OptionalDependencies {
				rootDeps = append(rootDeps, struct {
					name, spec string
					kind       graph.Kind
				}{k, v, graph.Optional})
			}
			for k, v := range pj.PeerDependencies {
				rootDeps = append(rootDeps, struct {
					name, spec string
					kind       graph.Kind
				}{k, v, graph.Peer})
			}
		}
	}

	rootIdx := b.Root(rootLabel, rootVersion)
	var warns []string

	if len(rootDeps) > 0 {
		for _, rd := range rootDeps {
			to, ok := resolveYarnClassic(descMap, rd.name, rd.spec)
			if ok {
				b.Edge(rootIdx, to, rd.kind)
			} else if rd.kind == graph.Prod || rd.kind == graph.Dev {
				warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", rd.name, rootLabel))
			}
		}
	} else {
		// No package.json: link root to direct candidates (nodes with no incoming dependencies)
		depCounts := map[string]int{}
		for _, e := range entries {
			for dep := range e.dependencies {
				depCounts[dep]++
			}
		}
		for _, e := range entries {
			if depCounts[e.name] == 0 {
				b.Edge(rootIdx, nodeMap[e], graph.Prod)
			}
		}
	}

	for _, e := range entries {
		from := nodeMap[e]
		for depName, spec := range e.dependencies {
			to, ok := resolveYarnClassic(descMap, depName, spec)
			if ok {
				b.Edge(from, to, graph.Prod)
			} else {
				warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", depName, e.name))
			}
		}
	}

	g, err := b.Build()
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return g, warns, nil
}

func resolveYarnClassic(descMap map[string]int, depName, spec string) (int, bool) {
	target := depName + "@" + spec
	if idx, ok := descMap[target]; ok {
		return idx, true
	}
	prefix := depName + "@"
	for d, idx := range descMap {
		if strings.HasPrefix(d, prefix) {
			return idx, true
		}
	}
	return 0, false
}

// parseDescriptor extracts package name and version specifier from a descriptor.
func parseDescriptor(desc string) (name, versionRange string) {
	desc = strings.Trim(desc, `"' `)
	lastAt := strings.LastIndex(desc, "@")
	if lastAt <= 0 {
		return desc, ""
	}
	name = desc[:lastAt]
	versionRange = desc[lastAt+1:]
	return name, versionRange
}
