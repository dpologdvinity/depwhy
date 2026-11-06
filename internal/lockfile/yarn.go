package lockfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// ParseYarn parses a yarn.lock file in either Yarn Berry (v2+) or Yarn Classic
// (v1) format (SPEC §14.2.1).
//
// Both formats key entries by descriptors ("name@range", several per entry),
// and a dependency resolves to the entry holding the descriptor
// "<dependency name>@<range>". Nodes are named after the real package, so npm:
// aliases and patch: entries share a node with the package they stand for.
// Direct dependencies are classified as prod, dev or optional through each
// project's package.json, read from next to the lockfile.
func ParseYarn(path string, data []byte) (*graph.Graph, []string, error) {
	if bytes.Contains(data, []byte("__metadata:")) {
		return parseYarnBerry(path, data)
	}
	return parseYarnClassic(path, data)
}

// yarnSplit splits a descriptor or locator at the first "@" after index 0, so
// scoped names survive and ranges holding an "@" stay whole:
// "@s/p@npm:1.0" gives "@s/p" and "npm:1.0", "a@npm:b@1" gives "a" and "npm:b@1".
func yarnSplit(desc string) (name, rng string) {
	desc = strings.Trim(desc, `"' `)
	if i := strings.Index(desc[min(1, len(desc)):], "@"); i >= 0 {
		return desc[:i+1], desc[i+2:]
	}
	return desc, ""
}

// yarnPackageName returns the real package name of a descriptor or locator.
// For an npm: alias such as "my-ms@npm:ms@2.1.3" that is the aliased package.
func yarnPackageName(desc string) string {
	name, rng := yarnSplit(desc)
	if target, ok := strings.CutPrefix(rng, "npm:"); ok {
		if real, v := yarnSplit(target); v != "" {
			return real
		}
	}
	return name
}

