package lockfile

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// cargoLock is the subset of Cargo.lock that depwhy reads. Dependencies are
// strings of the form "name", "name version" or "name version (source)".
type cargoLock struct {
	Version int `toml:"version"`
	Package []struct {
		Name         string   `toml:"name"`
		Version      string   `toml:"version"`
		Source       string   `toml:"source"`
		Dependencies []string `toml:"dependencies"`
	} `toml:"package"`
}

// ParseCargo parses Cargo.lock (version absent, 3 or 4, SPEC §8.4).
//
// Packages without a source are roots. Cargo.lock records no dev or build
// edges, so every edge is Prod, and a path dependency outside the workspace
// also lacks a source and shows up as a root. Both are documented limits.
// A dependency string is matched by name, then version, then source; the first
// match wins when several remain, with a warning.
func ParseCargo(path string, data []byte) (*graph.Graph, []string, error) {
	var lock cargoLock
	if _, err := toml.Decode(string(data), &lock); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var warns []string
	if v := lock.Version; v != 0 && v != 3 && v != 4 {
		warns = append(warns, fmt.Sprintf("unsupported Cargo.lock version %d; results may be wrong", v))
	}

	b := graph.NewBuilder("cargo", path)
	byName := map[string][]int{}
	nodes := make([]int, len(lock.Package))
	for i, p := range lock.Package {
		byName[p.Name] = append(byName[p.Name], i)
		if p.Source == "" {
			nodes[i] = b.Root(p.Name, p.Version)
		} else {
			nodes[i] = b.Node(p.Name, p.Version)
		}
	}

	for i, p := range lock.Package {
		for _, s := range p.Dependencies {
			parts := strings.SplitN(s, " ", 3)
			cands := byName[parts[0]]
			if len(parts) > 1 {
				cands = filterPkgs(cands, func(j int) bool { return lock.Package[j].Version == parts[1] })
			}
			if len(parts) > 2 {
				src := strings.Trim(parts[2], "()")
				cands = filterPkgs(cands, func(j int) bool { return lock.Package[j].Source == src })
			}
			switch len(cands) {
			case 0:
				warns = append(warns, fmt.Sprintf("unresolved dependency %s of %s", s, p.Name))
				continue
			case 1:
			default:
				warns = append(warns, fmt.Sprintf("ambiguous dependency %s of %s; picked %s", parts[0], p.Name, lock.Package[cands[0]].Version))
			}
			b.Edge(nodes[i], nodes[cands[0]], graph.Prod)
		}
	}
	return finish(b, warns)
}

type cargoMetadata struct {
	Packages         []cargoMetaPackage `json:"packages"`
	WorkspaceMembers []string           `json:"workspace_members"`
	Resolve          *cargoResolve      `json:"resolve"`
}

type cargoMetaPackage struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
}

type cargoResolve struct {
	Nodes []cargoResolveNode `json:"nodes"`
}

type cargoResolveNode struct {
	ID   string            `json:"id"`
	Deps []cargoResolveDep `json:"deps"`
}

type cargoResolveDep struct {
	Name     string             `json:"name"`
	Pkg      string             `json:"pkg"`
	DepKinds []cargoMetaDepKind `json:"dep_kinds"`
}

type cargoMetaDepKind struct {
	Kind *string `json:"kind"`
}

// ParseCargoMetadata parses the JSON output of `cargo metadata --format-version 1` (SPEC §14.2.5).
func ParseCargoMetadata(path string, data []byte) (*graph.Graph, []string, error) {
	var meta cargoMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	isMember := map[string]bool{}
	for _, id := range meta.WorkspaceMembers {
		isMember[id] = true
	}

	b := graph.NewBuilder("cargo", path)
	nodeMap := map[string]int{}

	for _, p := range meta.Packages {
		var idx int
		if isMember[p.ID] {
			idx = b.Root(p.Name, p.Version)
		} else {
			idx = b.Node(p.Name, p.Version)
		}
		nodeMap[p.ID] = idx
		nodeMap[p.Name+"@"+p.Version] = idx
	}

	var warns []string
	if meta.Resolve != nil {
		for _, node := range meta.Resolve.Nodes {
			fromIdx, ok := nodeMap[node.ID]
			if !ok {
				continue
			}
			for _, dep := range node.Deps {
				toIdx, ok := nodeMap[dep.Pkg]
				if !ok {
					continue
				}
				kind := graph.Dev
				if len(dep.DepKinds) == 0 {
					kind = graph.Prod
				} else {
					for _, dk := range dep.DepKinds {
						if dk.Kind == nil || *dk.Kind != "dev" {
							kind = graph.Prod
							break
						}
					}
				}
				b.Edge(fromIdx, toIdx, kind)
			}
		}
	}

	return finish(b, warns)
}

