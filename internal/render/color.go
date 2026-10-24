package render

import (
	"os"

	"github.com/dpologdvinity/depwhy/internal/graph"
)

// Style controls ANSI output. The zero value prints plain text.
type Style struct{ Color bool }

func (s Style) wrap(code, str string) string {
	if !s.Color {
		return str
	}
	return "\x1b[" + code + "m" + str + "\x1b[0m"
}

func (s Style) Bold(str string) string { return s.wrap("1", str) }
func (s Style) Cyan(str string) string { return s.wrap("36", str) }
func (s Style) Dim(str string) string  { return s.wrap("2", str) }

// kind prints an edge kind, dimming dev.
func (s Style) kind(k graph.Kind) string {
	if k == graph.Dev {
		return s.Dim(k.String())
	}
	return k.String()
}

// Enabled reports whether to use color on f (SPEC §7.4).
func Enabled(f *os.File, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