// yarnDescriptors splits a lockfile entry key ("a@npm:^1.0.0, a@npm:^1.0.1" or
// a Classic header) into its trimmed, unquoted descriptors.
func yarnDescriptors(key string) []string {
	var out []string
	for _, d := range strings.Split(key, ",") {
		if d = strings.Trim(strings.TrimSpace(d), `"' `); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// yarnManifest is the part of a package.json the yarn parser reads.
type yarnManifest struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	Workspaces           json.RawMessage   `json:"workspaces"` // a list of globs, or {"packages": [...]}
}

// readYarnManifest reads dir/package.json. The error wraps fs.ErrNotExist when
// the file is missing.
func readYarnManifest(dir string) (yarnManifest, error) {
	var m yarnManifest
	p := filepath.Join(dir, "package.json")
	raw, err := os.ReadFile(p)
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err != nil {
		return m, fmt.Errorf("%s: %w", p, err)
	}
	return m, nil
}

// kind says how the manifest declares direct dependency name. Yarn Berry lists a
// workspace's dependencies and devDependencies together in the lockfile, so
// package.json is the only place dev dependencies can be told apart. (Optional
// ones carry dependenciesMeta in the lockfile itself.)
func (m yarnManifest) kind(name string) graph.Kind {
	_, prod := m.Dependencies[name]
	if _, dev := m.DevDependencies[name]; dev && !prod {
		return graph.Dev
	}
	return graph.Prod
}

// workspaceGlobs returns the patterns of the manifest's "workspaces" field.
func (m yarnManifest) workspaceGlobs() []string {
	var list []string
	if json.Unmarshal(m.Workspaces, &list) == nil {
		return list
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if json.Unmarshal(m.Workspaces, &obj) == nil {
		return obj.Packages
	}
	return nil
}

// yarnCandidate is a node registered under a dependency name, with the
// descriptor that registered it.
type yarnCandidate struct {
	node int
	desc string
}

// yarnIndex resolves dependencies to nodes through lockfile descriptors.
type yarnIndex struct {
	desc   map[string]int             // "name@range" -> node
	byName map[string][]yarnCandidate // name -> distinct nodes, in registration order
}

// newYarnIndex returns an empty index.
func newYarnIndex() *yarnIndex {
	return &yarnIndex{desc: map[string]int{}, byName: map[string][]yarnCandidate{}}
}

// add registers descriptor desc for node. Callers add in sorted key order,
// which makes the fallback in link deterministic.
func (x *yarnIndex) add(desc string, node int) {
	name, rng := yarnSplit(desc)
	if rng != "" {
		x.desc[name+"@"+rng] = node
	}
	for _, c := range x.byName[name] {
		if c.node == node {
			return
		}
	}
	x.byName[name] = append(x.byName[name], yarnCandidate{node, desc})
}

// link resolves dependency name@rng of from (labelled fromName). An exact
// descriptor wins. Otherwise the packages registered under name are used: a
// package.json "resolutions" override makes Yarn lock a different range than
// the one requested, and Classic workspace members have no entry at all. With
// several candidates the first is taken and a warning added. A missing prod or
// dev dependency is warned about too; a missing optional or peer one is not.
func (x *yarnIndex) link(warns *[]string, fromName, name, rng string, kind graph.Kind) (int, bool) {
	if to, ok := x.desc[name+"@"+rng]; ok {
		return to, true
	}
	c := x.byName[name]
	switch {
	case len(c) == 0:
		if kind == graph.Prod || kind == graph.Dev {
			*warns = append(*warns, fmt.Sprintf("unresolved dependency %s of %s", name, fromName))
		}
		return 0, false
	case len(c) > 1:
		*warns = append(*warns, fmt.Sprintf("%s@%s of %s matches several entries; using %s", name, rng, fromName, c[0].desc))
	}
	return c[0].node, true
}

// yarnBerryEntry is one entry of a Berry lockfile.
type yarnBerryEntry struct {
	Version          string            `yaml:"version"`
	Resolution       string            `yaml:"resolution"` // the locator, e.g. "ms@npm:2.1.3"
	Dependencies     map[string]string `yaml:"dependencies"`
	PeerDependencies map[string]string `yaml:"peerDependencies"`
	DependenciesMeta map[string]struct {
		Optional bool `yaml:"optional"`
	} `yaml:"dependenciesMeta"`
}

// parseYarnBerry parses a Berry lockfile. Entries whose locator uses the
// workspace: protocol become roots, read with their package.json at the
// workspace path. A patch: entry (a user patch or one of Yarn's builtin compat
// patches for typescript, fsevents and resolve) has the name and version of its
// npm: base entry, so both become one node: the patched package is what gets
// installed, and dependencies may point at either descriptor.
func parseYarnBerry(path string, data []byte) (*graph.Graph, []string, error) {
	var raw map[string]yarnBerryEntry
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	delete(raw, "__metadata")

	dir := filepath.Dir(path)
	b := graph.NewBuilder("npm", path)
	x := newYarnIndex()
	var warns []string
	keys := sortedKeys(raw)
	nodes := make(map[string]int, len(keys))    // entry key -> node
	names := make(map[string]string, len(keys)) // entry key -> package name
	manifests := map[string]yarnManifest{}      // workspace entry key -> package.json

	for _, k := range keys {
		e := raw[k]
		descs := yarnDescriptors(k)
		locator := e.Resolution
		if locator == "" && len(descs) > 0 {
			locator = descs[0]
		}
		names[k] = yarnPackageName(locator)
		_, rng := yarnSplit(locator)
		if ws, ok := strings.CutPrefix(rng, "workspace:"); ok {
			version := e.Version // always 0.0.0-use.local
			m, err := readYarnManifest(filepath.Join(dir, filepath.FromSlash(ws)))
			if err == nil {
				manifests[k] = m
				if m.Version != "" {
					version = m.Version
				}
			} else if !errors.Is(err, fs.ErrNotExist) {
				warns = append(warns, err.Error())
			}
			nodes[k] = b.Root(names[k], version)
		} else {
			nodes[k] = b.Node(names[k], e.Version)
		}
		for _, d := range descs {
			x.add(d, nodes[k])
		}
	}

	provided := map[int]map[string]int{} // node -> dependency name -> node it resolved to
	parents := map[int][]int{}
	for _, k := range keys {
		e, from := raw[k], nodes[k]
		m, isWorkspace := manifests[k]
		for _, dn := range sortedKeys(e.Dependencies) {
			kind := graph.Prod
			if e.DependenciesMeta[dn].Optional {
				kind = graph.Optional
			} else if isWorkspace {
				kind = m.kind(dn)
			}
			to, ok := x.link(&warns, names[k], dn, e.Dependencies[dn], kind)
			if !ok {
				continue
			}
			b.Edge(from, to, kind)
			if provided[from] == nil {
				provided[from] = map[string]int{}
			}
			provided[from][dn] = to
			parents[to] = append(parents[to], from)
		}
	}

	// A peer dependency is provided by whoever depends on the package, so it
	// resolves to what each parent's own dependency of that name resolved to.
	// Workspaces have no parent; their peers only match an exact descriptor.
	// shortcut: a peer the parent itself only receives as a peer is not
	// followed further up; chain through parents if that matters.
	for _, k := range keys {
		e, from := raw[k], nodes[k]
		for _, pn := range sortedKeys(e.PeerDependencies) {
			if to, ok := x.desc[pn+"@"+e.PeerDependencies[pn]]; ok {
				b.Edge(from, to, graph.Peer)
			}
			for _, p := range parents[from] {
				if to, ok := provided[p][pn]; ok {
					b.Edge(from, to, graph.Peer)
				}
			}
		}
	}
	return finish(b, warns)
}

// yarnClassicEntry is one block of a Classic lockfile.
type yarnClassicEntry struct {
	descriptors  []string
	name         string
	version      string
	dependencies map[string]string
	optional     map[string]string // optionalDependencies
}

// parseYarnClassic parses a Classic (v1) lockfile. Classic lockfiles hold no
// projects, so the roots come from the package.json next to the lockfile and
// from the workspace members its "workspaces" globs match. Without a
// package.json, the root links to every entry nothing else depends on.
func parseYarnClassic(path string, data []byte) (*graph.Graph, []string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var entries []*yarnClassicEntry
	var curr *yarnClassicEntry
	var section map[string]string // the dependency block being read, if any

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			section = nil
			descs := yarnDescriptors(strings.TrimSuffix(line, ":"))
			if len(descs) > 0 {
				curr = &yarnClassicEntry{
					descriptors:  descs,
					name:         yarnPackageName(descs[0]),
					dependencies: map[string]string{},
					optional:     map[string]string{},
				}
				entries = append(entries, curr)
			}
			continue
		}

		if curr == nil {
			continue
		}

		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			section = nil
			switch {
			case strings.HasPrefix(trimmed, "version "):
				curr.version = strings.Trim(strings.TrimPrefix(trimmed, "version "), `"' `)
			case trimmed == "dependencies:":
				section = curr.dependencies
			case trimmed == "optionalDependencies:":
				section = curr.optional
			}
			continue
		}

		if strings.HasPrefix(line, "    ") && section != nil {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				section[strings.Trim(parts[0], `"' `)] = strings.Trim(parts[1], `"' `)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	b := graph.NewBuilder("npm", path)
	x := newYarnIndex()
	var warns []string
	nodeMap := map[*yarnClassicEntry]int{}
	for _, e := range entries {
		idx := b.Node(e.name, e.version)
		nodeMap[e] = idx
		for _, d := range e.descriptors {
			x.add(d, idx)
		}
	}

	dir := filepath.Dir(path)
	rootLabel := "app"
	pj, err := readYarnManifest(dir)
	hasManifest := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		warns = append(warns, err.Error())
	}
	if pj.Name != "" {
		rootLabel = pj.Name
	}
	rootIdx := b.Root(rootLabel, pj.Version)

	// Workspace members are roots too, registered by name so that members can
	// depend on each other: Classic gives them no lockfile entry.
	type member struct {
		node  int
		label string
		m     yarnManifest
	}
	var members []member
	seen := map[string]bool{}
	for _, pat := range pj.workspaceGlobs() {
		// shortcut: filepath.Glob has no "**" or "!" negation; expand by hand if a project needs them.
		matches, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pat), "package.json"))
		for _, p := range matches {
			d := filepath.Dir(p)
			if seen[d] {
				continue
			}
			seen[d] = true
			m, err := readYarnManifest(d)
			if err != nil {
				warns = append(warns, err.Error())
				continue
			}
			label := m.Name
			if label == "" {
				rel, _ := filepath.Rel(dir, d)
				label = filepath.ToSlash(rel)
			}
			idx := b.Root(label, m.Version)
			x.add(label, idx)
			members = append(members, member{idx, label, m})
		}
	}

	// linkManifest adds the direct dependencies a package.json declares.
	linkManifest := func(from int, label string, m yarnManifest) {
		for _, grp := range []struct {
			deps map[string]string
			kind graph.Kind
		}{{m.Dependencies, graph.Prod}, {m.DevDependencies, graph.Dev}, {m.OptionalDependencies, graph.Optional}, {m.PeerDependencies, graph.Peer}} {
			for _, dn := range sortedKeys(grp.deps) {
				if to, ok := x.link(&warns, label, dn, grp.deps[dn], grp.kind); ok {
					b.Edge(from, to, grp.kind)
				}
			}
		}
	}

	if hasManifest {
		linkManifest(rootIdx, rootLabel, pj)
		for _, mb := range members {
			linkManifest(mb.node, mb.label, mb.m)
		}
	} else {
		// No package.json: link root to direct candidates (nodes with no incoming dependencies)
		depCounts := map[string]int{}
		for _, e := range entries {
			for dep := range e.dependencies {
				depCounts[dep]++
			}
			for dep := range e.optional {
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
		for _, grp := range []struct {
			deps map[string]string
			kind graph.Kind
		}{{e.dependencies, graph.Prod}, {e.optional, graph.Optional}} {
			for _, dn := range sortedKeys(grp.deps) {
				if to, ok := x.link(&warns, e.name, dn, grp.deps[dn], grp.kind); ok {
					b.Edge(from, to, grp.kind)
				}
			}
		}
	}
	return finish(b, warns)
}

// parseDescriptor extracts package name and version specifier from a descriptor
// at its last "@". It suits single descriptors without "@" in the range; the
// yarn parsers use yarnSplit instead.
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
