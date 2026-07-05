package mdns

import (
	"net"
	"reflect"
	"testing"

	hmdns "github.com/hashicorp/mdns"
)

func TestInstanceName(t *testing.T) {
	tests := []struct {
		name    string
		full    string
		service string
		want    string
	}{
		{"full suffix", "Living Room TV._philipstv_s_rpc._tcp.local.", "_philipstv_s_rpc._tcp", "Living Room TV"},
		{"unrelated name", "tv-hostname.other._tcp.local.", "_philipstv_s_rpc._tcp", "tv-hostname"},
		{"no dots", "bare", "_philipstv_s_rpc._tcp", "bare"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := instanceName(tt.full, tt.service); got != tt.want {
				t.Errorf("instanceName(%q, %q) = %q, want %q", tt.full, tt.service, got, tt.want)
			}
		})
	}
}

func TestPickAddr(t *testing.T) {
	tests := []struct {
		name  string
		entry *hmdns.ServiceEntry
		want  string
	}{
		{"v4 preferred", &hmdns.ServiceEntry{AddrV4: net.IPv4(192, 168, 1, 10), AddrV6: net.ParseIP("fe80::1")}, "192.168.1.10"},
		{"v6 fallback", &hmdns.ServiceEntry{AddrV6: net.ParseIP("fe80::1")}, "fe80::1"},
		{"none", &hmdns.ServiceEntry{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickAddr(tt.entry)
			if tt.want == "" {
				if got.IsValid() {
					t.Errorf("pickAddr() = %v, want zero addr", got)
				}
				return
			}
			if got.String() != tt.want {
				t.Errorf("pickAddr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTXT(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		want   map[string]string
	}{
		{"empty", nil, nil},
		{"pairs and bare keys", []string{"model=55PUS8807", "secure"}, map[string]string{"model": "55PUS8807", "secure": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTXT(tt.fields); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTXT(%v) = %v, want %v", tt.fields, got, tt.want)
			}
		})
	}
}

func TestFromServiceEntry(t *testing.T) {
	raw := &hmdns.ServiceEntry{
		Name:       "TV._philipstv_s_rpc._tcp.local.",
		Host:       "philips-tv.local.",
		AddrV4:     net.IPv4(10, 0, 0, 5),
		Port:       1926,
		InfoFields: []string{"model=OLED806"},
	}
	got := fromServiceEntry(raw, "_philipstv_s_rpc._tcp")
	if got.Addr.String() != "10.0.0.5" {
		t.Errorf("Addr = %v, want 10.0.0.5", got.Addr)
	}
	want := Entry{
		Instance: "TV",
		Service:  "_philipstv_s_rpc._tcp",
		Host:     "philips-tv.local",
		Addr:     got.Addr,
		Port:     1926,
		TXT:      map[string]string{"model": "OLED806"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fromServiceEntry() = %+v, want %+v", got, want)
	}
}
