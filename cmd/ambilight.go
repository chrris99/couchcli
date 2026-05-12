package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/philips"
)

var (
	ambilightDevice  string
	ambilightTimeout time.Duration
)

var ambilightCmd = &cobra.Command{
	Use:   "ambilight",
	Short: "Inspect and control the TV's Ambilight LEDs",
	Long: `Control Philips Ambilight.

  couch ambilight                         Show power + mode + style
  couch ambilight on / off                Toggle the LED ring
  couch ambilight mode <internal|manual|expert>
  couch ambilight style                   List supported styles
  couch ambilight style <NAME>            Switch to a style (Android firmware)
  couch ambilight color <hex|name>        Paint a solid color (forces manual mode)
  couch ambilight topology                Print LED layout`,
	RunE: runAmbilightStatus,
}

var ambilightOnCmd = &cobra.Command{
	Use:   "on",
	Short: "Power Ambilight on",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runAmbilightPower(cmd, philips.AmbilightPowerOn)
	},
}

var ambilightOffCmd = &cobra.Command{
	Use:   "off",
	Short: "Power Ambilight off",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runAmbilightPower(cmd, philips.AmbilightPowerOff)
	},
}

var ambilightModeCmd = &cobra.Command{
	Use:   "mode <internal|manual|expert>",
	Short: "Set render mode",
	Args:  cobra.ExactArgs(1),
	RunE:  runAmbilightMode,
}

var ambilightStyleCmd = &cobra.Command{
	Use:   "style [NAME]",
	Short: "List supported styles or switch to one",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAmbilightStyle,
}

var ambilightColorCmd = &cobra.Command{
	Use:   "color <hex|name>",
	Short: "Paint a solid color across all LEDs",
	Args:  cobra.ExactArgs(1),
	RunE:  runAmbilightColor,
}

var ambilightTopologyCmd = &cobra.Command{
	Use:   "topology",
	Short: "Print the LED layout",
	Args:  cobra.NoArgs,
	RunE:  runAmbilightTopology,
}

func init() {
	ambilightCmd.PersistentFlags().StringVar(&ambilightDevice, "device", "", "alias of a paired device (default: configured default)")
	ambilightCmd.PersistentFlags().DurationVar(&ambilightTimeout, "timeout", 8*time.Second, "request timeout")
	ambilightCmd.AddCommand(
		ambilightOnCmd, ambilightOffCmd,
		ambilightModeCmd,
		ambilightStyleCmd,
		ambilightColorCmd,
		ambilightTopologyCmd,
	)
	rootCmd.AddCommand(ambilightCmd)
}

func runAmbilightStatus(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), ambilightTimeout)
	defer cancel()
	c, alias, err := openClient(ambilightDevice)
	if err != nil {
		return err
	}
	power, err := c.Ambilight.GetPower(ctx)
	if err != nil {
		return fmt.Errorf("get power: %w", err)
	}
	mode, err := c.Ambilight.GetMode(ctx)
	if err != nil {
		return fmt.Errorf("get mode: %w", err)
	}
	// Style is best-effort — older firmware 404s.
	var styleName philips.AmbilightStyleName
	var menuSetting string
	if cfg, err := c.Ambilight.Configuration(ctx); err == nil {
		styleName = cfg.StyleName
		menuSetting = cfg.MenuSetting
	} else if !philips.IsNotFound(err) {
		return fmt.Errorf("get configuration: %w", err)
	}
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias":        alias,
			"power":        power,
			"mode":         mode,
			"style":        styleName,
			"menu_setting": menuSetting,
		})
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "power:\t%s\n", power)
	fmt.Fprintf(tw, "mode:\t%s\n", mode)
	if styleName != "" {
		if menuSetting != "" {
			fmt.Fprintf(tw, "style:\t%s (%s)\n", styleName, menuSetting)
		} else {
			fmt.Fprintf(tw, "style:\t%s\n", styleName)
		}
	}
	return tw.Flush()
}

func runAmbilightPower(cmd *cobra.Command, p philips.AmbilightPower) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), ambilightTimeout)
	defer cancel()
	c, _, err := openClient(ambilightDevice)
	if err != nil {
		return err
	}
	return c.Ambilight.SetPower(ctx, p)
}

func runAmbilightMode(cmd *cobra.Command, args []string) error {
	mode := philips.AmbilightMode(strings.ToLower(args[0]))
	switch mode {
	case philips.AmbilightModeInternal, philips.AmbilightModeManual, philips.AmbilightModeExpert:
	default:
		return fmt.Errorf("invalid mode %q (want internal|manual|expert)", args[0])
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), ambilightTimeout)
	defer cancel()
	c, _, err := openClient(ambilightDevice)
	if err != nil {
		return err
	}
	return c.Ambilight.SetMode(ctx, mode)
}

