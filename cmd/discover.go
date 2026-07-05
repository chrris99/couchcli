package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/discovery"
	"github.com/chrris99/couchcli/internal/ui"
)

var discoverTimeout time.Duration

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover supported devices on the local network",
	Long: `Discover scans the local network once with generic mDNS and SSDP scanners.
Each physical device appears as one row per driver that can control it.`,
	RunE: runDiscover,
}

func init() {
	discoverCmd.Flags().DurationVar(&discoverTimeout, "timeout", 4*time.Second, "scanner listen window")
	rootCmd.AddCommand(discoverCmd)
}

func runDiscover(cmd *cobra.Command, _ []string) error {
	var candidates []device.Candidate
	var sweepErr error
	err := ui.Spin(cmd.Context(), "Scanning the network…", func(ctx context.Context) error {
		candidates, sweepErr = discovery.Sweep(ctx, discovery.Options{Timeout: discoverTimeout}, matchers()...)
		return nil
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		if candidates == nil {
			candidates = []device.Candidate{}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(candidates); err != nil {
			return err
		}
	} else {
		ui.CandidateTable(cmd.OutOrStdout(), candidates)
	}
	if sweepErr != nil {
		ui.Warnf(cmd.ErrOrStderr(), "some discovery sources failed: %v", sweepErr)
	}
	return nil
}
