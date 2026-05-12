package wol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// ErrMACNotFound is returned when the ARP cache lookup completes but no
// MAC was found for the requested IP. The caller can prompt the user to
// supply --mac instead.
var ErrMACNotFound = errors.New("wol: MAC not found in ARP cache")

// macRE matches a colon- or hyphen-separated six-octet MAC, case-insensitive.
// We allow either separator because Windows `arp -a` uses '-'.
var macRE = regexp.MustCompile(`(?i)\b([0-9a-f]{1,2})[:\-]([0-9a-f]{1,2})[:\-]([0-9a-f]{1,2})[:\-]([0-9a-f]{1,2})[:\-]([0-9a-f]{1,2})[:\-]([0-9a-f]{1,2})\b`)

// LookupMAC asks the OS for the MAC address associated with ip from the
// local ARP cache. The IP must have been recently contacted (the pair flow
// satisfies this on success).
//
// On macOS/Linux it shells out to `arp -n <ip>`; on Windows to `arp -a <ip>`.
// Returns ErrMACNotFound if the cache has no entry, or if the entry is
// incomplete (e.g. "(incomplete)" on Linux).
func LookupMAC(ctx context.Context, ip string) (net.HardwareAddr, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil, fmt.Errorf("wol: invalid IP %q", ip)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "arp", "-a", ip)
	default:
		cmd = exec.CommandContext(ctx, "arp", "-n", ip)
	}

	out, err := cmd.Output()
	if err != nil {
		// `arp` exits non-zero when there's no entry on some platforms.
		// Still try to parse whatever stdout we got before giving up.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("wol: run arp: %w", err)
		}
	}

	return parseARPOutput(string(out), ip)
}

// parseARPOutput finds the MAC for ip in raw `arp` output. Public for tests.
func parseARPOutput(raw, ip string) (net.HardwareAddr, error) {
	for _, line := range strings.Split(raw, "\n") {
		// Skip lines that don't mention our IP. On macOS the IP is in
		// parens, e.g. "tv (192.168.0.42) at aa:bb:..."; on Linux it's
		// the first column; on Windows it's the first column too.
		if !lineMentionsIP(line, ip) {
			continue
		}
		if strings.Contains(line, "incomplete") {
			continue
		}
		m := macRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// macRE captures may have 1-digit octets (macOS strips leading
		// zeros). Re-pad before handing to net.ParseMAC.
		padded := fmt.Sprintf("%02s:%02s:%02s:%02s:%02s:%02s",
			m[1], m[2], m[3], m[4], m[5], m[6])
		mac, err := net.ParseMAC(padded)
		if err != nil {
			continue
		}
		// Reject all-zero or all-FF results — these are unicast targets
		// for an unresolved entry on some Linux kernels.
		if isAllZeroOrFF(mac) {
			continue
		}
		return mac, nil
	}
	return nil, ErrMACNotFound
}

func lineMentionsIP(line, ip string) bool {
	// Strip surrounding parens / brackets / whitespace before matching to
	// catch "tv (192.168.0.42)" and bare "192.168.0.42" alike.
	return strings.Contains(line, ip)
}

func isAllZeroOrFF(mac net.HardwareAddr) bool {
	zero := true
	ff := true
	for _, b := range mac {
		if b != 0 {
			zero = false
		}
		if b != 0xFF {
			ff = false
		}
	}
	return zero || ff
}
