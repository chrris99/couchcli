package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/discovery"
	philipsdriver "github.com/chrris99/couchcli/internal/drivers/philips"
	"github.com/chrris99/couchcli/internal/registry"
	"github.com/chrris99/couchcli/internal/ui"
	"github.com/chrris99/couchcli/internal/wol"
)

var (
	pairAlias        string
	pairForce        bool
	pairSetDefault   bool
	pairNoSetDefault bool
	pairDeviceName   string
	pairTimeout      time.Duration
	pairNameMatch    string
	pairLabel        string
	pairRoom         string
	pairMAC          string
)

var pairCmd = &cobra.Command{
	Use:   "pair [address]",
	Short: "Pair couchcli with a TV",
	Long: `Pair couchcli with a Philips smart TV.

Usage:
  couch pair                      Interactive — discover and pick a TV
  couch pair 192.168.0.42         Pair with a specific address (or hostname)
  couch pair --name "Living Room" Match by discovered name

For Android Philips TVs (API v6+), the TV displays a 4-digit PIN. Type it
when prompted. Credentials are saved to a per-user config file with mode
0600. Legacy non-Android Philips TVs are saved without pairing — no PIN
required.`,
	RunE: runPair,
}

func init() {
	pairCmd.Flags().StringVar(&pairAlias, "as", "", "alias to save this TV under (default: slugified TV name)")
	pairCmd.Flags().BoolVar(&pairForce, "force", false, "overwrite an existing entry without prompting")
	pairCmd.Flags().BoolVar(&pairSetDefault, "set-default", false, "set this TV as the default for future commands")
	pairCmd.Flags().BoolVar(&pairNoSetDefault, "no-set-default", false, "do not set this TV as the default")
	pairCmd.Flags().StringVar(&pairDeviceName, "device-name", "", "name to send to the TV (visible in its paired-devices list)")
	pairCmd.Flags().DurationVar(&pairTimeout, "timeout", 2*time.Minute, "overall command timeout")
	pairCmd.Flags().StringVar(&pairNameMatch, "name", "", "match a discovered TV by name (fuzzy)")
	pairCmd.Flags().StringVar(&pairLabel, "label", "", "user-supplied friendly name for the TV (e.g. \"Living Room TV\")")
	pairCmd.Flags().StringVar(&pairRoom, "room", "", "room the TV is in (e.g. \"living-room\")")
	pairCmd.Flags().StringVar(&pairMAC, "mac", "", "TV's MAC address (for Wake-on-LAN); auto-detected from the ARP cache if omitted")
	rootCmd.AddCommand(pairCmd)
}

