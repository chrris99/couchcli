package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/device"
)

var (
	mediaDevice  string
	mediaTimeout time.Duration
)

var mediaCmd = &cobra.Command{
	Use:   "media",
	Short: "Control media playback",
	Long: `Send transport commands to whatever app is currently in front.

Not every app honours every command — when in doubt, use ` + "`couch media toggle`" + `
which sends the universal play/pause action.`,
}

var mediaOps = []struct {
	use    string
	short  string
	action device.MediaAction
}{
	{"play", "Send Play", device.MediaPlay},
	{"pause", "Send Pause", device.MediaPause},
	{"toggle", "Send PlayPause", device.MediaPlayPause},
	{"stop", "Send Stop", device.MediaStop},
	{"next", "Skip to next", device.MediaNext},
	{"previous", "Skip to previous", device.MediaPrevious},
	{"ff", "Fast-forward", device.MediaFastForward},
	{"rew", "Rewind", device.MediaRewind},
}

func init() {
	mediaCmd.PersistentFlags().StringVar(&mediaDevice, "device", "", "alias of a paired device (default: configured default)")
	mediaCmd.PersistentFlags().DurationVar(&mediaTimeout, "timeout", 5*time.Second, "request timeout")
	for _, op := range mediaOps {
		mediaCmd.AddCommand(&cobra.Command{
			Use:   op.use,
			Short: op.short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runMediaAction(cmd, op.action)
			},
		})
	}
	rootCmd.AddCommand(mediaCmd)
}

// mediaController resolves an alias to a device that supports media control.
func mediaController(alias string) (device.MediaController, string, error) {
	dev, resolved, err := openDevice(alias)
	if err != nil {
		return nil, "", err
	}
	mc, ok := dev.(device.MediaController)
	if !ok {
		return nil, resolved, fmt.Errorf("%s does not support media control", resolved)
	}
	return mc, resolved, nil
}

func runMediaAction(cmd *cobra.Command, action device.MediaAction) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), mediaTimeout)
	defer cancel()
	mc, _, err := mediaController(mediaDevice)
	if err != nil {
		return err
	}
	return mc.Media(ctx, action)
}
