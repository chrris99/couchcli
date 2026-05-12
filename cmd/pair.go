package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrris99/couchcli/internal/config"
	"github.com/chrris99/couchcli/internal/discovery"
	"github.com/chrris99/couchcli/internal/philips"
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
	pairLocation     string
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
	pairCmd.Flags().StringVar(&pairLocation, "location", "", "user-supplied location for the TV (e.g. \"Living Room\")")
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
		fmt.Fprintln(cmd.OutOrStdout(), hint)
	}

	// 2. Probe to figure out which transport works + grab system info.
	client, sys, err := philips.DetectEndpoint(ctx, addr)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", addr, err)
	}
	port := client.Port()
	secure := client.Secure()
	displayName := firstNonEmpty(sys.Name, sys.Model, candName, addr)
	fmt.Fprintf(cmd.OutOrStdout(), "Found %s at %s:%d — %s, API v%d.\n",
		displayName, addr, port, transportLabel(secure), sys.APIVersionMajor)

	// 3. Schema load + alias / collision check.
	schema, schemaPath, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	alias := pairAlias
	if alias == "" {
		alias = slugify(displayName)
		if alias == "" {
			alias = slugify(addr)
		}
	}
	if existing := schema.FindByAddress(addr); existing != "" && existing != alias && !pairForce {
		return fmt.Errorf("%s is already paired as %q — pass --force to overwrite or pair under a new --as alias", addr, existing)
	}
	if _, exists := schema.Get(alias); exists && !pairForce {
		if !confirm(cmd, fmt.Sprintf("Already paired as %q. Overwrite?", alias)) {
			return errors.New("aborted")
		}
	}

	// 4. Run the pair flow if required.
	var deviceID, authKey string
	if client.Pair.Required(sys) {
		if !secure {
			return fmt.Errorf("pairing requires HTTPS:1926 but only HTTP:1925 reachable on %s", addr)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Look at your TV — a PIN should appear on screen.")
		dev := philips.DeviceInfo{
			DeviceName: defaultClientName(),
			DeviceOS:   runtime.GOOS,
			AppID:      "1",
			AppName:    "couchcli",
			Type:       "native",
		}
		// philips.Pair attaches digest creds to client on success — make a
		// fresh client per attempt so a wrong PIN doesn't poison the
		// digest state for the next retry.
		var result philips.PairResult
		for attempt := 1; attempt <= 3; attempt++ {
			attemptClient := philips.NewSecure(addr)
			result, err = attemptClient.Pair.Run(ctx, dev, func(pCtx context.Context) (string, error) {
				return promptPIN(cmd, pCtx)
			})
			if err == nil {
				break
			}
			if errors.Is(err, philips.ErrPINRejected) && attempt < 3 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Wrong PIN. Try again (%d of 3).\n", attempt+1)
				continue
			}
			return pairExitError(err)
		}
		deviceID = result.DeviceID
		authKey = result.AuthKey
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "No pairing required for this device — saving entry.")
	}

	// 4b. Capture MAC for Wake-on-LAN. Order: --mac override > ARP lookup
	// against the TV's IP (works because pairing just touched it).
	macStr, macWarn := resolvePairMAC(ctx, addr)

	// 5. Persist.
	entry := &config.Device{
		Address:         addr,
		Port:            port,
		Secure:          secure,
		DeviceID:        deviceID,
		AuthKey:         authKey,
		Label:           strings.TrimSpace(pairLabel),
		Location:        strings.TrimSpace(pairLocation),
		MAC:             macStr,
		Name:            sys.Name,
		Model:           sys.Model,
		SerialNumber:    sys.SerialNumber,
		SoftwareVersion: sys.SoftwareVersion,
		APIVersion:      sys.APIVersionMajor,
		PairedAt:        time.Now().UTC(),
	}
	schema.Put(alias, entry)
	switch {
	case pairSetDefault:
		schema.Default = alias
	case pairNoSetDefault:
	default:
		if schema.Default == "" {
			schema.Default = alias
		}
	}
	if err := schema.Save(schemaPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// 6. Summary.
	if jsonOutput {
		out := map[string]any{
			"alias":    alias,
			"address":  entry.Address,
			"port":     entry.Port,
			"secure":   entry.Secure,
			"label":    entry.Label,
			"location": entry.Location,
			"name":     entry.Name,
			"model":    entry.Model,
			"path":     schemaPath,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"\nPaired with %s as %q\n  Saved to %s\n\nTry:\n  couch volume\n  couch volume up\n",
		displayName, alias, schemaPath)
	if macStr != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  couch device on %s   # waking via Wake-on-LAN\n", alias)
	} else if macWarn != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "\nWarning: %s\n  Run `couch device mac %s <MAC>` to enable `couch device on`.\n", macWarn, alias)
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
		devices, err := discoverForPair(ctx)
		if err != nil {
			return "", "", "", err
		}
		match := strings.ToLower(pairNameMatch)
		for _, d := range devices {
			if strings.Contains(strings.ToLower(d.Name), match) {
				return d.Address, d.Name, fmt.Sprintf("Matched %q to %s at %s", pairNameMatch, d.Name, d.Address), nil
			}
		}
		return "", "", "", fmt.Errorf("no discovered TV matched name %q", pairNameMatch)
	}

	// Interactive picker.
	devices, err := discoverForPair(ctx)
	if err != nil {
		return "", "", "", err
	}
	if len(devices) == 0 {
		return "", "", "", errors.New("no TVs discovered — pass an address explicitly: `couch pair <ip>`")
	}
	// Collapse to unique addresses (preserving the first row seen).
	seen := map[string]bool{}
	var unique []discovery.Device
	for _, d := range devices {
		if seen[d.Address] {
			continue
		}
		seen[d.Address] = true
		unique = append(unique, d)
	}
	if len(unique) == 1 {
		return unique[0].Address, unique[0].Name, fmt.Sprintf("Found one TV: %s (%s)", unique[0].Name, unique[0].Address), nil
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Discovered TVs:")
	for i, d := range unique {
		fmt.Fprintf(out, "  [%d] %s  %s\n", i+1, d.Name, d.Address)
	}
	fmt.Fprint(out, "Pick a number: ")
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", "", "", fmt.Errorf("read selection: %w", err)
	}
	idx, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || idx < 1 || idx > len(unique) {
		return "", "", "", fmt.Errorf("invalid selection")
	}
	return unique[idx-1].Address, unique[idx-1].Name, "", nil
}

