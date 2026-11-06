package graph

import (
	"cmp"
	"strings"
)

// compareVersions orders version strings from any supported ecosystem for
// output. It returns a negative number, zero or a positive number, and it is a
// total order: it returns zero only for identical strings, so sorting is
// deterministic. Keys, in order:
//
//  1. A PEP 440 epoch "N!" (absent is 0). A leading "v" (Go) is ignored.
//  2. The release, the leading dot-separated numbers, compared numerically with
//     missing segments counted as 0, so 9.0.0 < 10.0.0 and 1.0 ties 1.0.0.
//  3. The suffix after the release (leading "-", "." or "_" dropped, up to "+"):
//     dev (PEP 440 .devN) < any other suffix (semver or PEP 440 pre-release, Go
//     pseudo-version) < none < post (PEP 440 .postN). Suffixes of equal rank
//     compare naturally (see compareNatural), so alpha.2 < alpha.10.
//  4. Build metadata or a local version after "+", compared naturally; none first.
//  5. The raw strings, bytewise.
//
// Not handled: PEP 440 implicit post releases ("1.0-1" counts as a pre-release,
// as in semver) and spelling normalization ("alpha" and "a" are different words).
func compareVersions(a, b string) int {
	if a == b {
		return 0
	}
	va, vb := parseVersion(a), parseVersion(b)
	if c := compareNum(va.epoch, vb.epoch); c != 0 {
		return c
	}
	for i := 0; i < max(len(va.release), len(vb.release)); i++ {
		if c := compareNum(segment(va.release, i), segment(vb.release, i)); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(va.rank(), vb.rank()); c != 0 {
		return c
	}
	if c := compareNatural(va.suffix, vb.suffix); c != 0 {
		return c
	}
	if c := compareNatural(va.build, vb.build); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// version is a version string split into the keys compareVersions uses.
type version struct {
	epoch   string   // digits, "" when absent
	release []string // digit runs
	suffix  string   // pre-release, dev, post or any other text after the release
	build   string   // text after the first "+"
}

func parseVersion(s string) version {
	var v version
	if len(s) > 1 && (s[0] == 'v' || s[0] == 'V') && isDigit(s[1]) {
		s = s[1:]
	}
	if e, rest, ok := strings.Cut(s, "!"); ok && e != "" && strings.Trim(e, "0123456789") == "" {
		v.epoch, s = e, rest
	}
	s, v.build, _ = strings.Cut(s, "+")
	i := 0
	for i < len(s) && isDigit(s[i]) {
		j := i
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		v.release = append(v.release, s[i:j])
		i = j
		if i+1 < len(s) && s[i] == '.' && isDigit(s[i+1]) {
			i++
		}
	}
	v.suffix = strings.TrimLeft(s[i:], "-._")
	return v
}

// rank places the suffix relative to the plain release (0).
func (v version) rank() int {
	s := strings.ToLower(v.suffix)
	switch {
	case s == "":
		return 0
	case strings.HasPrefix(s, "post"):
		return 1
	case strings.HasPrefix(s, "dev"):
		return -2
	}
	return -1
}

func segment(release []string, i int) string {
	if i < len(release) {
		return release[i]
	}
	return "0"
}

// compareNum compares unsigned decimal strings of any length by value ("" is 0).
func compareNum(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if c := cmp.Compare(len(a), len(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// compareNatural compares strings as sequences of digit and non-digit runs:
// digit runs by value and before non-digit runs (as semver orders numeric
// identifiers first), non-digit runs bytewise, and a prefix first.
func compareNatural(a, b string) int {
	for a != "" && b != "" {
		ta, tb := run(a), run(b)
		da, db := isDigit(ta[0]), isDigit(tb[0])
		var c int
		switch {
		case da && db:
			c = compareNum(ta, tb)
		case da:
			c = -1
		case db:
			c = 1
		default:
			c = strings.Compare(ta, tb)
		}
		if c != 0 {
			return c
		}
		a, b = a[len(ta):], b[len(tb):]
	}
	return cmp.Compare(len(a), len(b))
}

// run returns the leading run of digits or of non-digits of a non-empty s.
func run(s string) string {
	d := isDigit(s[0])
	i := 1
	for i < len(s) && isDigit(s[i]) == d {
		i++
	}
	return s[:i]
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
