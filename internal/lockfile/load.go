package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

type Found struct{ Path, Ecosystem, Format string }

// supported is in load/output order (SPEC §4).
var supported = []struct{ File, Ecosystem, Format string }{
	{"pnpm-lock.yaml", "npm", "pnpm"},
	{"package-lock.json", "npm", "npm"},
	{"uv.lock", "python", "uv"},
	{"Cargo.lock", "cargo", "cargo"},
}

// Detect looks for lockfiles in dir, one per ecosystem. ecosystem "" means all.
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
		found = append(found, Found{p, s.Ecosystem, s.Format})
	}
	if len(found) == 0 {
		return nil, nil, fmt.Errorf("no supported lockfile in %s (looked for pnpm-lock.yaml, package-lock.json, uv.lock, Cargo.lock)", dir)
	}
	return found, warns, nil
}

// FromPaths classifies explicit lockfile paths by base name, in load order.
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
					found = append(found, Found{p, s.Ecosystem, s.Format})
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

// Load reads and parses one lockfile.
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
	case "uv":
		return ParseUV(f.Path, data)
	case "cargo":
		return ParseCargo(f.Path, data)
	}
	return nil, nil, fmt.Errorf("unknown lockfile format %q", f.Format)
}
