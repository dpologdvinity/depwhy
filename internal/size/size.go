// Package size measures the on-disk size of installed packages (SPEC §13).
//
// Every path that comes from a lockfile (npm install keys, package names) or
// from an installed package (Python RECORD entries) is untrusted: the scanner
// only measures paths that stay inside their base directory after symlinks
// are resolved, never follows symlinks while walking, and reports a package as
// unknown rather than measuring a directory it cannot positively identify.
package size

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Scanner measures package sizes and caches directory sizes, directory
// listings and site-packages indexes, so one Scanner should serve a whole run.
// It is safe for concurrent use.
type Scanner struct {
	mu    sync.Mutex
	sizes map[string]int64             // measured sizes by path (and mode)
	lists map[string][]string          // directory listings by path
	dists map[string]map[string]string // site-packages → "name@version" → dist-info dir
	recs  map[string]recordSize        // RECORD totals by dist-info dir
}

// recordSize is a cached RECORD measurement: the total, or why it failed.
type recordSize struct {
	n int64
	e error
}

// NewScanner returns a fresh size Scanner.
func NewScanner() *Scanner {
	return &Scanner{
		sizes: map[string]int64{},
		lists: map[string][]string{},
		dists: map[string]map[string]string{},
		recs:  map[string]recordSize{},
	}
}

// Format formats a byte count into a human-readable string with units (base 1024).
func Format(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	val := float64(b) / float64(div)
	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", val, units[exp])
}

// DirSize returns the total size of the regular files under dir, or the size
// of dir itself when it is a regular file. Symlinks are never followed, and a
// symlink passed as dir is an error rather than the length of its target text.
func (s *Scanner) DirSize(dir string) (int64, error) {
	return s.measure(dir, false)
}

// measure is DirSize; with skipNested it also skips every node_modules
// directory below dir, which holds other npm packages, not this one.
func (s *Scanner) measure(dir string, skipNested bool) (int64, error) {
	key := dir
	if skipNested {
		key += "\x00nested"
	}
	s.mu.Lock()
	cached, ok := s.sizes[key]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	switch {
	case info.Mode().IsRegular():
		total = info.Size()
	case !info.IsDir():
		return 0, fmt.Errorf("%s is not a directory or regular file", dir)
	default:
		err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				if path == dir {
					return err
				}
				return nil // unreadable entry below the root: skip it
			case skipNested && d.IsDir() && d.Name() == "node_modules" && path != dir:
				return filepath.SkipDir
			case !d.Type().IsRegular():
				return nil // directories are walked; symlinks and devices are not counted
			}
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	s.sizes[key] = total
	s.mu.Unlock()
	return total, nil
}

// within reports whether path p lies in (or is) directory base. Both must be
// clean, symlink-free paths.
func within(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	return err == nil && filepath.IsLocal(rel)
}

// resolveWithin joins rel to base and resolves symlinks. It fails when rel is
// not a local path or the resolved path leaves base; it is the guard for every
// lockfile-controlled path.
func resolveWithin(base, rel string) (string, error) {
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("path %q leaves %s", rel, base)
	}
	// Absolute first: an absolute symlink target never relates to a relative base.
	base, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(base, rel))
	if err != nil {
		return "", err
	}
	if !within(realBase, real) {
		return "", fmt.Errorf("path %q resolves outside %s", rel, base)
	}
	return real, nil
}

// list returns the sorted entry names of dir, cached; a missing dir is empty.
func (s *Scanner) list(dir string) []string {
	s.mu.Lock()
	names, ok := s.lists[dir]
	s.mu.Unlock()
	if ok {
		return names
	}
	entries, _ := os.ReadDir(dir) // sorted by name
	for _, e := range entries {
		names = append(names, e.Name())
	}
	s.mu.Lock()
	s.lists[dir] = names
	s.mu.Unlock()
	return names
}

// NPMSize measures an installed npm-ecosystem package in projectDir, without
// the nested node_modules directory that holds its own dependencies.
//
// installPath is the package-lock.json "packages" key ("node_modules/a" or
// "node_modules/a/node_modules/c"), slash-separated and relative to
// projectDir. When it is empty (pnpm, yarn, bun) the candidates are pnpm's
// virtual store (node_modules/.pnpm/<name>@<version>[_peers]/node_modules/<name>)
// and then node_modules/<name>, following its symlink if it has one.
//
// A candidate is measured only if it stays inside projectDir after symlinks
// are resolved and its package.json has exactly this name and version, so a
// hoisted copy of another version, a stale install or an alias never lends
// its size to the wrong package. Otherwise the size is unknown (an error).
func (s *Scanner) NPMSize(projectDir, installPath, name, version string) (int64, error) {
	if name == "" {
		return 0, errors.New("empty package name")
	}
	cands := []string{installPath}
	if installPath == "" {
		store := filepath.Join(projectDir, "node_modules", ".pnpm")
		prefix := strings.ReplaceAll(name, "/", "+") + "@" + version
		cands = nil
		for _, e := range s.list(store) {
			if e == prefix || strings.HasPrefix(e, prefix+"_") {
				cands = append(cands, "node_modules/.pnpm/"+e+"/node_modules/"+name)
			}
		}
		cands = append(cands, "node_modules/"+name)
	}
	for _, rel := range cands {
		dir, err := resolveWithin(projectDir, filepath.FromSlash(rel))
		if err != nil || !npmManifestIs(dir, name, version) {
			continue
		}
		return s.measure(dir, true)
	}
	return 0, fmt.Errorf("%s@%s is not installed in %s", name, version, projectDir)
}

