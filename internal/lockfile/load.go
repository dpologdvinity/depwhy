// Package lockfile finds lockfiles and parses them into graph.Graph values.
//
// Detect and FromPaths decide which files to read (SPEC §4), Load dispatches
// to the parser for a file's format, and each ParseX function turns the bytes
// of one format into a graph. Parsers are pure: they read only the data they
// are given, apart from pnpm, which also reads package.json for root labels.
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// Found is a lockfile selected for loading. Format is one of "pnpm", "npm",
// "yarn", "bun", "uv", "poetry", "cargo", or "gomod".
type Found struct {
	Path, Ecosystem, Format string
	GoGraph                 bool
	CargoMetadata           bool
}

// supported lists the lockfile names depwhy understands. The order is both
// the load order and the output order (SPEC §4).
var supported = []struct{ File, Ecosystem, Format string }{
	{"pnpm-lock.yaml", "npm", "pnpm"},
	{"package-lock.json", "npm", "npm"},
	{"uv.lock", "python", "uv"},
	{"Cargo.lock", "cargo", "cargo"},
}

// Detect looks for lockfiles in dir, at most one per ecosystem. ecosystem "" means
// all. If a directory has both pnpm-lock.yaml and package-lock.json it keeps the
// pnpm file and returns a warning. It returns an error when nothing is found.
// Detect only checks that files exist; it does not parse them.
func Detect(dir, ecosystem string) ([]Found, []string, error) {
	var found []Found
	var warns []string
	have := map[string]bool{}
	for _, s := range supported {
		if ecosystem != "" && s.Ecosystem != ecosystem {
			continue
		}
		p := filepath.Join(dir, s.File)
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			continue
		}
		if have[s.Ecosystem] {
			warns = append(warns, "both pnpm-lock.yaml and package-lock.json found; using pnpm-lock.yaml")
			continue
		}
		have[s.Ecosystem] = true
		found = append(found, Found{Path: p, Ecosystem: s.Ecosystem, Format: s.Format})
	}
	if len(found) == 0 {
		return nil, nil, fmt.Errorf("no supported lockfile in %s (looked for pnpm-lock.yaml, package-lock.json, uv.lock, Cargo.lock)", dir)
	}
	return found, warns, nil
}

// FromPaths classifies explicit lockfile paths by base name and returns them in
// load order. Unlike Detect it does not apply the one-lockfile-per-ecosystem
// rule. It errors on an unknown file name, or when ecosystem filters out every
// path.
func FromPaths(paths []string, ecosystem string) ([]Found, error) {
	var found []Found
	rank := map[string]int{}
	for i, s := range supported {
		rank[s.Format] = i
	}
	for _, p := range paths {
		base := filepath.Base(p)
		ok := false
		for _, s := range supported {
			if s.File == base {
				ok = true
				if ecosystem == "" || ecosystem == s.Ecosystem {
					found = append(found, Found{Path: p, Ecosystem: s.Ecosystem, Format: s.Format})
				}
			}
		}
		if !ok {
			return nil, fmt.Errorf("unrecognized lockfile name: %s", base)
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no lockfile matches ecosystem %s", ecosystem)
	}
	sort.SliceStable(found, func(i, j int) bool { return rank[found[i].Format] < rank[found[j].Format] })
	return found, nil
}

// Load reads the file named by f and parses it with the parser for f.Format.
// The returned strings are warnings; they never indicate failure.
func Load(f Found) (*graph.Graph, []string, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, nil, err
	}
	switch f.Format {
	case "npm":
		return ParseNPM(f.Path, data)
	case "pnpm":
		return ParsePnpm(f.Path, data)
	case "yarn":
		return ParseYarn(f.Path, data)
	case "bun":
		return ParseBun(f.Path, data)
	case "poetry":
		return ParsePoetry(f.Path, data)
	case "uv":
		return ParseUV(f.Path, data)
	case "cargo":
		if f.CargoMetadata {
			g, ws, err := parseCargoWithCLI(f.Path)
			if err == nil {
				return g, ws, nil
			}
			g2, ws2, err2 := ParseCargo(f.Path, data)
			return g2, append(ws, ws2...), err2
		}
		return ParseCargo(f.Path, data)
	case "gomod":
		return ParseGoMod(f.Path, data, f.GoGraph)
	}
	return nil, nil, fmt.Errorf("unknown lockfile format %q", f.Format)
}
