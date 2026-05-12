package wol

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestMagicPacket_Shape(t *testing.T) {
	mac := net.HardwareAddr{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	pkt, err := MagicPacket(mac)
	if err != nil {
		t.Fatalf("MagicPacket: %v", err)
	}
	if len(pkt) != 102 {
		t.Fatalf("len=%d, want 102", len(pkt))
	}
	// Header: 6× 0xFF
	for i := 0; i < 6; i++ {
		if pkt[i] != 0xFF {
			t.Fatalf("header[%d]=0x%02X, want 0xFF", i, pkt[i])
		}
	}
	// Body: 16× MAC
	for i := 0; i < 16; i++ {
		off := 6 + i*6
		if !bytes.Equal(pkt[off:off+6], mac) {
			t.Fatalf("repeat[%d]=% X, want % X", i, pkt[off:off+6], mac)
		}
	}
}

func TestMagicPacket_RejectsBadMAC(t *testing.T) {
	for _, n := range []int{0, 3, 5, 7, 8} {
		mac := make(net.HardwareAddr, n)
		if _, err := MagicPacket(mac); err == nil {
			t.Errorf("MAC len %d: expected error", n)
		}
	}
}

func TestOptions_Defaults(t *testing.T) {
	o := Options{}.withDefaults()
	if o.Addr != DefaultAddr {
		t.Errorf("Addr=%q", o.Addr)
	}
	if o.Count != 3 {
		t.Errorf("Count=%d", o.Count)
	}
	if o.Interval != 400*time.Millisecond {
		t.Errorf("Interval=%v", o.Interval)
	}
}

func TestOptions_PreservesOverrides(t *testing.T) {
	o := Options{Addr: "127.0.0.1:9", Count: 5, Interval: 10 * time.Millisecond}.withDefaults()
	if o.Addr != "127.0.0.1:9" || o.Count != 5 || o.Interval != 10*time.Millisecond {
		t.Errorf("overrides clobbered: %+v", o)
	}
}

// TestSend_LocalLoopback exercises the full Send path against a UDP listener
// on 127.0.0.1, bypassing the global broadcast address so the test stays
// hermetic.
func TestSend_LocalLoopback(t *testing.T) {
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	addr := listener.LocalAddr().String()

	type recvd struct {
		buf []byte
		err error
	}
	got := make(chan recvd, 4)
	go func() {
		buf := make([]byte, 200)
		for {
			_ = listener.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, _, err := listener.ReadFromUDP(buf)
			if err != nil {
				got <- recvd{err: err}
				return
			}
			cp := make([]byte, n)
			copy(cp, buf[:n])
			got <- recvd{buf: cp}
		}
	}()

	mac := net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Send(ctx, mac, Options{Addr: addr, Count: 2, Interval: 20 * time.Millisecond}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	expected, _ := MagicPacket(mac)
	for i := 0; i < 2; i++ {
		select {
		case r := <-got:
			if r.err != nil {
				t.Fatalf("recv %d: %v", i+1, r.err)
			}
			if !bytes.Equal(r.buf, expected) {
				t.Fatalf("packet %d mismatch", i+1)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for packet %d", i+1)
		}
	}
}

func TestSend_RejectsBadMAC(t *testing.T) {
	ctx := context.Background()
	if err := Send(ctx, net.HardwareAddr{0x01, 0x02}, Options{}); err == nil {
		t.Fatal("expected MAC length error")
	}
}

func TestSend_ContextCancelled(t *testing.T) {
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = Send(ctx, net.HardwareAddr{1, 2, 3, 4, 5, 6}, Options{Addr: listener.LocalAddr().String(), Count: 3})
	if err == nil {
		t.Fatal("expected context error")
	}
}
