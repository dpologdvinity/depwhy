package render

import (
	"os"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// Style controls ANSI output. The zero value prints plain text, which is what
// tests and pipes get.
type Style struct {
	Color bool
	// Notes holds online findings for the graph being rendered (nil when offline).
	// A Style is cheap to copy, so callers make one per graph.
	Notes Notes
}

// wrap surrounds str with an ANSI SGR code, or returns it unchanged when color
// is off.
func (s Style) wrap(code, str string) string {
	if !s.Color {
		return str
	}
	return "\x1b[" + code + "m" + str + "\x1b[0m"
}

// Bold styles headings.
func (s Style) Bold(str string) string { return s.wrap("1", str) }

// Cyan styles root labels.
func (s Style) Cyan(str string) string { return s.wrap("36", str) }

// Red marks security advisories.
func (s Style) Red(str string) string { return s.wrap("31", str) }

// Yellow marks deprecated and yanked packages.
func (s Style) Yellow(str string) string { return s.wrap("33", str) }

// Dim de-emphasizes markers such as (*) and the dev kind.
func (s Style) Dim(str string) string { return s.wrap("2", str) }

// kind prints an edge kind, dimming dev.
func (s Style) kind(k graph.Kind) string {
	if k == graph.Dev {
		return s.Dim(k.String())
	}
	return k.String()
}

// Enabled reports whether to use color when writing to f (SPEC §7.4): f must be
// a terminal, NO_COLOR must be unset or empty, and --no-color must not be set.
func Enabled(f *os.File, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
