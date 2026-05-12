package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/discovery"
	"github.com/chrris99/couchcli/internal/render"
)

var (
	discoverTimeout time.Duration
	discoverProbe   bool
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover smart TVs on the local network",
	Long: `Discover scans the local network for smart TVs using mDNS and SSDP.

Four discovery sources run in parallel:
  - mDNS _philipstv_s_rpc._tcp  (Philips Android TVs, HTTPS port 1926)
  - mDNS _philipstv_rpc._tcp    (Legacy non-Android Philips TVs, HTTP port 1925)
  - mDNS _androidtvremote2._tcp (Android TV Remote v2 endpoint)
  - SSDP M-SEARCH               (UPnP MediaRenderer + rootdevice, filtered to
                                 Philips / TP Vision manufacturer)

After discovery, each unique address is probed against every registered
device driver to enrich MODEL and API columns (disable with --probe=false).

A single TV that exposes multiple protocols will appear as multiple rows —
one per protocol — so you can see exactly what's available where.`,
	RunE: runDiscover,
}

func init() {
	discoverCmd.Flags().DurationVar(&discoverTimeout, "timeout", 5*time.Second, "discovery timeout per source")
	discoverCmd.Flags().BoolVar(&discoverProbe, "probe", true, "probe each unique address for driver metadata")
	rootCmd.AddCommand(discoverCmd)
}

func runDiscover(cmd *cobra.Command, _ []string) error {
	// Give the per-source goroutines a small grace period beyond their own
	// timeout before the outer context fires. Allow extra time for probing.
	overall := discoverTimeout + 2*time.Second
	if discoverProbe {
		overall += 4 * time.Second
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), overall)
	defer cancel()

	candidates, err := discovery.Discover(ctx, discovery.Options{
		Timeout: discoverTimeout,
		Probe:   discoverProbe,
	})
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	return render.Devices(cmd.OutOrStdout(), candidates, jsonOutput)
}
