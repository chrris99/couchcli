package wol

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestParseARPOutput(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		raw  string
		want string // empty means expect ErrMACNotFound
	}{
		{
			name: "macos_full_form",
			ip:   "192.168.0.42",
			raw:  "tv.local (192.168.0.42) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]",
			want: "aa:bb:cc:dd:ee:ff",
		},
		{
			name: "macos_short_octets",
			ip:   "192.168.0.42",
			raw:  "? (192.168.0.42) at a:b:c:d:e:f on en0 ifscope [ethernet]",
			want: "0a:0b:0c:0d:0e:0f",
		},
		{
			name: "linux_arp_n",
			ip:   "192.168.1.10",
			raw: `Address                  HWtype  HWaddress           Flags Mask            Iface
192.168.1.10             ether   12:34:56:78:9a:bc   C                     wlan0`,
			want: "12:34:56:78:9a:bc",
		},
		{
			name: "linux_incomplete_entry",
			ip:   "192.168.1.99",
			raw: `Address                  HWtype  HWaddress           Flags Mask            Iface
192.168.1.99                     (incomplete)                              wlan0`,
			want: "",
		},
		{
			name: "windows_arp_a",
			ip:   "192.168.1.10",
			raw: `Interface: 192.168.1.5 --- 0xb
  Internet Address      Physical Address      Type
  192.168.1.10          aa-bb-cc-dd-ee-ff     dynamic`,
			want: "aa:bb:cc:dd:ee:ff",
		},
		{
			name: "no_match_for_different_ip",
			ip:   "192.168.0.99",
			raw:  "tv.local (192.168.0.42) at aa:bb:cc:dd:ee:ff on en0",
			want: "",
		},
		{
			name: "rejects_all_zero",
			ip:   "192.168.0.42",
			raw:  "tv.local (192.168.0.42) at 00:00:00:00:00:00 on en0",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mac, err := parseARPOutput(tc.raw, tc.ip)
			if tc.want == "" {
				if !errors.Is(err, ErrMACNotFound) {
					t.Fatalf("err=%v, want ErrMACNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			want, err := net.ParseMAC(tc.want)
			if err != nil {
				t.Fatalf("test fixture parse: %v", err)
			}
			if mac.String() != want.String() {
				t.Fatalf("mac=%s, want %s", mac, want)
			}
		})
	}
}

func TestLookupMAC_RejectsInvalidIP(t *testing.T) {
	if _, err := LookupMAC(context.Background(), "not-an-ip"); err == nil {
		t.Fatal("expected error for invalid IP")
	}
}