// parseCargoWithCLI runs `cargo metadata` for the Cargo.toml next to lockPath
// (SPEC §14.2.5). It runs in that directory, so cargo reads the project's own
// configuration, with --locked and --offline so it never rewrites Cargo.lock
// or touches the network. On failure it returns a warning and the error; the
// caller then falls back to ParseCargo.
func parseCargoWithCLI(lockPath string) (*graph.Graph, []string, error) {
	env, err := cargoEnv()
	if err != nil {
		return nil, []string{fmt.Sprintf("cargo metadata not run: %v; falling back to Cargo.lock", err)}, err
	}
	// cargo runs in the lockfile's directory, so the manifest path is relative
	// to it; joining that directory again breaks relative lockfile paths.
	out, err := runTool(filepath.Dir(lockPath), env, "cargo", "metadata", "--locked", "--offline", "--format-version", "1",
		"--manifest-path", "Cargo.toml")
	if err != nil {
		return nil, []string{fmt.Sprintf("cargo metadata failed: %v; falling back to Cargo.lock", err)}, err
	}
	return ParseCargoMetadata(lockPath, out)
}

// cargoEnv returns environment overrides that stop the project from running
// code through cargo metadata, which executes rustc to query the compiler:
//
//   - RUSTC_WRAPPER and RUSTC_WORKSPACE_WRAPPER are always cleared, disabling
//     the user's wrappers as well as the project's.
//   - RUSTC=rustc replaces .cargo/config.toml's build.rustc unless the user
//     set RUSTC.
//   - CARGO_ENCODED_RUSTFLAGS is always set empty, which overrides every other
//     rustflags source (build.rustflags, target.<triple>.rustflags,
//     CARGO_BUILD_RUSTFLAGS, RUSTFLAGS). Cargo passes rustflags to its
//     `rustc --print=cfg` query, and with an [env] RUSTC_BOOTSTRAP a project
//     could make a stable rustc load a library via -Zcodegen-backend.
//   - RUSTUP_TOOLCHAIN is set to `rustup default` unless the user set it, so a
//     rust-toolchain.toml cannot pick a `path` toolchain or download one. If
//     rustup is installed but names no default toolchain, cargoEnv returns an
//     error and cargo metadata is not run. Without rustup, toolchain files
//     are not read at all.
//
// Other project configuration is still read; see the README's warning.
func cargoEnv() ([]string, error) {
	env := []string{"RUSTC_WRAPPER=", "RUSTC_WORKSPACE_WRAPPER=", "CARGO_ENCODED_RUSTFLAGS="}
	if _, ok := os.LookupEnv("RUSTC"); !ok {
		env = append(env, "RUSTC=rustc")
	}
	if _, ok := os.LookupEnv("RUSTUP_TOOLCHAIN"); ok {
		return env, nil
	}
	if _, err := exec.LookPath("rustup"); err != nil {
		return env, nil
	}
	// `rustup default` ignores override files, so this is the user's own choice.
	out, err := runTool("", nil, "rustup", "default")
	f := strings.Fields(string(out))
	if err != nil || len(f) == 0 {
		return nil, fmt.Errorf("rustup has no default toolchain to pin, so a rust-toolchain.toml could choose one (set RUSTUP_TOOLCHAIN)")
	}
	return append(env, "RUSTUP_TOOLCHAIN="+f[0]), nil
}
