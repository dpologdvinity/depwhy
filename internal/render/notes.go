package render

import (
	"strings"
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

// suffix returns the text appended after a package's name@version, such as
// "  ⚠ GHSA-1, GHSA-2  [deprecated: use b instead]", or "" when there is nothing
// to show (SPEC §12.6).
func (s Style) suffix(node int) string {
	n, ok := s.Notes[node]
	if !ok {
		return ""
	}
	var sb strings.Builder
	if len(n.Advisories) > 0 {
		sb.WriteString("  " + s.Red("⚠ "+strings.Join(n.Advisories, ", ")))
	}
	if n.Deprecated != "" {
		sb.WriteString("  " + s.Yellow("[deprecated: "+tidy(n.Deprecated)+"]"))
	}
	if n.Yanked {
		sb.WriteString("  " + s.Yellow("[yanked]"))
	}
	return sb.String()
}

// tidy collapses all whitespace runs (including newlines) to single spaces and
// shortens the message to maxDeprecation characters.
func tidy(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if utf8.RuneCountInString(msg) <= maxDeprecation {
		return msg
	}
	return string([]rune(msg)[:maxDeprecation-1]) + "…"
}
