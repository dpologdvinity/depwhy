package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// DetectRecursive discovers lockfiles in dir and its non-ignored descendants.
// Paths use forward slashes and are relative to dir. Directories are visited in
// lexical depth-first order, with Detect's ecosystem priority in each directory.
// Directory symlinks are never followed, and filesystem errors stop the walk.
func DetectRecursive(dir, ecosystem string) ([]Found, []string, error) {
	var found []Found
	var warns []string
	var walk func(string, []ignoreRule) error
	walk = func(rel string, rules []ignoreRule) error {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		entries, err := os.ReadDir(full)
		if err != nil {
			return err
		}
		rules, err = readIgnore(full, rel, rules)
		if err != nil {
			return err
		}
		files, ws := detect(full, ecosystem, func(name string) bool {
			return ignored(path.Join(rel, name), false, rules)
		})
		for _, f := range files {
			f.Path = path.Join(rel, filepath.Base(f.Path))
			found = append(found, f)
		}
		for _, w := range ws {
			if rel != "" {
				w = rel + ": " + w
			}
			warns = append(warns, w)
		}
		for _, e := range entries {
			if !e.IsDir() || skipDir(e.Name()) {
				continue
			}
			next := path.Join(rel, e.Name())
			if !ignored(next, true, rules) {
				if err := walk(next, rules); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk("", nil); err != nil {
		return nil, nil, err
	}
	if len(found) == 0 {
		return nil, nil, fmt.Errorf("no supported lockfile in %s or its subdirectories", dir)
	}
	return found, warns, nil
}

// skipDir lists dependency, VCS and build directories that are always pruned.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", ".venv", "venv", "target", "dist", "build", "vendor", "__pycache__", ".tox":
		return true
	}
	return false
}

type ignoreRule struct {
	base            string
	pattern         []*regexp.Regexp // nil segment means **
	negate, dirOnly bool
	anchored        bool
}

// readIgnore appends this directory's rules after inherited rules, so later
// matches win. Pruned directories cannot be re-included, just as in Git.
func readIgnore(dir, rel string, rules []ignoreRule) ([]ignoreRule, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if errors.Is(err, os.ErrNotExist) {
		return rules, nil
	}
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		for strings.HasSuffix(line, " ") {
			prefix := strings.TrimRight(line[:len(line)-1], "\\")
			if (len(line)-1-len(prefix))%2 == 1 {
				break
			}
			line = strings.TrimSuffix(line, " ")
		}
		if line == "" || line[0] == '#' {
			continue
		}
		r := ignoreRule{base: rel}
		r.negate = strings.HasPrefix(line, "!")
		if r.negate {
			line = line[1:]
		}
		r.dirOnly = strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		r.anchored = strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line != "" {
			r.pattern = compileIgnore(line)
		}
		if len(r.pattern) > 0 {
			rules = append(rules, r)
		}
	}
	return rules, nil
}

// ignored evaluates rules scoped to the path's ancestor directories.
func ignored(rel string, dir bool, rules []ignoreRule) bool {
	match := false
	for _, r := range rules {
		if r.dirOnly && !dir {
			continue
		}
		local := rel
		if r.base != "" {
			var ok bool
			local, ok = strings.CutPrefix(rel, r.base+"/")
			if !ok {
				continue
			}
		}
		if !r.anchored {
			local = path.Base(local)
		}
		if matchIgnore(r.pattern, strings.Split(local, "/")) {
			match = !r.negate
		}
	}
	return match
}

// matchIgnore bounds wildcard matching to O(pattern segments × path segments).
func matchIgnore(pattern []*regexp.Regexp, parts []string) bool {
	states := make([]bool, len(parts)+1)
	states[0] = true
	for i, p := range pattern {
		next := make([]bool, len(states))
		if p == nil {
			if i < len(pattern)-1 {
				copy(next, states) // A non-final ** can consume zero directories.
			}
			for j := 1; j < len(states); j++ {
				next[j] = next[j] || states[j-1] || next[j-1]
			}
		} else {
			for j := 1; j < len(states); j++ {
				next[j] = states[j-1] && p.MatchString(parts[j-1])
			}
		}
		states = next
	}
	return states[len(parts)]
}

// compileIgnore compiles each segment once. Invalid patterns never match.
func compileIgnore(pattern string) []*regexp.Regexp {
	var parts []*regexp.Regexp
	for part := range strings.SplitSeq(pattern, "/") {
		if part == "**" {
			parts = append(parts, nil)
			continue
		}
		r, err := compileGlob(part)
		if err != nil {
			return nil
		}
		parts = append(parts, r)
	}
	return parts
}

// compileGlob translates a Git shell glob into a linear-time regexp. Unlike
// path.Match, regexps support POSIX classes and a literal ] first in a class.
func compileGlob(glob string) (*regexp.Regexp, error) {
	var re strings.Builder
	re.WriteString("(?s)^")
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '*':
			re.WriteString(".*")
		case '?':
			re.WriteByte('.')
		case '\\':
			i++
			if i == len(glob) {
				return nil, fmt.Errorf("trailing glob escape")
			}
			re.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		case '[':
			end := i + 1
			if end < len(glob) && (glob[end] == '!' || glob[end] == '^') {
				end++
			}
			if end < len(glob) && glob[end] == ']' {
				end++
			}
			for end < len(glob) && glob[end] != ']' {
				switch {
				case glob[end] == '\\':
					end += 2
				case strings.HasPrefix(glob[end:], "[:"):
					n := strings.Index(glob[end+2:], ":]")
					if n < 0 {
						return nil, fmt.Errorf("unclosed glob class")
					}
					end += n + 4
				default:
					end++
				}
			}
			if end >= len(glob) {
				return nil, fmt.Errorf("unclosed glob class")
			}
			re.WriteByte('[')
			for j := i + 1; j < end; j++ {
				if j == i+1 && (glob[j] == '!' || glob[j] == '^') {
					re.WriteByte('^')
					continue
				}
				if glob[j] == '\\' {
					j++
					if strings.ContainsRune(`-[]^\`, rune(glob[j])) {
						re.WriteByte('\\')
					}
				}
				re.WriteByte(glob[j])
			}
			re.WriteByte(']')
			i = end
		default:
			re.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}
	re.WriteByte('$')
	return regexp.Compile(re.String())
}
