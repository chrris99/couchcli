package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	philipsdriver "github.com/chrris99/couchcli/internal/drivers/philips"
	"github.com/chrris99/couchcli/internal/drivers/philips/effects"
	"github.com/chrris99/couchcli/internal/ui"
)

var (
	ambilightDevice    string
	ambilightTimeout   time.Duration
	ambilightDur       time.Duration
	ambilightFPS       int
	ambilightColor     string
	ambilightNoRestore bool
)

var ambilightCmd = &cobra.Command{
	Use:   "ambilight",
	Short: "Control the TV's Ambilight",
	Long: `Control a Philips TV's Ambilight LED ring.

  couch ambilight on|off        Toggle Ambilight power
  couch ambilight style <name>  Set a built-in style (FOLLOW_VIDEO, LOUNGE, …)
  couch ambilight color <hex>   Paint every LED one color (#RRGGBB)
  couch ambilight comet|fire|pulse|rainbow|wave   Run an animated effect`,
}

// ambilightController resolves an alias to a device that supports Ambilight.
func ambilightController(alias string) (philipsdriver.AmbilightController, string, error) {
	dev, resolved, err := openDevice(alias)
	if err != nil {
		return nil, "", err
	}
	ac, ok := dev.(philipsdriver.AmbilightController)
	if !ok {
		return nil, resolved, fmt.Errorf("%s does not support ambilight", resolved)
	}
	return ac, resolved, nil
}

func ambilightCtx(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cmd.Context(), ambilightTimeout)
}

var ambilightOnCmd = &cobra.Command{
	Use: "on", Short: "Turn Ambilight on", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ac, alias, err := ambilightController(ambilightDevice)
		if err != nil {
			return err
		}
		ctx, cancel := ambilightCtx(cmd)
		defer cancel()
		if err := ac.AmbilightOn(ctx); err != nil {
			return err
		}
		ui.Successf(cmd.OutOrStdout(), "%s ambilight on", alias)
		return nil
	},
}

var ambilightOffCmd = &cobra.Command{
	Use: "off", Short: "Turn Ambilight off", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ac, alias, err := ambilightController(ambilightDevice)
		if err != nil {
			return err
		}
		ctx, cancel := ambilightCtx(cmd)
		defer cancel()
		if err := ac.AmbilightOff(ctx); err != nil {
			return err
		}
		ui.Successf(cmd.OutOrStdout(), "%s ambilight off", alias)
		return nil
	},
}

var ambilightStyleCmd = &cobra.Command{
	Use: "style <name>", Short: "Set a built-in Ambilight style", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ac, alias, err := ambilightController(ambilightDevice)
		if err != nil {
			return err
		}
		ctx, cancel := ambilightCtx(cmd)
		defer cancel()
		style := strings.ToUpper(args[0])
		if err := ac.SetAmbilightStyle(ctx, style); err != nil {
			return err
		}
		ui.Successf(cmd.OutOrStdout(), "%s ambilight style → %s", alias, style)
		return nil
	},
}

var ambilightColorCmd = &cobra.Command{
	Use: "color <hex>", Short: "Paint every LED one solid color (#RRGGBB)", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, g, b, err := parseHexColor(args[0])
		if err != nil {
			return err
		}
		ac, alias, err := ambilightController(ambilightDevice)
		if err != nil {
			return err
		}
		ctx, cancel := ambilightCtx(cmd)
		defer cancel()
		if err := ac.SetAmbilightColor(ctx, r, g, b); err != nil {
			return err
		}
		ui.Successf(cmd.OutOrStdout(), "%s ambilight color → %s", alias, args[0])
		return nil
	},
}

// ambilightEffects maps subcommand names to effect constructors. usesColor
// marks the effects that honour the --color flag (the others pick their own).
var ambilightEffects = []struct {
	use       string
	short     string
	usesColor bool
	build     func(c effects.Color) effects.Effect
}{
	{"comet", "A bright head chasing the ring with a fading tail", true,
		func(c effects.Color) effects.Effect { return effects.Comet{Color: c, Period: 3 * time.Second, TailLen: 8} }},
	{"pulse", "A single color breathing in and out", true,
		func(c effects.Color) effects.Effect { return effects.Pulse{Color: c, Period: 4 * time.Second, Floor: 0.1} }},
	{"wave", "A color sweeping around the perimeter", true,
		func(c effects.Color) effects.Effect { return effects.Wave{Color: c, Period: 4 * time.Second} }},
	{"rainbow", "A rainbow rotating around the ring", false,
		func(effects.Color) effects.Effect { return effects.Rainbow{Period: 6 * time.Second, Saturation: 1, Value: 1} }},
	{"fire", "A flickering fire simulation", false,
		func(effects.Color) effects.Effect { return effects.NewFire(55, 120, uint64(time.Now().UnixNano())) }},
}

func runAmbilightEffect(build func(effects.Color) effects.Effect, usesColor bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		var col effects.Color
		if usesColor {
			r, g, b, err := parseHexColor(ambilightColor)
			if err != nil {
				return err
			}
			col = effects.Color{R: r, G: g, B: b}
		}
		ac, alias, err := ambilightController(ambilightDevice)
		if err != nil {
			return err
		}
		// Budget the effect's own duration plus slack for the final restore.
		ctx, cancel := context.WithTimeout(cmd.Context(), ambilightDur+5*time.Second)
		defer cancel()
		ui.Infof(cmd.ErrOrStderr(), "Running %s on %s for %s (Ctrl-C to stop)…", cmd.Name(), alias, ambilightDur)
		err = ac.RunAmbilightEffect(ctx, build(col), ambilightFPS, ambilightDur, !ambilightNoRestore)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		ui.Successf(cmd.OutOrStdout(), "%s ambilight %s finished", alias, cmd.Name())
		return nil
	}
}

func init() {
	ambilightCmd.PersistentFlags().StringVar(&ambilightDevice, "device", "", "alias of a paired device (default: configured default)")
	ambilightCmd.PersistentFlags().DurationVar(&ambilightTimeout, "timeout", 5*time.Second, "request timeout")
	ambilightCmd.AddCommand(ambilightOnCmd, ambilightOffCmd, ambilightStyleCmd, ambilightColorCmd)

	for _, e := range ambilightEffects {
		c := &cobra.Command{Use: e.use, Short: e.short, Args: cobra.NoArgs, RunE: runAmbilightEffect(e.build, e.usesColor)}
		c.Flags().DurationVar(&ambilightDur, "duration", 10*time.Second, "how long to run the effect")
		c.Flags().IntVar(&ambilightFPS, "fps", 20, "frames per second (1-30)")
		c.Flags().BoolVar(&ambilightNoRestore, "no-restore", false, "leave Ambilight in manual mode on exit")
		if e.usesColor {
			c.Flags().StringVar(&ambilightColor, "color", "#00AAFF", "effect color (#RRGGBB)")
		}
		ambilightCmd.AddCommand(c)
	}
	rootCmd.AddCommand(ambilightCmd)
}

// parseHexColor parses "#RRGGBB" or "RRGGBB" into 8-bit channels.
func parseHexColor(s string) (r, g, b uint8, err error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid color %q — use #RRGGBB", s)
	}
	v, perr := strconv.ParseUint(h, 16, 32)
	if perr != nil {
		return 0, 0, 0, fmt.Errorf("invalid color %q: %w", s, perr)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), nil
}
