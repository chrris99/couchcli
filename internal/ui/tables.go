package ui

import (
	"fmt"
	"io"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/chrris99/couchcli/internal/device"
)

// DeviceRow is one paired device prepared for display; cmd maps registry
// entries (and their driver-owned settings) into rows so this package stays
// driver-agnostic.
type DeviceRow struct {
	Alias   string
	Label   string
	Room    string
	Driver  string
	Address string
	Model   string
	Default bool
}

// DeviceTable renders the paired-device list.
func DeviceTable(w io.Writer, rows []DeviceRow) {
	out := Out(w)
	if len(rows) == 0 {
		fmt.Fprintln(out, Subtle.Render("No paired devices — run `couch pair` to add one."))
		return
	}
	t := newTable("", "ALIAS", "LABEL", "ROOM", "DRIVER", "ADDRESS", "MODEL")
	for _, r := range rows {
		def := " "
		if r.Default {
			def = Accent.Render("●")
		}
		t.Row(def, r.Alias, truncate(r.Label, 30), truncate(r.Room, 20), r.Driver, r.Address, truncate(r.Model, 30))
	}
	fmt.Fprintln(out, t.Render())
}

// CandidateTable renders discovery results.
func CandidateTable(w io.Writer, candidates []device.Candidate) {
	out := Out(w)
	if len(candidates) == 0 {
		fmt.Fprintln(out, Subtle.Render("No devices found."))
		return
	}
	t := newTable("NAME", "ADDRESS", "DRIVER", "MODEL", "API")
	for _, c := range candidates {
		api := c.Hints["api"]
		if api != "" {
			api = "v" + api
		}
		name := c.Name
		if strings.TrimSpace(name) == "" {
			name = Subtle.Render("(unnamed)")
		}
		t.Row(truncate(name, 40), c.Address, string(c.Driver), truncate(c.Hints["model"], 30), api)
	}
	fmt.Fprintln(out, t.Render())
}

func newTable(headers ...string) *table.Table {
	headerStyle := lipgloss.NewStyle().Foreground(accent).Bold(true).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)
	return table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(subtle)).
		Headers(headers...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
