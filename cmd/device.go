package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
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
	deviceOnForce   bool
	deviceOffWait   time.Duration
	deviceProbeWait time.Duration
)

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Power and status of a paired TV",
	Long: `Power and connection commands for a paired TV.

  couch device on  [alias]    Power the TV on (powerstate=On, or WoL if unreachable)
  couch device off [alias]    Put the TV into standby (powerstate=Standby)
  couch device status [alias] Show on / standby / unreachable`,
}

var deviceOnCmd = &cobra.Command{
	Use:   "on [alias]",
	Short: "Power the TV on",
	Long: `Power the TV on using the canonical /6/powerstate endpoint.

Behaviour:
  1. If the JointSpace API is reachable, GET /6/powerstate.
     - "On"      → no-op, print "already on".
     - "Standby" → POST powerstate=On and poll until reported "On".
     - 404       → older firmware, send the Standby key as a toggle.
  2. If the API is unreachable, send a Wake-on-LAN magic packet to the
     stored MAC, then wait for the API to come back and ensure
     powerstate=On.

The MAC is only required for step 2. Most Android Philips TVs in soft
standby answer step 1 directly — WoL is rarely needed.

Use --force-wol to skip step 1 and always send a magic packet first
(useful for debugging WoL configuration).`,
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
	Short: "Show on / standby / unreachable status",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDeviceStatus,
}