func runAmbilightStyle(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), ambilightTimeout)
	defer cancel()
	c, alias, err := openClient(ambilightDevice)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		styles, err := c.Ambilight.SupportedStyles(ctx)
		if err != nil {
			if philips.IsNotFound(err) {
				return errors.New("this TV does not expose styles — try `mode manual` + `color`")
			}
			return fmt.Errorf("list styles: %w", err)
		}
		if jsonOutput {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{"alias": alias, "styles": styles})
		}
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "STYLE\tALGORITHM")
		for _, s := range styles {
			fmt.Fprintf(tw, "%s\t%s\n", s.StyleName, s.Algorithm)
		}
		return tw.Flush()
	}

	cfg := philips.AmbilightConfiguration{
		StyleName: philips.AmbilightStyleName(strings.ToUpper(args[0])),
	}
	if err := c.Ambilight.SetConfiguration(ctx, cfg); err != nil {
		return fmt.Errorf("set style %q: %w", args[0], err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: style %s\n", alias, cfg.StyleName)
	return nil
}

func runAmbilightColor(cmd *cobra.Command, args []string) error {
	color, err := parseAmbilightColor(args[0])
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), ambilightTimeout)
	defer cancel()
	c, alias, err := openClient(ambilightDevice)
	if err != nil {
		return err
	}
	if _, err := c.Ambilight.SetSolidColor(ctx, color); err != nil {
		return fmt.Errorf("set color: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: painted #%02x%02x%02x\n", alias, color.R, color.G, color.B)
	return nil
}

func runAmbilightTopology(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), ambilightTimeout)
	defer cancel()
	c, alias, err := openClient(ambilightDevice)
	if err != nil {
		return err
	}
	top, err := c.Ambilight.Topology(ctx)
	if err != nil {
		return fmt.Errorf("topology: %w", err)
	}
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias":    alias,
			"layers":   top.Layers,
			"left":     top.Left,
			"top":      top.Top,
			"right":    top.Right,
			"bottom":   top.Bottom,
		})
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "layers:\t%d\n", top.Layers)
	fmt.Fprintf(tw, "left:\t%d\n", top.Left)
	fmt.Fprintf(tw, "top:\t%d\n", top.Top)
	fmt.Fprintf(tw, "right:\t%d\n", top.Right)
	fmt.Fprintf(tw, "bottom:\t%d\n", top.Bottom)
	return tw.Flush()
}

// namedAmbilightColors covers the handful of presets pylips ships, so users
// familiar with that CLI can reuse the same names.
var namedAmbilightColors = map[string]philips.AmbilightColor{
	"black":     {R: 0, G: 0, B: 0},
	"white":     {R: 255, G: 255, B: 255},
	"warmwhite": {R: 255, G: 200, B: 120},
	"red":       {R: 255, G: 0, B: 0},
	"green":     {R: 0, G: 255, B: 0},
	"blue":      {R: 0, G: 0, B: 255},
	"yellow":    {R: 255, G: 255, B: 0},
	"cyan":      {R: 0, G: 255, B: 255},
	"magenta":   {R: 255, G: 0, B: 255},
	"orange":    {R: 255, G: 128, B: 0},
	"purple":    {R: 128, G: 0, B: 128},
}

// parseAmbilightColor accepts "#RRGGBB", "RRGGBB", or a named preset.
func parseAmbilightColor(s string) (philips.AmbilightColor, error) {
	s = strings.TrimSpace(s)
	if c, ok := namedAmbilightColors[strings.ToLower(s)]; ok {
		return c, nil
	}
	hex := strings.TrimPrefix(s, "#")
	if len(hex) != 6 {
		return philips.AmbilightColor{}, fmt.Errorf("color %q: want #RRGGBB or a preset name (e.g. red, warmwhite)", s)
	}
	r, errR := strconv.ParseUint(hex[0:2], 16, 8)
	g, errG := strconv.ParseUint(hex[2:4], 16, 8)
	b, errB := strconv.ParseUint(hex[4:6], 16, 8)
	if errR != nil || errG != nil || errB != nil {
		return philips.AmbilightColor{}, fmt.Errorf("color %q: invalid hex", s)
	}
	return philips.AmbilightColor{R: uint8(r), G: uint8(g), B: uint8(b)}, nil
}
