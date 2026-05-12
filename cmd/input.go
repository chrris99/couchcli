package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/philips"
)

var (
	inputDevice  string
	inputTimeout time.Duration
)

var inputCmd = &cobra.Command{
	Use:   "input",
	Short: "List and switch TV input sources",
	Long: `Manage input sources (HDMI ports, built-in tuner, …) on a paired TV.

  couch input ls               List available inputs
  couch input current          Show the active input
  couch input switch <query>   Switch to an input (fuzzy match on ID or label)
  couch input picker           Open the on-screen source selector (fallback)`,
}

var inputLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List available inputs",
	Args:  cobra.NoArgs,
	RunE:  runInputLs,
}

var inputCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the active input",
	Args:  cobra.NoArgs,
	RunE:  runInputCurrent,
}

var inputSwitchCmd = &cobra.Command{
	Use:   "switch <query>",
	Short: "Switch to an input by ID or label",
	Args:  cobra.ExactArgs(1),
	RunE:  runInputSwitch,
}

var inputPickerCmd = &cobra.Command{
	Use:   "picker",
	Short: "Open the on-screen source selector",
	Args:  cobra.NoArgs,
	RunE:  runInputPicker,
}

func init() {
	inputCmd.PersistentFlags().StringVar(&inputDevice, "device", "", "alias of a paired device (default: configured default)")
	inputCmd.PersistentFlags().DurationVar(&inputTimeout, "timeout", 5*time.Second, "request timeout")
	inputCmd.AddCommand(inputLsCmd, inputCurrentCmd, inputSwitchCmd, inputPickerCmd)
	rootCmd.AddCommand(inputCmd)
}

func runInputLs(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), inputTimeout)
	defer cancel()
	c, alias, err := openClient(inputDevice)
	if err != nil {
		return err
	}
	inputs, err := c.Inputs.List(ctx)
	if err != nil {
		return fmt.Errorf("list inputs: %w", err)
	}
	if jsonOutput {
		out := make([]map[string]any, 0, len(inputs))
		for _, i := range inputs {
			out = append(out, map[string]any{
				"id":    i.ID,
				"label": i.Label,
				"kind":  i.Kind,
			})
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"alias": alias, "inputs": out})
	}
	if len(inputs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No inputs reported by this TV.")
		fmt.Fprintln(cmd.OutOrStdout(), "Try `couch input picker` to open the on-screen selector,")
		fmt.Fprintln(cmd.OutOrStdout(), "or `couch input switch hdmi1` directly (Switch works even when List is empty).")
		return nil
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tLABEL")
	for _, i := range inputs {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", i.ID, i.Kind, truncateStr(i.Label, 40))
	}
	return tw.Flush()
}

func runInputCurrent(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), inputTimeout)
	defer cancel()
	c, alias, err := openClient(inputDevice)
	if err != nil {
		return err
	}
	cur, err := c.Inputs.Current(ctx)
	if err != nil {
		return fmt.Errorf("get current input: %w", err)
	}
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias": alias,
			"id":    cur.ID,
			"label": cur.Label,
			"kind":  cur.Kind,
		})
	}
	if cur.ID == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: (none reported)\n", alias)
		return nil
	}
	label := cur.Label
	if label == "" {
		label = cur.ID
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s  [%s]\n", alias, label, cur.ID)
	return nil
}

func runInputSwitch(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), inputTimeout)
	defer cancel()
	c, alias, err := openClient(inputDevice)
	if err != nil {
		return err
	}

	target := philips.Input{ID: args[0]}
	// Try to resolve via ListInputs for a friendlier match. If empty or it
	// errors, fall through and POST the raw arg as the ID — that's the
	// documented fallback path.
	if inputs, err := c.Inputs.List(ctx); err == nil && len(inputs) > 0 {
		matches := matchInputs(inputs, args[0])
		switch len(matches) {
		case 0:
			return fmt.Errorf("no input matching %q (try `couch input ls`)", args[0])
		case 1:
			target = matches[0]
		default:
			ids := make([]string, 0, len(matches))
			for _, m := range matches {
				ids = append(ids, m.ID)
			}
			return fmt.Errorf("%q is ambiguous (%d matches: %s)", args[0], len(matches), strings.Join(ids, ", "))
		}
	}

	if err := c.Inputs.Switch(ctx, target); err != nil {
		return fmt.Errorf("switch to %q: %w", target.ID, err)
	}
	label := target.Label
	if label == "" {
		label = target.ID
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: switched to %s\n", alias, label)
	return nil
}

func runInputPicker(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), inputTimeout)
	defer cancel()
	c, _, err := openClient(inputDevice)
	if err != nil {
		return err
	}
	return c.Inputs.OpenPicker(ctx)
}

// matchInputs is the same shape as matchApps: exact-ID match wins, otherwise
// case-insensitive substring on ID then Label.
func matchInputs(inputs []philips.Input, q string) []philips.Input {
	needle := strings.ToLower(strings.TrimSpace(q))
	if needle == "" {
		return nil
	}
	var exact []philips.Input
	var partial []philips.Input
	for _, in := range inputs {
		id := strings.ToLower(in.ID)
		label := strings.ToLower(in.Label)
		switch {
		case id == needle, label == needle:
			exact = append(exact, in)
		case strings.Contains(id, needle), strings.Contains(label, needle):
			partial = append(partial, in)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}
