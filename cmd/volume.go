package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
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
  couch volume up [N]   Press VolumeUp N times (default 1)
  couch volume down [N] Press VolumeDown N times (default 1)
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

func runVolumeGet(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), volumeTimeout)
	defer cancel()
	c, alias, err := openClient(volumeDevice)
	if err != nil {
		return err
	}
	v, err := c.Volume.Get(ctx)
	if err != nil {
		return fmt.Errorf("get volume: %w", err)
	}
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias":   alias,
			"current": v.Current,
			"min":     v.Min,
			"max":     v.Max,
			"muted":   v.Muted,
		})
	}
	muted := ""
	if v.Muted {
		muted = " (muted)"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %d / %d%s\n", alias, v.Current, v.Max, muted)
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
		ctx, cancel := context.WithTimeout(cmd.Context(), volumeTimeout+time.Duration(count)*200*time.Millisecond)
		defer cancel()
		c, _, err := openClient(volumeDevice)
		if err != nil {
			return err
		}
		if up {
			return c.Volume.Up(ctx, count)
		}
		return c.Volume.Down(ctx, count)
	}
}

func runVolumeMute(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), volumeTimeout)
	defer cancel()
	c, _, err := openClient(volumeDevice)
	if err != nil {
		return err
	}
	return c.Volume.Mute(ctx)
}
