package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/philips"
)

var (
	mediaDevice  string
	mediaTimeout time.Duration
)

var mediaCmd = &cobra.Command{
	Use:   "media",
	Short: "Control media playback",
	Long: `Send transport keys to whatever app is currently in front.

Not every app honours every key — when in doubt, use ` + "`couch media toggle`" + `
which sends the universal PlayPause key.`,
}

type mediaOp struct {
	use   string
	short string
	key   string
}

var mediaOps = []mediaOp{
	{"play", "Send Play", philips.KeyPlay},
	{"pause", "Send Pause", philips.KeyPause},
	{"toggle", "Send PlayPause", philips.KeyPlayPause},
	{"stop", "Send Stop", philips.KeyStop},
	{"next", "Skip to next", philips.KeyNext},
	{"previous", "Skip to previous", philips.KeyPrevious},
	{"ff", "Fast-forward", philips.KeyFastForward},
	{"rew", "Rewind", philips.KeyRewind},
}

func init() {
	mediaCmd.PersistentFlags().StringVar(&mediaDevice, "device", "", "alias of a paired device (default: configured default)")
	mediaCmd.PersistentFlags().DurationVar(&mediaTimeout, "timeout", 5*time.Second, "request timeout")
	for _, op := range mediaOps {
		op := op
		mediaCmd.AddCommand(&cobra.Command{
			Use:   op.use,
			Short: op.short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runMediaKey(cmd, op.key)
			},
		})
	}
	rootCmd.AddCommand(mediaCmd)
}

func runMediaKey(cmd *cobra.Command, key string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), mediaTimeout)
	defer cancel()
	c, _, err := openClient(mediaDevice)
	if err != nil {
		return err
	}
	return c.Keys.Send(ctx, key)
}