func discoverForPair(ctx context.Context) ([]discovery.Device, error) {
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return discovery.Discover(dctx, discovery.Options{Timeout: 4 * time.Second})
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

// promptPIN reads a 4-digit PIN from stdin. Echoes (PIN is visible on the TV
// anyway). Cancels promptly if pCtx expires.
func promptPIN(cmd *cobra.Command, pCtx context.Context) (string, error) {
	type res struct {
		pin string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		fmt.Fprint(cmd.OutOrStdout(), "Enter PIN: ")
		reader := bufio.NewReader(cmd.InOrStdin())
		line, err := reader.ReadString('\n')
		ch <- res{pin: strings.TrimSpace(line), err: err}
	}()
	select {
	case <-pCtx.Done():
		return "", pCtx.Err()
	case r := <-ch:
		if r.err != nil && r.err != io.EOF {
			return "", r.err
		}
		return r.pin, nil
	}
}

func confirm(cmd *cobra.Command, q string) bool {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", q)
	reader := bufio.NewReader(cmd.InOrStdin())
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
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
	case errors.Is(err, philips.ErrPINRejected):
		return fmt.Errorf("too many wrong PIN attempts — re-run `couch pair` to retry")
	case errors.Is(err, philips.ErrTimeout):
		return fmt.Errorf("pairing timed out — the TV PIN expired; re-run the command")
	case errors.Is(err, philips.ErrPairingRefused):
		return err
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return fmt.Errorf("couldn't reach TV: %w", err)
	}
	return err
}
