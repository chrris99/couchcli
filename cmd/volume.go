package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/ui"
)

var (
	volumeDevice  string
	volumeTimeout time.Duration
)

var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Read or change media volume",
	Long: `Control the volume on a paired device.

  couch volume          Show current volume
  couch volume up [N]   Raise volume by N steps (default 1)
  couch volume down [N] Lower volume by N steps (default 1)
  couch volume mute     Toggle mute`,
	RunE: runVolumeGet,
}

var volumeUpCmd = &cobra.Command{
	Use:   "up [count]",
	Short: "Increase volume",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runVolumeStep(true),
}

var volumeDownCmd = &cobra.Command{
	Use:   "down [count]",
	Short: "Decrease volume",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runVolumeStep(false),
}

var volumeMuteCmd = &cobra.Command{
	Use:   "mute",
	Short: "Toggle mute",
	Args:  cobra.NoArgs,
	RunE:  runVolumeMute,
}

func init() {
	volumeCmd.PersistentFlags().StringVar(&volumeDevice, "device", "", "alias of a paired device (default: configured default)")
	volumeCmd.PersistentFlags().DurationVar(&volumeTimeout, "timeout", 5*time.Second, "request timeout")
	volumeCmd.AddCommand(volumeUpCmd, volumeDownCmd, volumeMuteCmd)
	rootCmd.AddCommand(volumeCmd)
}

// volumeController resolves an alias to a device that supports volume control.
func volumeController(alias string) (device.VolumeController, string, error) {
	dev, resolved, err := openDevice(alias)
	if err != nil {
		return nil, "", err
	}
	vc, ok := dev.(device.VolumeController)
	if !ok {
		return nil, resolved, fmt.Errorf("%s does not support volume control", resolved)
	}
	return vc, resolved, nil
}

func runVolumeGet(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), volumeTimeout)
	defer cancel()
	vc, alias, err := volumeController(volumeDevice)
	if err != nil {
		return err
	}
	v, err := vc.Volume(ctx)
	if err != nil {
		return fmt.Errorf("get volume: %w", err)
	}
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias":   alias,
			"current": v.Level,
			"min":     v.Min,
			"max":     v.Max,
			"muted":   v.Muted,
			"percent": v.Percent(),
		})
	}
	fmt.Fprintln(ui.Out(cmd.OutOrStdout()), ui.VolumeLine(alias, v))
	return nil
}

func runVolumeStep(up bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		count := 1
		if len(args) == 1 {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return fmt.Errorf("invalid count %q", args[0])
			}
			count = n
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), volumeTimeout)
		defer cancel()
		vc, _, err := volumeController(volumeDevice)
		if err != nil {
			return err
		}
		v, err := vc.Volume(ctx)
		if err != nil {
			return fmt.Errorf("get volume: %w", err)
		}
		target := v.Level + count
		if !up {
			target = v.Level - count
		}
		return vc.SetVolume(ctx, target)
	}
}

func runVolumeMute(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), volumeTimeout)
	defer cancel()
	vc, _, err := volumeController(volumeDevice)
	if err != nil {
		return err
	}
	v, err := vc.Volume(ctx)
	if err != nil {
		return fmt.Errorf("get volume: %w", err)
	}
	return vc.SetMuted(ctx, !v.Muted)
}
