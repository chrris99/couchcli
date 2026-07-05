// Package wol sends Wake-on-LAN magic packets over UDP broadcast.
//
// A magic packet is a fixed-shape frame the device's NIC scans for while in
// standby: a synchronization header of six 0xFF bytes, followed by 16
// repetitions of the target 6-byte MAC. Total length is 102 bytes.
//
// This package uses plain UDP broadcast (no raw sockets, no admin needed).
// The TV must have "Wake on WLAN" enabled in its network settings, and the
// host must be on the same Layer-2 broadcast domain (same Wi-Fi / LAN).
package wol

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DefaultAddr is the conventional WoL broadcast target. Port 9 (discard)
// works on every Philips Android TV we've tested; port 7 (echo) is the
// historical alternative.
const DefaultAddr = "255.255.255.255:9"

// Options tunes how Send delivers the magic packet(s).
type Options struct {
	// Addr is the UDP target. Defaults to DefaultAddr.
	Addr string
	// Count is how many magic packets to send. The NIC samples
	// intermittently in standby, so a small burst raises the wake
	// probability without harm. Defaults to 3.
	Count int
	// Interval is the gap between consecutive packets. Defaults to 400ms.
	Interval time.Duration
}

func (o Options) withDefaults() Options {
	if o.Addr == "" {
		o.Addr = DefaultAddr
	}
	if o.Count <= 0 {
		o.Count = 3
	}
	if o.Interval <= 0 {
		o.Interval = 400 * time.Millisecond
	}
	return o
}

// MagicPacket builds a 102-byte WoL frame for mac.
//
//	[0:6]    0xFF 0xFF 0xFF 0xFF 0xFF 0xFF
//	[6:102]  mac repeated 16 times
func MagicPacket(mac net.HardwareAddr) ([]byte, error) {
	if len(mac) != 6 {
		return nil, fmt.Errorf("wol: MAC must be 6 bytes, got %d", len(mac))
	}
	pkt := make([]byte, 6+16*6)
	for i := 0; i < 6; i++ {
		pkt[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		copy(pkt[6+i*6:], mac)
	}
	return pkt, nil
}

// Send broadcasts the magic packet for mac to opts.Addr.
//
// It applies defaults to opts and sends opts.Count packets opts.Interval
// apart. Returns the first error encountered; context cancellation is
// honoured between packets.
func Send(ctx context.Context, mac net.HardwareAddr, opts Options) error {
	opts = opts.withDefaults()

	pkt, err := MagicPacket(mac)
	if err != nil {
		return err
	}

	udpAddr, err := net.ResolveUDPAddr("udp4", opts.Addr)
	if err != nil {
		return fmt.Errorf("wol: resolve %q: %w", opts.Addr, err)
	}

	// Source port 0 → kernel picks an ephemeral one. Bind to 0.0.0.0 so
	// the kernel routes the broadcast out the default interface.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("wol: open udp: %w", err)
	}
	defer conn.Close()

	for i := 0; i < opts.Count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Cap the per-write deadline at whichever is sooner: the remaining
		// context or 1 second. Sending to a broadcast address shouldn't
		// block, but a deadline keeps us honest.
		deadline := time.Now().Add(time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("wol: set deadline: %w", err)
		}
		if _, err := conn.WriteToUDP(pkt, udpAddr); err != nil {
			return fmt.Errorf("wol: write %d/%d: %w", i+1, opts.Count, err)
		}
		if i == opts.Count-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.Interval):
		}
	}
	return nil
}
