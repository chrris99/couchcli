package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/config"
	"github.com/chrris99/couchcli/internal/philips"
	"github.com/chrris99/couchcli/internal/wol"
)

var (
	deviceOnWait    time.Duration
	deviceOnNoWait  bool
	deviceOnAddr    string
	deviceOnCount   int
	deviceOffWait   time.Duration
	deviceProbeWait time.Duration
)

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Power and status of a paired TV",
	Long: `Power and connection commands for a paired TV.

  couch device on  [alias]    Wake the TV via Wake-on-LAN
  couch device off [alias]    Send the TV to standby
  couch device status [alias] Show awake/asleep status`,
}

var deviceOnCmd = &cobra.Command{
	Use:   "on [alias]",
	Short: "Wake the TV (Wake-on-LAN)",
	Long: `Send a Wake-on-LAN magic packet to wake a sleeping TV.

The TV must have "Wake on WLAN" (or wired WoL) enabled in its network
settings. After paired devices are added, run this from the same LAN.

After sending the packet, waits up to --wait for the TV to come online,
unless --no-wait is set. Polls every 500ms.

Out of scope: deep-sleep (multi-hour standby) wake — some Philips Android
TVs enter a state that only Google Cast / Android TV Remote v2 can wake.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDeviceOn,
}

var deviceOffCmd = &cobra.Command{
	Use:   "off [alias]",
	Short: "Put the TV into standby",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDeviceOff,
}

var deviceStatusCmd = &cobra.Command{
	Use:   "status [alias]",
	Short: "Show whether the TV is awake",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDeviceStatus,
}

func init() {
	deviceOnCmd.Flags().DurationVar(&deviceOnWait, "wait", 20*time.Second, "how long to wait for the TV to come online")
	deviceOnCmd.Flags().BoolVar(&deviceOnNoWait, "no-wait", false, "return immediately after sending the magic packet")
	deviceOnCmd.Flags().StringVar(&deviceOnAddr, "broadcast", wol.DefaultAddr, "UDP broadcast target (host:port)")
	deviceOnCmd.Flags().IntVar(&deviceOnCount, "count", 3, "number of magic packets to send")

	deviceOffCmd.Flags().DurationVar(&deviceOffWait, "timeout", 5*time.Second, "request timeout")
	deviceStatusCmd.Flags().DurationVar(&deviceProbeWait, "timeout", 2*time.Second, "probe timeout")

	deviceCmd.AddCommand(deviceOnCmd, deviceOffCmd, deviceStatusCmd)
	rootCmd.AddCommand(deviceCmd)
}

// resolveDeviceEntry returns the saved device entry for alias. If alias is ""
// the default entry is used; if there's no default but exactly one device is
// stored, that one is used.
func resolveDeviceEntry(alias string) (*config.Device, string, error) {
	schema, _, err := config.Load()
	if err != nil {
		return nil, "", err
	}
	if len(schema.Devices) == 0 {
		return nil, "", fmt.Errorf("no paired devices — run `couch pair` first")
	}
	if alias == "" {
		alias = schema.Default
	}
	if alias == "" {
		if len(schema.Devices) == 1 {
			for k := range schema.Devices {
				alias = k
			}
		} else {
			return nil, "", fmt.Errorf("no default device — pass --device <alias> (paired: %s)", deviceAliases(schema))
		}
	}
	d, ok := schema.Get(alias)
	if !ok {
		return nil, "", fmt.Errorf("no device %q (paired: %s)", alias, deviceAliases(schema))
	}
	return d, alias, nil
}

// openClient resolves an alias and builds a JointSpace client preloaded with
// the stored digest credentials.
func openClient(alias string) (*philips.Client, string, error) {
	entry, resolvedAlias, err := resolveDeviceEntry(alias)
	if err != nil {
		return nil, "", err
	}
	var c *philips.Client
	if entry.Secure {
		c = philips.NewSecure(entry.Address)
	} else {
		c = philips.NewPlain(entry.Address)
	}
	if entry.DeviceID != "" && entry.AuthKey != "" {
		c.WithDigest(entry.DeviceID, entry.AuthKey)
	}
	return c, resolvedAlias, nil
}

func deviceAliases(s *config.Schema) string {
	out := ""
	first := true
	for a := range s.Devices {
		if !first {
			out += ", "
		}
		out += a
		first = false
	}
	return out
}

// firstArgOrEmpty returns args[0] if present, else "". Used by subcommands
// where the alias is optional.
func firstArgOrEmpty(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func runDeviceOn(cmd *cobra.Command, args []string) error {
	entry, alias, err := resolveDeviceEntry(firstArgOrEmpty(args))
	if err != nil {
		return err
	}
	if entry.MAC == "" {
		return fmt.Errorf("no MAC address stored for %q — re-pair with `couch pair --mac <MAC> --as %s` to populate it", alias, alias)
	}
	mac, err := net.ParseMAC(entry.MAC)
	if err != nil {
		return fmt.Errorf("stored MAC %q is invalid: %w", entry.MAC, err)
	}
	if deviceOnCount < 1 {
		return fmt.Errorf("--count must be >= 1")
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Sending Wake-on-LAN to %s (%s) via %s\n", alias, entry.MAC, deviceOnAddr)
	sendCtx, sendCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer sendCancel()
	if err := wol.Send(sendCtx, mac, wol.Options{
		Addr:  deviceOnAddr,
		Count: deviceOnCount,
	}); err != nil {
		return fmt.Errorf("send magic packet: %w", err)
	}

	if deviceOnNoWait {
		fmt.Fprintln(cmd.OutOrStdout(), "Magic packet sent.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Waiting up to %s for the TV to come online…\n", deviceOnWait)
	client, _, err := openClient(alias)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(deviceOnWait)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if client.System.IsAwake(cmd.Context(), 800*time.Millisecond) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s is awake.\n", alias)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not respond within %s — check that Wake-on-WLAN is enabled in the TV's network settings", alias, deviceOnWait)
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-tick.C:
		}
	}
}

func runDeviceOff(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), deviceOffWait)
	defer cancel()
	c, alias, err := openClient(firstArgOrEmpty(args))
	if err != nil {
		return err
	}
	if err := c.Keys.Standby(ctx); err != nil {
		return fmt.Errorf("send standby: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s sent to standby.\n", alias)
	return nil
}

func runDeviceStatus(cmd *cobra.Command, args []string) error {
	entry, alias, err := resolveDeviceEntry(firstArgOrEmpty(args))
	if err != nil {
		return err
	}
	client, _, err := openClient(alias)
	if err != nil {
		return err
	}
	awake := client.System.IsAwake(cmd.Context(), deviceProbeWait)

	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias":   alias,
			"address": entry.Address,
			"mac":     entry.MAC,
			"awake":   awake,
		})
	}
	state := "asleep"
	if awake {
		state = "awake"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s (%s): %s\n", alias, entry.Address, state)
	return nil
}