func runPair(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), pairTimeout)
	defer cancel()

	// 1. Resolve the target address.
	addr, candName, hint, err := resolveTarget(ctx, cmd, args)
	if err != nil {
		return err
	}
	if hint != "" {
		ui.Infof(cmd.OutOrStdout(), "%s", hint)
	}

	// 2. Probe to figure out which transport works + grab system info.
	var prof philipsdriver.Profile
	if err := ui.Spin(ctx, "Probing "+addr+"…", func(sctx context.Context) error {
		var perr error
		prof, perr = philipsdriver.Detect(sctx, addr)
		return perr
	}); err != nil {
		return fmt.Errorf("could not reach %s: %w", addr, err)
	}
	displayName := firstNonEmpty(prof.Name, prof.Model, candName, addr)
	ui.Infof(cmd.OutOrStdout(), "Found %s at %s:%d — %s, API v%d.",
		displayName, addr, prof.Port, transportLabel(prof.Secure), prof.APIVersion)

	// 3. Registry load + alias / collision check.
	reg, err := registry.Load()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	alias := pairAlias
	if alias == "" {
		alias = slugify(displayName)
		if alias == "" {
			alias = slugify(addr)
		}
	}
	if existing := reg.FindByAddress(addr); existing != "" && existing != alias && !pairForce {
		return fmt.Errorf("%s is already paired as %q — pass --force to overwrite or pair under a new --as alias", addr, existing)
	}
	if _, exists := reg.Get(alias); exists && !pairForce {
		ok, cerr := ui.Confirm(fmt.Sprintf("Already paired as %q. Overwrite?", alias))
		if cerr != nil || !ok {
			return errors.New("aborted — pass --force to overwrite")
		}
	}

	// 4. Run the pair flow if required. Each attempt uses a fresh client
	// (inside philipsdriver.Pair) so a wrong PIN can't poison digest state.
	var deviceID, authKey string
	if prof.PairingRequired {
		if !prof.Secure {
			return fmt.Errorf("pairing requires HTTPS:1926 but only HTTP:1925 reachable on %s", addr)
		}
		ui.Infof(cmd.OutOrStdout(), "Look at your TV — a PIN should appear on screen.")
		for attempt := 1; attempt <= 3; attempt++ {
			deviceID, authKey, err = philipsdriver.Pair(ctx, addr, defaultClientName(), func(pCtx context.Context) (string, error) {
				return ui.PromptPIN(pCtx)
			})
			if err == nil {
				break
			}
			if errors.Is(err, philipsdriver.ErrPINRejected) && attempt < 3 {
				ui.Warnf(cmd.ErrOrStderr(), "Wrong PIN. Try again (%d of 3).", attempt+1)
				continue
			}
			return pairExitError(err)
		}
	} else {
		ui.Infof(cmd.OutOrStdout(), "No pairing required for this device — saving entry.")
	}

	// 4b. Capture MAC for Wake-on-LAN. Order: --mac override > ARP lookup
	// against the TV's IP (works because pairing just touched it).
	macStr, macWarn := resolvePairMAC(ctx, addr)

	// 5. Persist.
	settings, err := prof.Settings(deviceID, authKey).Encode()
	if err != nil {
		return err
	}
	entry := &registry.Device{
		Driver:     philipsdriver.ID,
		Address:    addr,
		MAC:        macStr,
		HardwareID: prof.SerialNumber,
		Label:      strings.TrimSpace(pairLabel),
		Room:       strings.TrimSpace(pairRoom),
		PairedAt:   time.Now().UTC(),
		Settings:   settings,
	}
	reg.Put(alias, entry)
	switch {
	case pairSetDefault:
		reg.Default = alias
	case pairNoSetDefault:
	default:
		if reg.Default == "" {
			reg.Default = alias
		}
	}
	if err := reg.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}

	// 6. Summary.
	if jsonOutput {
		out := map[string]any{
			"alias":   alias,
			"driver":  entry.Driver,
			"address": entry.Address,
			"port":    prof.Port,
			"secure":  prof.Secure,
			"label":   entry.Label,
			"room":    entry.Room,
			"name":    prof.Name,
			"model":   prof.Model,
			"path":    reg.Path(),
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	ui.Successf(cmd.OutOrStdout(), "Paired with %s as %q", displayName, alias)
	ui.Infof(cmd.OutOrStdout(), "  Saved to %s\n\nTry:\n  couch volume\n  couch volume up", reg.Path())
	if macStr != "" {
		ui.Infof(cmd.OutOrStdout(), "  couch device on %s   # powerstate=On (Wake-on-LAN fallback if unreachable)", alias)
	} else if macWarn != "" {
		ui.Warnf(cmd.ErrOrStderr(), "%s", macWarn)
		ui.Infof(cmd.ErrOrStderr(), "  `couch device on` will still work while the TV is reachable. Re-run `couch pair --mac <MAC> --as %s --force` to enable the Wake-on-LAN fallback for when the TV is fully offline.", alias)
	}
	return nil
}

// resolvePairMAC returns the MAC string to persist, plus a non-empty warning
// message if no MAC could be resolved. Order: --mac flag (validated), then
// ARP cache (silent on miss; warning bubbled up).
func resolvePairMAC(ctx context.Context, addr string) (string, string) {
	if pairMAC != "" {
		mac, err := net.ParseMAC(strings.TrimSpace(pairMAC))
		if err != nil {
			return "", fmt.Sprintf("--mac %q is not a valid MAC: %v", pairMAC, err)
		}
		return mac.String(), ""
	}
	// ARP needs a bare IP. If addr is a hostname, resolve to IPv4 first.
	ip := addr
	if net.ParseIP(addr) == nil {
		if ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", addr); err == nil && len(ips) > 0 {
			ip = ips[0].String()
		}
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	mac, err := wol.LookupMAC(lookupCtx, ip)
	if err != nil {
		return "", fmt.Sprintf("couldn't auto-detect MAC for %s (%v)", addr, err)
	}
	return mac.String(), ""
}

// resolveTarget figures out which address to pair with. Returns (addr,
// discoveredName, userHint, error).
func resolveTarget(ctx context.Context, cmd *cobra.Command, args []string) (string, string, string, error) {
	if len(args) > 0 {
		return args[0], "", "", nil
	}
	if pairNameMatch != "" {
		candidates, err := discoverForPair(ctx)
		if err != nil {
			return "", "", "", err
		}
		match := strings.ToLower(pairNameMatch)
		for _, c := range candidates {
			if strings.Contains(strings.ToLower(c.Name), match) {
				return c.Address, c.Name, fmt.Sprintf("Matched %q to %s at %s", pairNameMatch, c.Name, c.Address), nil
			}
		}
		return "", "", "", fmt.Errorf("no discovered TV matched name %q", pairNameMatch)
	}

	// Interactive picker.
	candidates, err := discoverForPair(ctx)
	if err != nil {
		return "", "", "", err
	}
	if len(candidates) == 1 {
		return candidates[0].Address, candidates[0].Name, fmt.Sprintf("Found one TV: %s (%s)", candidates[0].Name, candidates[0].Address), nil
	}
	picked, err := ui.PickCandidate("Which TV do you want to pair?", candidates)
	if err != nil {
		if errors.Is(err, ui.ErrNotInteractive) {
			return "", "", "", fmt.Errorf("multiple TVs found — pass an address explicitly: `couch pair <ip>`")
		}
		return "", "", "", err
	}
	return picked.Address, picked.Name, "", nil
}

// discoverForPair sweeps with only the philips matcher — pairing speaks
// JointSpace. Sweep errors are fatal only when nothing was found; a partial
// scan that still saw the TV is good enough to pair with.
func discoverForPair(ctx context.Context) ([]device.Candidate, error) {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var candidates []device.Candidate
	var err error
	if spinErr := ui.Spin(dctx, "Scanning for TVs…", func(sctx context.Context) error {
		candidates, err = discovery.Sweep(sctx, discovery.Options{Timeout: 4 * time.Second}, philipsdriver.Driver{})
		return nil
	}); spinErr != nil {
		return nil, spinErr
	}
	if len(candidates) == 0 {
		if err != nil {
			return nil, fmt.Errorf("discovery failed: %w", err)
		}
		return nil, errors.New("no TVs discovered — pass an address explicitly: `couch pair <ip>`")
	}
	return candidates, nil
}

func transportLabel(secure bool) string {
	if secure {
		return "secure (HTTPS)"
	}
	return "plain (HTTP)"
}

func defaultClientName() string {
	if pairDeviceName != "" {
		return pairDeviceName
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "couchcli"
	}
	return host
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = slugRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// pairExitError maps philips-typed errors to friendly CLI messages.
func pairExitError(err error) error {
	switch {
	case errors.Is(err, philipsdriver.ErrPINRejected):
		return fmt.Errorf("too many wrong PIN attempts — re-run `couch pair` to retry")
	case errors.Is(err, philipsdriver.ErrTimeout):
		return fmt.Errorf("pairing timed out — the TV PIN expired; re-run the command")
	case errors.Is(err, philipsdriver.ErrPairingRefused):
		return err
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return fmt.Errorf("couldn't reach TV: %w", err)
	}
	return err
}
