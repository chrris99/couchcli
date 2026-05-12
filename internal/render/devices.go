// Package render formats CLI output for couchcli commands.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/chrris99/couchcli/internal/discovery"
)

// Devices writes the discovered device list as either pretty table or JSON.
func Devices(w io.Writer, devices []discovery.Device, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if devices == nil {
			devices = []discovery.Device{}
		}
		return enc.Encode(devices)
	}
	return prettyDevices(w, devices)
}

func prettyDevices(w io.Writer, devices []discovery.Device) error {
	if len(devices) == 0 {
		_, err := fmt.Fprintln(w, "No devices found.")
		return err
	}
	withProbe := false
	for _, d := range devices {
		if d.Model != "" || d.APIVersion != 0 {
			withProbe = true
			break
		}
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if withProbe {
		fmt.Fprintln(tw, "NAME\tADDRESS\tPORT\tSOURCE\tMODEL\tAPI")
		for _, d := range devices {
			api := ""
			if d.APIVersion > 0 {
				api = fmt.Sprintf("v%d", d.APIVersion)
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
				truncate(d.Name, 40),
				d.Address,
				d.Port,
				shortSource(d.Source),
				truncate(d.Model, 30),
				api,
			)
		}
	} else {
		fmt.Fprintln(tw, "NAME\tADDRESS\tPORT\tSOURCE")
		for _, d := range devices {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
				truncate(d.Name, 40),
				d.Address,
				d.Port,
				shortSource(d.Source),
			)
		}
	}
	return tw.Flush()
}

// shortSource renders a source identifier for the table column.
func shortSource(s string) string {
	if rest, ok := strings.CutPrefix(s, "mdns:"); ok {
		return rest
	}
	return s
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