func init() {
	deviceOnCmd.Flags().DurationVar(&deviceOnWait, "wait", 20*time.Second, "how long to wait for the TV to report \"On\"")
	deviceOnCmd.Flags().BoolVar(&deviceOnNoWait, "no-wait", false, "return immediately after issuing power-on / WoL")
	deviceOnCmd.Flags().StringVar(&deviceOnAddr, "broadcast", wol.DefaultAddr, "UDP broadcast target (host:port) for Wake-on-LAN")
	deviceOnCmd.Flags().IntVar(&deviceOnCount, "count", 3, "number of magic packets to send when WoL is used")
	deviceOnCmd.Flags().BoolVar(&deviceOnForce, "force-wol", false, "skip the powerstate path and always send Wake-on-LAN first")

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
	if deviceOnCount < 1 {
		return fmt.Errorf("--count must be >= 1")
	}
	entry, alias, err := resolveDeviceEntry(firstArgOrEmpty(args))
	if err != nil {
		return err
	}
	client, _, err := openClient(alias)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	deadline := time.Now().Add(deviceOnWait)

	// Phase A: try the direct /6/powerstate path unless --force-wol is set.
	if !deviceOnForce && client.System.IsReachable(cmd.Context(), 1500*time.Millisecond) {
		state, err := client.Power.Get(cmd.Context())
		switch {
		case errors.Is(err, philips.ErrPowerstateUnsupported):
			fmt.Fprintf(out, "%s: /6/powerstate unsupported; sending Standby key as a toggle\n", alias)
			if err := client.Keys.Standby(cmd.Context()); err != nil {
				return fmt.Errorf("send standby key: %w", err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("read powerstate: %w", err)
		case state == philips.PowerOn:
			fmt.Fprintf(out, "%s is already on.\n", alias)
			return nil
		case state == philips.PowerStandby:
			fmt.Fprintf(out, "%s is in standby — sending powerstate=On\n", alias)
			if err := client.Power.On(cmd.Context()); err != nil {
				return fmt.Errorf("set powerstate=On: %w", err)
			}
			if deviceOnNoWait {
				return nil
			}
			return waitOn(cmd.Context(), client, out, alias, time.Until(deadline))
		default:
			fmt.Fprintf(out, "%s reported unexpected powerstate %q — continuing\n", alias, state)
		}
	}

	// Phase B: API unreachable (or --force-wol). Need a MAC for WoL.
	if entry.MAC == "" {
		return fmt.Errorf("%s is unreachable and no MAC is stored — re-pair with `couch pair --mac <MAC> --as %s --force` to enable Wake-on-LAN", alias, alias)
	}
	mac, err := net.ParseMAC(entry.MAC)
	if err != nil {
		return fmt.Errorf("stored MAC %q is invalid: %w", entry.MAC, err)
	}

	fmt.Fprintf(out, "Sending Wake-on-LAN to %s (%s) via %s\n", alias, entry.MAC, deviceOnAddr)
	sendCtx, sendCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	err = wol.Send(sendCtx, mac, wol.Options{Addr: deviceOnAddr, Count: deviceOnCount})
	sendCancel()
	if err != nil {
		return fmt.Errorf("send magic packet: %w", err)
	}
	if deviceOnNoWait {
		fmt.Fprintln(out, "Magic packet sent.")
		return nil
	}

	// Phase C: wait for the API to come back, then ensure powerstate=On.
	fmt.Fprintf(out, "Waiting up to %s for the TV to come online…\n", time.Until(deadline).Round(time.Second))
	if err := waitReachable(cmd.Context(), client, alias, time.Until(deadline)); err != nil {
		return err
	}
	state, err := client.Power.Get(cmd.Context())
	switch {
	case errors.Is(err, philips.ErrPowerstateUnsupported):
		// Older firmware — best-effort: TV came back, we're done.
		fmt.Fprintf(out, "%s is reachable (powerstate unsupported, assuming on).\n", alias)
		return nil
	case err != nil:
		return fmt.Errorf("read powerstate after WoL: %w", err)
	case state == philips.PowerOn:
		fmt.Fprintf(out, "%s is on.\n", alias)
		return nil
	case state == philips.PowerStandby:
		if err := client.Power.On(cmd.Context()); err != nil {
			return fmt.Errorf("set powerstate=On after WoL: %w", err)
		}
		return waitOn(cmd.Context(), client, out, alias, time.Until(deadline))
	default:
		fmt.Fprintf(out, "%s reported powerstate %q after WoL.\n", alias, state)
		return nil
	}
}

// waitOn polls Power.Get every 500ms until the TV reports "On" or budget
// runs out. budget <= 0 returns immediately with an error.
func waitOn(ctx context.Context, client *philips.Client, out io.Writer, alias string, budget time.Duration) error {
	if budget <= 0 {
		return fmt.Errorf("%s did not reach powerstate=On in time", alias)
	}
	deadline := time.Now().Add(budget)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := client.Power.Get(ctx)
		if err == nil && state == philips.PowerOn {
			fmt.Fprintf(out, "%s is on.\n", alias)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not reach powerstate=On within %s", alias, budget)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// waitReachable polls System.IsReachable every 500ms until budget runs out.
func waitReachable(ctx context.Context, client *philips.Client, alias string, budget time.Duration) error {
	if budget <= 0 {
		return fmt.Errorf("%s did not respond in time — check that Wake-on-LAN/WoWLAN is enabled in the TV's network settings", alias)
	}
	deadline := time.Now().Add(budget)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if client.System.IsReachable(ctx, 800*time.Millisecond) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not respond within %s — check that Wake-on-LAN/WoWLAN is enabled in the TV's network settings", alias, budget)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
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
	err = c.Power.Standby(ctx)
	if errors.Is(err, philips.ErrPowerstateUnsupported) {
		// Older firmware — fall back to the toggle key.
		err = c.Keys.Standby(ctx)
	}
	if err != nil {
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

	reachable := client.System.IsReachable(cmd.Context(), deviceProbeWait)
	state := "unreachable"
	if reachable {
		probeCtx, cancel := context.WithTimeout(cmd.Context(), deviceProbeWait)
		p, perr := client.Power.Get(probeCtx)
		cancel()
		switch {
		case errors.Is(perr, philips.ErrPowerstateUnsupported):
			state = "reachable" // older firmware; we cannot say on vs standby
		case perr != nil:
			state = fmt.Sprintf("error: %v", perr)
		default:
			state = strings.ToLower(p) // "on" / "standby"
		}
	}

	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias":      alias,
			"address":    entry.Address,
			"mac":        entry.MAC,
			"reachable":  reachable,
			"powerstate": state,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s (%s): %s\n", alias, entry.Address, state)
	return nil
}
