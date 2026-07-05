// Package ui is couchcli's human-facing terminal surface: one palette plus
// tables, prompts, and spinners built on it.
package ui

import (
	"io"
	"os"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

var (
	accent = lipgloss.Color("212")
	subtle = lipgloss.Color("243")
	green  = lipgloss.Color("42")
	orange = lipgloss.Color("214")
	red    = lipgloss.Color("203")
)

var (
	Title   = lipgloss.NewStyle().Bold(true)
	Accent  = lipgloss.NewStyle().Foreground(accent)
	Subtle  = lipgloss.NewStyle().Foreground(subtle)
	Success = lipgloss.NewStyle().Foreground(green)
	Warn    = lipgloss.NewStyle().Foreground(orange)
	Error   = lipgloss.NewStyle().Foreground(red)
)

// Out wraps w so styled output is downsampled to the terminal's color
// profile, and stripped entirely when w is not a terminal.
func Out(w io.Writer) io.Writer { return colorprofile.NewWriter(w, nil) }

// Interactive reports whether stdin and stderr are terminals, used as a
// precondition for prompts and spinners.
func Interactive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stderr.Fd())
}