// npmManifestIs reports whether dir/package.json is a regular file naming
// exactly name@version.
func npmManifestIs(dir, name, version string) bool {
	p := filepath.Join(dir, "package.json")
	if fi, err := os.Lstat(p); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	var m struct{ Name, Version string }
	return json.Unmarshal(data, &m) == nil && m.Name == name && m.Version == version
}

// separators matches the runs of characters PEP 503 treats as one separator.
var separators = regexp.MustCompile(`[-_.]+`)

// normalize returns the PEP 503 normalized form of a Python project name:
// lowercase, with every run of "-", "_" and "." replaced by "-".
func normalize(name string) string {
	return separators.ReplaceAllString(strings.ToLower(name), "-")
}

// distInfos indexes the *.dist-info directories of sitePackages by normalized
// name and lowercased version ("pyyaml@6.0.1" → "PyYAML-6.0.1.dist-info").
// The directory is listed once per Scanner.
func (s *Scanner) distInfos(sitePackages string) map[string]string {
	s.mu.Lock()
	idx, ok := s.dists[sitePackages]
	s.mu.Unlock()
	if ok {
		return idx
	}
	idx = map[string]string{}
	for _, e := range s.list(sitePackages) {
		base, ok := strings.CutSuffix(e, ".dist-info")
		// The version never contains "-", so the last one separates it.
		i := strings.LastIndex(base, "-")
		if !ok || i <= 0 {
			continue
		}
		key := normalize(base[:i]) + "@" + strings.ToLower(base[i+1:])
		if _, dup := idx[key]; !dup {
			idx[key] = e
		}
	}
	s.mu.Lock()
	s.dists[sitePackages] = idx
	s.mu.Unlock()
	return idx
}

// PythonSize measures an installed Python distribution: the regular files its
// dist-info RECORD lists, found by PEP 503 normalized name and version.
// RECORD entries are relative to sitePackages; entries that resolve outside
// venvRoot are not counted, so console scripts in the venv's bin directory
// count but nothing outside the virtualenv ever does. A distribution without
// a dist-info directory or RECORD is unknown (an error).
func (s *Scanner) PythonSize(sitePackages, venvRoot, name, version string) (int64, error) {
	dist, ok := s.distInfos(sitePackages)[normalize(name)+"@"+strings.ToLower(version)]
	if !ok {
		return 0, fmt.Errorf("%s@%s is not installed in %s", name, version, sitePackages)
	}
	s.mu.Lock()
	r, ok := s.recs[filepath.Join(sitePackages, dist)]
	s.mu.Unlock()
	if ok {
		return r.n, r.e
	}
	r.n, r.e = measureRecord(sitePackages, venvRoot, filepath.Join(sitePackages, dist, "RECORD"))
	s.mu.Lock()
	s.recs[filepath.Join(sitePackages, dist)] = r
	s.mu.Unlock()
	return r.n, r.e
}

// measureRecord sums the on-disk sizes of the regular files listed in a
// RECORD file (CSV: path, hash, size). Recorded sizes are not trusted; each
// file is checked on disk and counted once.
func measureRecord(sitePackages, venvRoot, record string) (int64, error) {
	// Absolute paths, so resolved directories compare with the resolved root.
	venvRoot, err := filepath.Abs(venvRoot)
	if err != nil {
		return 0, err
	}
	if sitePackages, err = filepath.Abs(sitePackages); err != nil {
		return 0, err
	}
	root, err := filepath.EvalSymlinks(venvRoot)
	if err != nil {
		return 0, err
	}
	f, err := os.Open(record)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	realDirs := map[string]string{} // directory → resolved directory ("" if unresolvable)
	seen := map[string]bool{}
	var total int64
	counted := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("%s: %w", record, err)
		}
		if rec[0] == "" {
			continue
		}
		p := filepath.FromSlash(rec[0])
		if !filepath.IsAbs(p) {
			p = filepath.Join(sitePackages, p)
		}
		// Resolve the directory (symlinked dirs such as lib64 are common) and
		// require the file itself to be a regular file, not a symlink.
		dir := filepath.Dir(p)
		real, ok := realDirs[dir]
		if !ok {
			real, _ = filepath.EvalSymlinks(dir)
			realDirs[dir] = real
		}
		if real == "" || !within(root, real) {
			continue
		}
		p = filepath.Join(real, filepath.Base(p))
		if seen[p] {
			continue
		}
		seen[p] = true
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
			total += fi.Size()
			counted++
		}
	}
	if counted == 0 {
		// A real RECORD lists at least its own dist-info files.
		return 0, fmt.Errorf("%s: no listed file is installed", record)
	}
	return total, nil
}

// FindPythonSitePackages finds the site-packages directory of a virtualenv:
// venvOverride when set, else projectDir/.venv. It returns the site-packages
// path and the virtualenv root.
func FindPythonSitePackages(projectDir, venvOverride string) (sitePackages, venvRoot string, err error) {
	venv := venvOverride
	if venv == "" {
		venv = filepath.Join(projectDir, ".venv")
	}
	if fi, err := os.Stat(venv); err != nil || !fi.IsDir() {
		return "", "", fmt.Errorf("virtualenv not found: %s", venv)
	}
	// Unix lib/pythonX.Y/site-packages, then Windows Lib/site-packages, then lib/site-packages.
	cands, _ := filepath.Glob(filepath.Join(venv, "lib", "python*", "site-packages"))
	cands = append(cands, filepath.Join(venv, "Lib", "site-packages"), filepath.Join(venv, "lib", "site-packages"))
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c, venv, nil
		}
	}
	return "", "", fmt.Errorf("no site-packages found in virtualenv %s", venv)
}
