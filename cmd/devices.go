package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	philipsdriver "github.com/chrris99/couchcli/internal/drivers/philips"
	"github.com/chrris99/couchcli/internal/registry"
	"github.com/chrris99/couchcli/internal/ui"
)

var devicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List paired devices",
	Long:  "List devices that have been paired with couchcli, stored in the local registry file.",
	RunE:  runDevices,
}

func init() {
	rootCmd.AddCommand(devicesCmd)
}

func runDevices(cmd *cobra.Command, _ []string) error {
	reg, err := registry.Load()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	aliases := reg.Aliases()

	if jsonOutput {
		out := make([]map[string]any, 0, len(aliases))
		for _, a := range aliases {
			d := reg.Devices[a]
			row := map[string]any{
				"alias":     a,
				"default":   a == reg.Default,
				"driver":    d.Driver,
				"address":   d.Address,
				"label":     d.Label,
				"room":      d.Room,
				"paired_at": d.PairedAt,
			}
			if name, model := describeEntry(d); name != "" || model != "" {
				row["name"] = name
				row["model"] = model
			}
			out = append(out, row)
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"path":    reg.Path(),
			"default": reg.Default,
			"devices": out,
		})
	}

	rows := make([]ui.DeviceRow, 0, len(aliases))
	for _, a := range aliases {
		d := reg.Devices[a]
		_, model := describeEntry(d)
		rows = append(rows, ui.DeviceRow{
			Alias:   a,
			Label:   d.Label,
			Room:    d.Room,
			Driver:  string(d.Driver),
			Address: d.Address,
			Model:   model,
			Default: a == reg.Default,
		})
	}
	ui.DeviceTable(cmd.OutOrStdout(), rows)
	fmt.Fprintln(ui.Out(cmd.OutOrStdout()), ui.Subtle.Render("Registry: "+reg.Path()))
	return nil
}

// describeEntry extracts display name and model from the driver-owned
// settings blob, for the drivers whose blob format we know.
func describeEntry(d *registry.Device) (name, model string) {
	if d.Driver != philipsdriver.ID {
		return "", ""
	}
	s, err := philipsdriver.ParseSettings(d.Settings)
	if err != nil {
		return "", ""
	}
	return s.Name, s.Model
}
