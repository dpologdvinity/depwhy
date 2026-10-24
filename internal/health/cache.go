// Package health looks up online information about packages: known security
// advisories (from OSV) and deprecated or yanked status (from the npm, PyPI and
// crates.io registries). It implements SPEC §12.
//
// Lookups never fail a run. Network trouble is reported as warnings and the
// affected packages simply come back without information. Responses are cached
// on disk so repeated runs do not hit the network.
package health

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Pkg identifies one package version. Ecosystem uses depwhy's names: "npm",
// "python" or "cargo".
type Pkg struct{ Ecosystem, Name, Version string }

// Info is what is known about a package from the network.
type Info struct {
	Advisories []string `json:"advisories"`           // advisory IDs such as GHSA-xxxx, sorted
	Deprecated string   `json:"deprecated,omitempty"` // npm deprecation message
	Yanked     bool     `json:"yanked,omitempty"`     // yanked from PyPI or crates.io
}

// Cache stores Info per package as small JSON files. A nil *Cache is valid and
// never hits. Entries older than TTL count as misses, as do unreadable files.
type Cache struct {
	Dir string
	TTL time.Duration
	Now func() time.Time
}

// cacheFile is the on-disk form of one entry.
type cacheFile struct {
	Fetched time.Time `json:"fetched"`
	Info    Info      `json:"info"`
}

// DefaultCacheDir returns $XDG_CACHE_HOME/depwhy, falling back to the OS user
// cache directory. It returns "" when neither is available, which disables caching.
func DefaultCacheDir() string {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "depwhy")
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "depwhy")
	}
	return ""
}

// path returns the entry's file. The name is a hash of the package, so scoped
// names and characters that are invalid in paths are harmless.
func (c *Cache) path(p Pkg) string {
	// NUL cannot occur in a name or version, so different packages never share a key.
	sum := sha256.Sum256([]byte(p.Ecosystem + "\x00" + p.Name + "\x00" + p.Version))
	return filepath.Join(c.Dir, hex.EncodeToString(sum[:])+".json")
}

// Get returns a fresh cached entry for p.
func (c *Cache) Get(p Pkg) (Info, bool) {
	if c == nil {
		return Info{}, false
	}
	data, err := os.ReadFile(c.path(p))
	if err != nil {
		return Info{}, false
	}
	var f cacheFile
	if json.Unmarshal(data, &f) != nil {
		return Info{}, false
	}
	// A negative age means the entry is stamped in the future (clock change or a
	// hand-edited file); never trust it indefinitely.
	if age := c.Now().Sub(f.Fetched); age < 0 || age >= c.TTL {
		return Info{}, false
	}
	return f.Info, true
}

// Put stores info for p. Errors are ignored: a cache that cannot be written only
// costs speed. The file is written to a temporary name first and renamed, so a
// reader never sees a half-written entry.
func (c *Cache) Put(p Pkg, info Info) {
	if c == nil {
		return
	}
	if os.MkdirAll(c.Dir, 0o700) != nil {
		return
	}
	data, err := json.Marshal(cacheFile{Fetched: c.Now().UTC(), Info: info})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(c.Dir, "tmp-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), c.path(p)) != nil {
		_ = os.Remove(tmp.Name())
	}
}
