package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/config"
)

var devicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List paired TVs",
	Long:  "List TVs that have been paired with couchcli, stored in the local config file.",
	RunE:  runDevices,
}

var devicesLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List paired TVs",
	RunE:  runDevices,
}

func init() {
	devicesCmd.AddCommand(devicesLsCmd)
	rootCmd.AddCommand(devicesCmd)
}

func runDevices(cmd *cobra.Command, _ []string) error {
	schema, path, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	aliases := make([]string, 0, len(schema.Devices))
	for a := range schema.Devices {
		aliases = append(aliases, a)
	}
	sort.Strings(aliases)

	if jsonOutput {
		out := make([]map[string]any, 0, len(aliases))
		for _, a := range aliases {
			d := schema.Devices[a]
			out = append(out, map[string]any{
				"alias":     a,
				"default":   a == schema.Default,
				"address":   d.Address,
				"port":      d.Port,
				"secure":    d.Secure,
				"label":     d.Label,
				"location":  d.Location,
				"name":      d.Name,
				"model":     d.Model,
				"api":       d.APIVersion,
				"paired_at": d.PairedAt,
			})
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"path":    path,
			"default": schema.Default,
			"devices": out,
		})
	}

	if len(aliases) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No paired devices.\n  Run `couch pair` to add one.\n  Config: %s\n", path)
		return nil
	}

	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ALIAS\tLABEL\tLOCATION\tADDRESS\tNAME\tMODEL\tAPI\tDEFAULT")
	for _, a := range aliases {
		d := schema.Devices[a]
		def := ""
		if a == schema.Default {
			def = "*"
		}
		api := ""
		if d.APIVersion > 0 {
			api = fmt.Sprintf("v%d", d.APIVersion)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			a,
			truncateStr(d.Label, 30),
			truncateStr(d.Location, 20),
			d.Address,
			truncateStr(d.Name, 30),
			truncateStr(d.Model, 30),
			api,
			def,
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nConfig: %s\n", path)
	return nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
