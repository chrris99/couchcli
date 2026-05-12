package discovery

// SSDP M-SEARCH is hand-rolled here rather than pulled from koron/go-ssdp or
// huin/goupnp. The wire protocol is a small text-based UDP request/response
// plus an HTTP GET of an XML descriptor — about a hundred lines total. Keeping
// it in-tree avoids dragging in a UPnP library we don't otherwise need and
// keeps the binary small.

import (
	"bufio"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ssdpMulticastAddr = "239.255.255.250:1900"
	ssdpDescTimeout   = 2 * time.Second
)

// ssdpSearchTargets is the list of STs we query. Some Philips TVs only respond
// to one of these, so we fire both and dedupe by LOCATION URL.
var ssdpSearchTargets = []string{
	"urn:schemas-upnp-org:device:MediaRenderer:1",
	"upnp:rootdevice",
}

// ssdpScan sends SSDP M-SEARCH multicast queries, fetches each unique LOCATION
// descriptor, filters to Philips-manufactured devices, and emits each match
// as a Device via emit.
func ssdpScan(ctx context.Context, timeout time.Duration, emit func(Device)) {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return
	}
	defer conn.Close()

	mcastAddr, err := net.ResolveUDPAddr("udp4", ssdpMulticastAddr)
	if err != nil {
		return
	}

	// Fire one M-SEARCH per ST. MX=2 hints servers to spread responses over
	// up to 2s; we keep listening for the full timeout window regardless.
	for _, st := range ssdpSearchTargets {
		req := buildMSearch(st, 2)
		_, _ = conn.WriteTo(req, mcastAddr)
	}

	deadline := time.Now().Add(timeout)
	_ = conn.SetReadDeadline(deadline)

	// Collect unique LOCATION URLs first; descriptor fetches happen after the
	// read loop so we don't block on slow HTTP while UDP responses are still
	// arriving.
	locations := map[string]struct{}{}
	buf := make([]byte, 2048)
	for {
		if ctx.Err() != nil {
			break
		}
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				break
			}
			break
		}
		loc := parseSSDPResponseLocation(buf[:n])
		if loc == "" {
			continue
		}
		locations[loc] = struct{}{}
	}

	// Fetch descriptors in parallel; each has its own short timeout so one
	// slow / dead LOCATION can't stall the whole scan.
	client := &http.Client{Timeout: ssdpDescTimeout}
	var wg sync.WaitGroup
	for loc := range locations {
		wg.Add(1)
		go func(loc string) {
			defer wg.Done()
			if dev, ok := fetchPhilipsDescriptor(ctx, client, loc); ok {
				emit(dev)
			}
		}(loc)
	}
	wg.Wait()
}

// buildMSearch constructs an SSDP M-SEARCH request for the given search target.
func buildMSearch(st string, mx int) []byte {
	return []byte(strings.Join([]string{
		"M-SEARCH * HTTP/1.1",
		"HOST: " + ssdpMulticastAddr,
		`MAN: "ssdp:discover"`,
		"MX: " + strconv.Itoa(mx),
		"ST: " + st,
		"",
		"",
	}, "\r\n"))
}

// parseSSDPResponseLocation extracts the LOCATION header value from an SSDP
// HTTP-like response. Returns "" if missing or malformed.
func parseSSDPResponseLocation(data []byte) string {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	// First line is the status line ("HTTP/1.1 200 OK"), skip it.
	if !scanner.Scan() {
		return ""
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "LOCATION") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// rootDesc models the subset of a UPnP root device descriptor XML we care
// about.
type rootDesc struct {
	Device struct {
		FriendlyName string `xml:"friendlyName"`
		Manufacturer string `xml:"manufacturer"`
		ModelName    string `xml:"modelName"`
		UDN          string `xml:"UDN"`
	} `xml:"device"`
}

// fetchPhilipsDescriptor GETs the descriptor at loc, parses it, and returns
// a Device if the manufacturer matches Philips. The second return value is
// false on any failure or for non-Philips devices.
func fetchPhilipsDescriptor(ctx context.Context, client *http.Client, loc string) (Device, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return Device{}, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return Device{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Device{}, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return Device{}, false
	}

	var desc rootDesc
	if err := xml.Unmarshal(body, &desc); err != nil {
		return Device{}, false
	}
	if !isPhilipsManufacturer(desc.Device.Manufacturer) {
		return Device{}, false
	}

	host, port := hostPortFromURL(loc)
	name := desc.Device.FriendlyName
	if strings.TrimSpace(name) == "" {
		name = desc.Device.ModelName
	}
	return Device{
		Name:    strings.TrimSpace(name),
		Address: host,
		Port:    port,
		// SSDP itself is unauthenticated UPnP — the JointSpace endpoint
		// behind it may still be HTTPS:1926 (probe confirms).
		Secure: false,
		Source: SourceSSDPMediaRender,
		TXT: map[string]string{
			"manufacturer": desc.Device.Manufacturer,
			"model":        desc.Device.ModelName,
			"udn":          desc.Device.UDN,
			"location":     loc,
		},
	}, true
}

// isPhilipsManufacturer matches the manufacturer strings real Philips TVs
// advertise: "Royal Philips Electronics", "Philips Consumer Lifestyle", and
// "TP Vision" (the OEM that builds Philips-branded TVs).
func isPhilipsManufacturer(m string) bool {
	lower := strings.ToLower(strings.TrimSpace(m))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "philips") || strings.Contains(lower, "tp vision")
}

func hostPortFromURL(raw string) (string, int) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0
	}
	host := u.Hostname()
	port := 0
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	} else {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = 80
		case "https":
			port = 443
		}
	}
	return host, port
}
