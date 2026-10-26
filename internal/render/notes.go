package render

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxDeprecation is the longest deprecation message shown in text output; longer
// ones are cut with an ellipsis. JSON always carries the full message.
const maxDeprecation = 80

// Note holds the online findings for one package (SPEC §12).
type Note struct {
	Advisories []string // advisory IDs, already sorted
	Deprecated string   // npm deprecation message, "" if not deprecated
	Yanked     bool     // yanked from PyPI or crates.io
}

// Notes maps a node index of the rendered graph to its Note. A nil Notes means
// the run was offline; a non-nil one (even if empty) means --online.
type Notes map[int]Note

// suffix returns the text appended after a package's name@version, including
// size annotation and online findings.
func (s Style) suffix(node int) string {
	var sb strings.Builder
	if s.Sizes != nil {
		if sz, ok := s.Sizes[node]; ok {
			sb.WriteString(" (" + FormatSize(sz) + ")")
		}
	}
	sb.WriteString(s.suffixOnline(node))
	return sb.String()
}

// suffixOnline returns the online findings suffix, or "" when there is none.
func (s Style) suffixOnline(node int) string {
	n, ok := s.Notes[node]
	if !ok {
		return ""
	}
	var sb strings.Builder
	if len(n.Advisories) > 0 {
		ids := make([]string, len(n.Advisories))
		for i, id := range n.Advisories {
			ids[i] = printable(id)
		}
		sb.WriteString("  " + s.Red("⚠ "+strings.Join(ids, ", ")))
	}
	if n.Deprecated != "" {
		sb.WriteString("  " + s.Yellow("[deprecated: "+tidy(n.Deprecated)+"]"))
	}
	if n.Yanked {
		sb.WriteString("  " + s.Yellow("[yanked]"))
	}
	return sb.String()
}

// printable turns every whitespace character into a plain space and removes
// control and invisible formatting characters (escape sequences, DEL,
// bidirectional overrides). Advisory IDs and deprecation messages
// come from public services that anyone can write to, and must not be able to
// drive the user's terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

// tidy makes a deprecation message safe for one line of terminal output: it
// drops non-printable characters, collapses whitespace runs (including
// newlines) to single spaces, and shortens it to maxDeprecation characters.
func tidy(msg string) string {
	msg = strings.Join(strings.Fields(printable(msg)), " ")
	if utf8.RuneCountInString(msg) <= maxDeprecation {
		return msg
	}
	return string([]rune(msg)[:maxDeprecation-1]) + "…"
}
