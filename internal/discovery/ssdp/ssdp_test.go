package ssdp

import (
	"strings"
	"testing"
)

func TestParseResponse(t *testing.T) {
	valid := strings.Join([]string{
		"HTTP/1.1 200 OK",
		"CACHE-CONTROL: max-age=1800",
		"LOCATION: http://10.0.0.5:8080/description.xml",
		"SERVER: Linux UPnP/1.0 Sonos/70.4",
		"ST: upnp:rootdevice",
		"USN: uuid:RINCON_abc::upnp:rootdevice",
		"",
		"",
	}, "\r\n")

	tests := []struct {
		name string
		data string
		ok   bool
		want Response
	}{
		{
			"valid response", valid, true,
			Response{
				Location: "http://10.0.0.5:8080/description.xml",
				ST:       "upnp:rootdevice",
				USN:      "uuid:RINCON_abc::upnp:rootdevice",
				Server:   "Linux UPnP/1.0 Sonos/70.4",
			},
		},
		{"missing location", "HTTP/1.1 200 OK\r\nST: upnp:rootdevice\r\n\r\n", false, Response{}},
		{"non-200", "HTTP/1.1 404 Not Found\r\nLOCATION: http://x/\r\n\r\n", false, Response{}},
		{"not http", "NOTIFY * HTTP/1.1\r\nLOCATION: http://x/\r\n\r\n", false, Response{}},
		{"garbage", "\x00\x01\x02", false, Response{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseResponse([]byte(tt.data))
			if ok != tt.ok {
				t.Fatalf("parseResponse() ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if got.Location != tt.want.Location || got.ST != tt.want.ST || got.USN != tt.want.USN || got.Server != tt.want.Server {
				t.Errorf("parseResponse() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestBuildMSearch(t *testing.T) {
	got := string(buildMSearch("upnp:rootdevice", 2))
	for _, want := range []string{
		"M-SEARCH * HTTP/1.1\r\n",
		"HOST: 239.255.255.250:1900\r\n",
		`MAN: "ssdp:discover"` + "\r\n",
		"MX: 2\r\n",
		"ST: upnp:rootdevice\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildMSearch() missing %q in:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\r\n\r\n") {
		t.Error("buildMSearch() must end with blank line")
	}
}

func TestParseDescription(t *testing.T) {
	xml := `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <friendlyName> 55OLED806/12 </friendlyName>
    <manufacturer>TP Vision</manufacturer>
    <modelName>55OLED806/12</modelName>
    <UDN>uuid:abc-123</UDN>
  </device>
</root>`
	got, err := parseDescription([]byte(xml), "http://10.0.0.5/desc.xml")
	if err != nil {
		t.Fatalf("parseDescription() error: %v", err)
	}
	want := Description{
		FriendlyName: "55OLED806/12",
		Manufacturer: "TP Vision",
		ModelName:    "55OLED806/12",
		UDN:          "uuid:abc-123",
		Location:     "http://10.0.0.5/desc.xml",
	}
	if got != want {
		t.Errorf("parseDescription() = %+v, want %+v", got, want)
	}

	if _, err := parseDescription([]byte("not xml"), "http://x/"); err == nil {
		t.Error("parseDescription() should fail on malformed XML")
	}
}

func TestHostPort(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantHost string
		wantPort int
	}{
		{"explicit port", "http://10.0.0.5:8080/desc.xml", "10.0.0.5", 8080},
		{"default http port", "http://10.0.0.5/desc.xml", "10.0.0.5", 80},
		{"default https port", "https://10.0.0.5/desc.xml", "10.0.0.5", 443},
		{"invalid url", "://bad", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := HostPort(tt.raw)
			if host != tt.wantHost || port != tt.wantPort {
				t.Errorf("HostPort(%q) = (%q, %d), want (%q, %d)", tt.raw, host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}
