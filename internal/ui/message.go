package ui

import (
	"fmt"
	"io"

	lipgloss "charm.land/lipgloss/v2"
)

// Message glyphs. A colored glyph prefixes the line; the message text stays
// default-colored so it reads cleanly on any background.
const (
	glyphSuccess = "✓"
	glyphWarn    = "⚠"
	glyphError   = "✗"
)

// Successf writes a ✓-prefixed line to w (typically stdout).
func Successf(w io.Writer, format string, args ...any) {
	glyphLine(w, Success, glyphSuccess, format, args...)
}

// Warnf writes a ⚠-prefixed line to w — pass a stderr writer for diagnostics
// that must not pollute stdout / the --json contract.
func Warnf(w io.Writer, format string, args ...any) {
	glyphLine(w, Warn, glyphWarn, format, args...)
}

// Errorf writes a ✗-prefixed line to w. For non-fatal notices printed mid-
// command; fatal errors should be returned so fang renders them.
func Errorf(w io.Writer, format string, args ...any) {
	glyphLine(w, Error, glyphError, format, args...)
}

// Infof writes a plain line through the color-profile writer, so it shares the
// same output path as the styled helpers (ANSI stripped when piped).
func Infof(w io.Writer, format string, args ...any) {
	fmt.Fprintln(Out(w), fmt.Sprintf(format, args...))
}

func glyphLine(w io.Writer, style lipgloss.Style, glyph, format string, args ...any) {
	fmt.Fprintf(Out(w), "%s %s\n", style.Render(glyph), fmt.Sprintf(format, args...))
}
