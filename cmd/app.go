package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/philips"
)

var (
	appDevice  string
	appTimeout time.Duration
)

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "Launch and inspect TV apps",
	Long: `Manage apps on a paired TV.

  couch app ls              List installed apps
  couch app current         Show the currently foreground app
  couch app launch <name>   Launch app by label or package name (fuzzy match)
  couch app close           Return to the home launcher`,
}

var appLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List installed apps",
	Args:  cobra.NoArgs,
	RunE:  runAppLs,
}

var appCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the currently foreground app",
	Args:  cobra.NoArgs,
	RunE:  runAppCurrent,
}

var appLaunchCmd = &cobra.Command{
	Use:   "launch <query>",
	Short: "Launch app by label or package name",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppLaunch,
}

var appCloseCmd = &cobra.Command{
	Use:   "close",
	Short: "Return to the home launcher",
	Args:  cobra.NoArgs,
	RunE:  runAppClose,
}

func init() {
	appCmd.PersistentFlags().StringVar(&appDevice, "device", "", "alias of a paired device (default: configured default)")
	appCmd.PersistentFlags().DurationVar(&appTimeout, "timeout", 8*time.Second, "request timeout")
	appCmd.AddCommand(appLsCmd, appCurrentCmd, appLaunchCmd, appCloseCmd)
	rootCmd.AddCommand(appCmd)
}

func runAppLs(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), appTimeout)
	defer cancel()
	c, alias, err := openClient(appDevice)
	if err != nil {
		return err
	}
	apps, err := c.Apps.List(ctx)
	if err != nil {
		return fmt.Errorf("list apps: %w", err)
	}
	sort.Slice(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Label) < strings.ToLower(apps[j].Label)
	})
	if jsonOutput {
		out := make([]map[string]any, 0, len(apps))
		for _, a := range apps {
			out = append(out, map[string]any{
				"label":        a.Label,
				"package_name": a.Intent.Component.PackageName,
				"class_name":   a.Intent.Component.ClassName,
			})
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"alias": alias, "apps": out})
	}
	if len(apps) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No apps reported.")
		return nil
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LABEL\tPACKAGE")
	for _, a := range apps {
		fmt.Fprintf(tw, "%s\t%s\n", truncateStr(a.Label, 40), a.Intent.Component.PackageName)
	}
	return tw.Flush()
}

func runAppCurrent(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), appTimeout)
	defer cancel()
	c, alias, err := openClient(appDevice)
	if err != nil {
		return err
	}
	comp, err := c.Apps.Current(ctx)
	if err != nil {
		return fmt.Errorf("get current app: %w", err)
	}
	// Best-effort label lookup against the installed-app list.
	label := ""
	if comp.PackageName != "" {
		if apps, err := c.Apps.List(ctx); err == nil {
			for _, a := range apps {
				if a.Intent.Component.PackageName == comp.PackageName {
					label = a.Label
					break
				}
			}
		}
	}
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"alias":        alias,
			"label":        label,
			"package_name": comp.PackageName,
			"class_name":   comp.ClassName,
		})
	}
	if comp.PackageName == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: (launcher)\n", alias)
		return nil
	}
	display := label
	if display == "" {
		display = comp.PackageName
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s  [%s]\n", alias, display, comp.PackageName)
	return nil
}

func runAppLaunch(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), appTimeout)
	defer cancel()
	c, alias, err := openClient(appDevice)
	if err != nil {
		return err
	}
	apps, err := c.Apps.List(ctx)
	if err != nil {
		return fmt.Errorf("list apps: %w", err)
	}
	matches := matchApps(apps, args[0])
	switch len(matches) {
	case 0:
		return fmt.Errorf("no app matching %q (try `couch app ls`)", args[0])
	case 1:
		if err := c.Apps.Launch(ctx, matches[0].Intent); err != nil {
			return fmt.Errorf("launch %q: %w", matches[0].Label, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: launched %s\n", alias, matches[0].Label)
		return nil
	default:
		labels := make([]string, 0, len(matches))
		for _, m := range matches {
			labels = append(labels, m.Label)
		}
		return fmt.Errorf("%q is ambiguous (%d matches: %s)", args[0], len(matches), strings.Join(labels, ", "))
	}
}

func runAppClose(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), appTimeout)
	defer cancel()
	c, _, err := openClient(appDevice)
	if err != nil {
		return err
	}
	return c.Apps.Home(ctx)
}

// matchApps returns apps whose Label or PackageName contains q (case-insensitive).
// An exact label match wins outright and shortcuts the result to a single entry.
func matchApps(apps []philips.Application, q string) []philips.Application {
	needle := strings.ToLower(strings.TrimSpace(q))
	if needle == "" {
		return nil
	}
	var exact []philips.Application
	var partial []philips.Application
	for _, a := range apps {
		label := strings.ToLower(a.Label)
		pkg := strings.ToLower(a.Intent.Component.PackageName)
		switch {
		case label == needle:
			exact = append(exact, a)
		case strings.Contains(label, needle), strings.Contains(pkg, needle):
			partial = append(partial, a)
		}
	}
	if len(exact) >= 1 {
		return exact
	}
	return partial
}
