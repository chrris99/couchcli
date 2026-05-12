package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/philips"
	"github.com/chrris99/couchcli/internal/philips/ambilighteffects"
)

// Shared across all effect subcommands. Bound on the parent's
// PersistentFlags so any leaf can read them.
var (
	effectDuration time.Duration
	effectFPS      int
	effectRestore  bool
)

// Per-effect flag vars (kept package-level to make wiring readable).
var (
	wavePeriod time.Duration

	pulsePeriod time.Duration
	pulseFloor  float64

	rainbowPeriod time.Duration
	rainbowSat    float64
	rainbowVal    float64

	cometPeriod time.Duration
	cometTail   int

	fireCooling  int
	fireSparking int
	fireSeed     uint64
)

var ambilightEffectCmd = &cobra.Command{
	Use:   "effect",
	Short: "Run animated Ambilight effects (wave, pulse, rainbow, comet, fire)",
	Long: `Animated Ambilight effects.

Each effect forces mode=manual while running and (unless --no-restore is set)
returns mode=internal on exit. Use --duration to control how long an effect
runs and --fps to set the frame rate (TV throttles above ~15).`,
}

var effectWaveCmd = &cobra.Command{
	Use:   "wave [color]",
	Short: "Sinusoidal brightness sweep around the perimeter",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		color, err := pickEffectColor(args, philips.AmbilightColor{G: 255})
		if err != nil {
			return err
		}
		return runEffect(cmd, ambilighteffects.Wave{Color: color, Period: wavePeriod}, describeColor("wave", color))
	},
}

var effectPulseCmd = &cobra.Command{
	Use:   "pulse [color]",
	Short: "Whole-strip breathing pulse in a single color",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		color, err := pickEffectColor(args, philips.AmbilightColor{R: 255, G: 200, B: 120})
		if err != nil {
			return err
		}
		return runEffect(cmd,
			ambilighteffects.Pulse{Color: color, Period: pulsePeriod, Floor: pulseFloor},
			describeColor("pulse", color),
		)
	},
}

var effectRainbowCmd = &cobra.Command{
	Use:   "rainbow",
	Short: "Rainbow hue cycle around the perimeter",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runEffect(cmd,
			ambilighteffects.Rainbow{Period: rainbowPeriod, Saturation: rainbowSat, Value: rainbowVal},
			"rainbow",
		)
	},
}

var effectCometCmd = &cobra.Command{
	Use:   "comet [color]",
	Short: "Single bright head with a fading tail looping the perimeter",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		color, err := pickEffectColor(args, philips.AmbilightColor{R: 0, G: 200, B: 255})
		if err != nil {
			return err
		}
		return runEffect(cmd,
			ambilighteffects.Comet{Color: color, Period: cometPeriod, TailLen: cometTail},
			describeColor("comet", color),
		)
	},
}

var effectFireCmd = &cobra.Command{
	Use:   "fire",
	Short: "Flame simulation (cool/diffuse/spark, black→red→yellow→white)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		seed := fireSeed
		if seed == 0 {
			seed = uint64(time.Now().UnixNano())
		}
		return runEffect(cmd, ambilighteffects.NewFire(fireCooling, fireSparking, seed), "fire")
	},
}

func init() {
	// Parent persistent flags shared by every effect leaf.
	ambilightEffectCmd.PersistentFlags().DurationVar(&effectDuration, "duration", 6*time.Second, "how long to animate")
	ambilightEffectCmd.PersistentFlags().IntVar(&effectFPS, "fps", 12, "frames per second (TV throttles above ~15)")
	ambilightEffectCmd.PersistentFlags().BoolVar(&effectRestore, "restore", true, "restore mode=internal when done")

	effectWaveCmd.Flags().DurationVar(&wavePeriod, "period", 2*time.Second, "time for one wave to lap the ring")

	effectPulseCmd.Flags().DurationVar(&pulsePeriod, "period", 1500*time.Millisecond, "time for one breath in→out cycle")
	effectPulseCmd.Flags().Float64Var(&pulseFloor, "floor", 0.1, "minimum intensity, 0..1 (use 0 for full fade)")

	effectRainbowCmd.Flags().DurationVar(&rainbowPeriod, "period", 5*time.Second, "time for one full hue rotation")
	effectRainbowCmd.Flags().Float64Var(&rainbowSat, "saturation", 1.0, "color saturation, 0..1")
	effectRainbowCmd.Flags().Float64Var(&rainbowVal, "value", 1.0, "color value/brightness, 0..1")

	effectCometCmd.Flags().DurationVar(&cometPeriod, "period", 3*time.Second, "time for one lap")
	effectCometCmd.Flags().IntVar(&cometTail, "tail", 4, "length of fading tail in LEDs")

	effectFireCmd.Flags().IntVar(&fireCooling, "cooling", 55, "cooling rate (higher = flame dies out faster)")
	effectFireCmd.Flags().IntVar(&fireSparking, "sparking", 120, "spark probability 0..255 (higher = livelier)")
	effectFireCmd.Flags().Uint64Var(&fireSeed, "seed", 0, "rng seed (0 = use current time)")

	ambilightEffectCmd.AddCommand(
		effectWaveCmd,
		effectPulseCmd,
		effectRainbowCmd,
		effectCometCmd,
		effectFireCmd,
	)
	ambilightCmd.AddCommand(ambilightEffectCmd)
}

func pickEffectColor(args []string, fallback philips.AmbilightColor) (philips.AmbilightColor, error) {
	if len(args) == 0 {
		return fallback, nil
	}
	return parseAmbilightColor(args[0])
}

func describeColor(name string, c philips.AmbilightColor) string {
	return fmt.Sprintf("%s color #%02x%02x%02x", name, c.R, c.G, c.B)
}

// runEffect is the single entry point all effect subcommands share. It
// opens the client, runs the effect, and prints a one-line summary.
func runEffect(cmd *cobra.Command, e ambilighteffects.Effect, description string) error {
	if effectDuration <= 0 {
		return errors.New("--duration must be positive")
	}
	// Budget the context to the animation plus headroom for setup + restore.
	ctx, cancel := context.WithTimeout(cmd.Context(), effectDuration+10*time.Second)
	defer cancel()

	c, alias, err := openClient(ambilightDevice)
	if err != nil {
		return err
	}

	// Fetch topology once up front for the summary line — Run will fetch it
	// again, but the extra GET is cheap and the message is useful before
	// the effect starts.
	topo, err := c.Ambilight.Topology(ctx)
	if err != nil {
		return fmt.Errorf("topology: %w", err)
	}
	total := topo.Left + topo.Top + topo.Right + topo.Bottom
	fmt.Fprintf(cmd.OutOrStdout(),
		"%s: %d LEDs, %s, %s @ %d fps\n",
		alias, total, description, effectDuration, effectFPS,
	)

	return ambilighteffects.Run(ctx, c.Ambilight, e, effectFPS, effectDuration, effectRestore)
}
