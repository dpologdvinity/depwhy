package size

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Scanner measures on-disk package sizes and caches results per path.
type Scanner struct {
	mu    sync.RWMutex
	cache map[string]int64
}

// NewScanner returns a fresh size Scanner.
func NewScanner() *Scanner {
	return &Scanner{
		cache: make(map[string]int64),
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

// DirSize recursively calculates total bytes of all regular files in dir.
// Symlinks outside the directory are not followed.
func (s *Scanner) DirSize(dir string) (int64, error) {
	s.mu.RLock()
	if cached, ok := s.cache[dir]; ok {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	info, err := os.Lstat(dir)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		size := info.Size()
		s.store(dir, size)
		return size, nil
	}

	var total int64
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil // skip unreadable files gracefully
		}
		total += fi.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}

	s.store(dir, total)
	return total, nil
}

func (s *Scanner) store(path string, size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[path] = size
}

// NPMSize measures the size of an installed npm package given its lockfile key
// (e.g. "node_modules/foo").
func (s *Scanner) NPMSize(projectDir, packageKey string) (int64, error) {
	if packageKey == "" || packageKey == "." {
		return 0, nil
	}
	targetDir := filepath.Join(projectDir, filepath.FromSlash(packageKey))
	return s.DirSize(targetDir)
}

// PythonSize measures the size of an installed Python package in a site-packages directory.
func (s *Scanner) PythonSize(sitePackagesDir, name, version string) (int64, error) {
	cacheKey := filepath.Join(sitePackagesDir, fmt.Sprintf("%s@%s", name, version))
	s.mu.RLock()
	if cached, ok := s.cache[cacheKey]; ok {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	// Try PEP 376 / 427 dist-info naming: name can use _ or -
	candidates := []string{
		strings.ReplaceAll(name, "-", "_"),
		strings.ReplaceAll(name, "_", "-"),
	}

	for _, cand := range candidates {
		distInfoName := fmt.Sprintf("%s-%s.dist-info", cand, version)
		distInfoPath := filepath.Join(sitePackagesDir, distInfoName)
		if fi, err := os.Stat(distInfoPath); err == nil && fi.IsDir() {
			recordFile := filepath.Join(distInfoPath, "RECORD")
			if size, err := s.measureRecord(sitePackagesDir, recordFile); err == nil {
				s.store(cacheKey, size)
				return size, nil
			}
			// Fallback: sum dist-info and candidate package dir
			var total int64
			if dSize, err := s.DirSize(distInfoPath); err == nil {
				total += dSize
			}
			pkgDir := filepath.Join(sitePackagesDir, cand)
			if pSize, err := s.DirSize(pkgDir); err == nil {
				total += pSize
			}
			if total > 0 {
				s.store(cacheKey, total)
				return total, nil
			}
		}
	}

	// Direct directory fallback
	for _, cand := range candidates {
		pkgDir := filepath.Join(sitePackagesDir, cand)
		if fi, err := os.Stat(pkgDir); err == nil && fi.IsDir() {
			return s.DirSize(pkgDir)
		}
	}

	return 0, fmt.Errorf("package %s@%s not found in %s", name, version, sitePackagesDir)
}

func (s *Scanner) measureRecord(sitePackagesDir, recordFile string) (int64, error) {
	f, err := os.Open(recordFile)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var total int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ",")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		relPath := parts[0]
		// If 3rd field is numeric size, use it directly
		if len(parts) >= 3 && parts[2] != "" {
			if n, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
				total += n
				continue
			}
		}
		// Otherwise check file on disk
		fullPath := filepath.Join(sitePackagesDir, filepath.FromSlash(relPath))
		if fi, err := os.Lstat(fullPath); err == nil && fi.Mode().IsRegular() {
			total += fi.Size()
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return total, nil
}

// FindPythonSitePackages discovers the active site-packages directory inside a virtualenv.
func FindPythonSitePackages(projectDir, venvOverride string) (string, error) {
	venv := venvOverride
	if venv == "" {
		venv = filepath.Join(projectDir, ".venv")
	}
	if fi, err := os.Stat(venv); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("virtualenv not found: %s", venv)
	}

	// 1. Unix standard: lib/pythonX.Y/site-packages
	matches, _ := filepath.Glob(filepath.Join(venv, "lib", "python*", "site-packages"))
	if len(matches) > 0 {
		return matches[0], nil
	}

	// 2. Windows standard: Lib/site-packages
	winSite := filepath.Join(venv, "Lib", "site-packages")
	if fi, err := os.Stat(winSite); err == nil && fi.IsDir() {
		return winSite, nil
	}

	// 3. Fallback: lib/site-packages
	altSite := filepath.Join(venv, "lib", "site-packages")
	if fi, err := os.Stat(altSite); err == nil && fi.IsDir() {
		return altSite, nil
	}

	return "", fmt.Errorf("no site-packages found in virtualenv %s", venv)
}
