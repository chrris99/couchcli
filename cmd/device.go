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

	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/registry"
	"github.com/chrris99/couchcli/internal/ui"
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
  1. If the control API is reachable, read the power state.
     - On      → no-op, print "already on".
     - Standby → power on and poll until reported "On".
     - unsupported → older firmware, send the Standby key as a toggle.
  2. If the API is unreachable, send a Wake-on-LAN magic packet to the
     stored MAC, then wait for the API to come back and ensure it is on.

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
func resolveDeviceEntry(alias string) (*registry.Device, string, error) {
	reg, err := registry.Load()
	if err != nil {
		return nil, "", err
	}
	if len(reg.Devices) == 0 {
		return nil, "", fmt.Errorf("no paired devices — run `couch pair` first")
	}
	if alias == "" {
		alias = reg.Default
	}
	if alias == "" {
		aliases := reg.Aliases()
		if len(aliases) != 1 {
			return nil, "", fmt.Errorf("no default device — pass --device <alias> (paired: %s)", strings.Join(aliases, ", "))
		}
		alias = aliases[0]
	}
	d, ok := reg.Get(alias)
	if !ok {
		return nil, "", fmt.Errorf("no device %q (paired: %s)", alias, strings.Join(reg.Aliases(), ", "))
	}
	return d, alias, nil
}

// powerController resolves an alias to a device that supports power control.
func powerController(alias string) (device.PowerController, string, error) {
	dev, resolved, err := openDevice(alias)
	if err != nil {
		return nil, "", err
	}
	pc, ok := dev.(device.PowerController)
	if !ok {
		return nil, resolved, fmt.Errorf("%s does not support power control", resolved)
	}
	return pc, resolved, nil
}

// powerReachable reports whether the device's control API answers within
// timeout. A powerstate the driver cannot read (ErrUnsupported) still counts as
// reachable — the API responded.
func powerReachable(ctx context.Context, pc device.PowerController, timeout time.Duration) bool {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := pc.Power(probeCtx)
	return err == nil || errors.Is(err, device.ErrUnsupported)
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
	pc, _, err := powerController(alias)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	deadline := time.Now().Add(deviceOnWait)

	// Phase A: try the direct powerstate path unless --force-wol is set.
	if !deviceOnForce && powerReachable(cmd.Context(), pc, 1500*time.Millisecond) {
		state, err := pc.Power(cmd.Context())
		switch {
		case errors.Is(err, device.ErrUnsupported):
			ui.Infof(out, "%s: powerstate unsupported; toggling power via the Standby key", alias)
			if err := pc.PowerOn(cmd.Context()); err != nil {
				return fmt.Errorf("toggle power: %w", err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("read powerstate: %w", err)
		case state == device.PowerOn:
			ui.Successf(out, "%s is already on.", alias)
			return nil
		case state == device.PowerStandby:
			ui.Infof(out, "%s is in standby — powering on", alias)
			if err := pc.PowerOn(cmd.Context()); err != nil {
				return fmt.Errorf("power on: %w", err)
			}
			if deviceOnNoWait {
				return nil
			}
			return waitOn(cmd.Context(), pc, out, alias, time.Until(deadline))
		default:
			ui.Warnf(out, "%s reported unexpected power state %q — continuing", alias, state)
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

	ui.Infof(out, "Sending Wake-on-LAN to %s (%s) via %s", alias, entry.MAC, deviceOnAddr)
	sendCtx, sendCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	err = wol.Send(sendCtx, mac, wol.Options{Addr: deviceOnAddr, Count: deviceOnCount})
	sendCancel()
	if err != nil {
		return fmt.Errorf("send magic packet: %w", err)
	}
	if deviceOnNoWait {
		ui.Infof(out, "Magic packet sent.")
		return nil
	}

	// Phase C: wait for the API to come back, then ensure it is on.
	ui.Infof(out, "Waiting up to %s for the TV to come online…", time.Until(deadline).Round(time.Second))
	if err := waitReachable(cmd.Context(), pc, alias, time.Until(deadline)); err != nil {
		return err
	}
	state, err := pc.Power(cmd.Context())
	switch {
	case errors.Is(err, device.ErrUnsupported):
		// Older firmware — best-effort: TV came back, we're done.
		ui.Successf(out, "%s is reachable (powerstate unsupported, assuming on).", alias)
		return nil
	case err != nil:
		return fmt.Errorf("read powerstate after WoL: %w", err)
	case state == device.PowerOn:
		ui.Successf(out, "%s is on.", alias)
		return nil
	case state == device.PowerStandby:
		if err := pc.PowerOn(cmd.Context()); err != nil {
			return fmt.Errorf("power on after WoL: %w", err)
		}
		return waitOn(cmd.Context(), pc, out, alias, time.Until(deadline))
	default:
		ui.Warnf(out, "%s reported power state %q after WoL.", alias, state)
		return nil
	}
}

// waitOn polls Power every 500ms until the TV reports PowerOn or the budget
// runs out. budget <= 0 returns immediately with an error.
func waitOn(ctx context.Context, pc device.PowerController, out io.Writer, alias string, budget time.Duration) error {
	if budget <= 0 {
		return fmt.Errorf("%s did not power on in time", alias)
	}
	deadline := time.Now().Add(budget)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := pc.Power(ctx)
		if err == nil && state == device.PowerOn {
			ui.Successf(out, "%s is on.", alias)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not power on within %s", alias, budget)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// waitReachable polls the control API every 500ms until it answers or the
// budget runs out.
func waitReachable(ctx context.Context, pc device.PowerController, alias string, budget time.Duration) error {
	if budget <= 0 {
		return fmt.Errorf("%s did not respond in time — check that Wake-on-LAN/WoWLAN is enabled in the TV's network settings", alias)
	}
	deadline := time.Now().Add(budget)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if powerReachable(ctx, pc, 800*time.Millisecond) {
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
	pc, alias, err := powerController(firstArgOrEmpty(args))
	if err != nil {
		return err
	}
	if err := pc.Standby(ctx); err != nil {
		return fmt.Errorf("send standby: %w", err)
	}
	ui.Successf(cmd.OutOrStdout(), "%s sent to standby.", alias)
	return nil
}

func runDeviceStatus(cmd *cobra.Command, args []string) error {
	entry, alias, err := resolveDeviceEntry(firstArgOrEmpty(args))
	if err != nil {
		return err
	}
	pc, _, err := powerController(alias)
	if err != nil {
		return err
	}

	probeCtx, cancel := context.WithTimeout(cmd.Context(), deviceProbeWait)
	p, perr := pc.Power(probeCtx)
	cancel()
	var state string
	switch {
	case errors.Is(perr, device.ErrUnsupported):
		state = "reachable" // answered, but cannot say on vs standby
	case perr != nil:
		state = "unreachable"
	case p == device.PowerOn:
		state = "on"
	case p == device.PowerStandby:
		state = "standby"
	default:
		state = "reachable"
	}

	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias":      alias,
			"address":    entry.Address,
			"mac":        entry.MAC,
			"reachable":  state != "unreachable",
			"powerstate": state,
		})
	}
	fmt.Fprintf(ui.Out(cmd.OutOrStdout()), "%s %s %s\n", ui.StatusBadge(state), alias, ui.Subtle.Render("("+entry.Address+")"))
	return nil
}
