// Package ssdp is a generic SSDP M-SEARCH scanner. It multicasts search
// requests and returns raw responses.
//
// The wire protocol is hand-rolled rather than pulled from a UPnP library:
// it is a small text-based UDP request/response plus an HTTP GET of an XML
// descriptor, and keeping it in-tree keeps the binary small.
package ssdp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const multicastAddr = "239.255.255.250:1900"

// Response is one SSDP search response.
type Response struct {
	Location string // descriptor URL
	ST       string // search target this response answers
	USN      string // unique service name
	Server   string
	Headers  http.Header    // all headers, for anything not lifted above
	From     netip.AddrPort // responder address
}

// Description is the subset of a UPnP root device descriptor drivers match against.
type Description struct {
	FriendlyName string
	Manufacturer string
	ModelName    string
	UDN          string
	Location     string
}

// Options configures a search.
type Options struct {
	// Targets are the STs to search for. Defaults to ["upnp:rootdevice"].
	Targets []string
	// Timeout is the response listen window. Defaults to 4s.
	// The M-SEARCH MX hint is derived from it, capped at 5 per the UPnP spec.
	Timeout time.Duration
}

// Search multicasts one M-SEARCH per target and collects responses until the
// timeout window closes. Responses are deduplicated by (ST, USN, Location) —
// devices retransmit each response several times.
func Search(ctx context.Context, opts Options) ([]Response, error) {
	targets := opts.Targets
	if len(targets) == 0 {
		targets = []string{"upnp:rootdevice"}
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 4 * time.Second
	}

	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("ssdp: listen: %w", err)
	}
	defer conn.Close()

	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()

	dst, err := net.ResolveUDPAddr("udp4", multicastAddr)
	if err != nil {
		return nil, fmt.Errorf("ssdp: resolve multicast addr: %w", err)
	}
	mx := max(min(int(timeout/time.Second), 5), 1)
	for _, st := range targets {
		if _, err := conn.WriteTo(buildMSearch(st, mx), dst); err != nil {
			return nil, fmt.Errorf("ssdp: send M-SEARCH: %w", err)
		}
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	seen := map[string]Response{}
	buf := make([]byte, 4096)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				break
			}
			return responses(seen), fmt.Errorf("ssdp: read: %w", err)
		}
		resp, ok := parseResponse(buf[:n])
		if !ok {
			continue
		}
		if ua, ok := from.(*net.UDPAddr); ok {
			resp.From = ua.AddrPort()
		}
		seen[resp.ST+"|"+resp.USN+"|"+resp.Location] = *resp
	}
	return responses(seen), ctx.Err()
}

func responses(seen map[string]Response) []Response {
	out := make([]Response, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}
	return out
}

// FetchDescription GETs and parses the UPnP root descriptor at location.
func FetchDescription(ctx context.Context, location string) (Description, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return Description{}, fmt.Errorf("build descriptor request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Description{}, fmt.Errorf("fetch descriptor: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Description{}, fmt.Errorf("fetch descriptor: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return Description{}, fmt.Errorf("read descriptor: %w", err)
	}
	return parseDescription(body, location)
}

// HostPort extracts the host and port from a descriptor Location URL,
// defaulting the port from the scheme (80/443) when the URL omits it.
func HostPort(rawURL string) (host string, port int) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", 0
	}
	host = u.Hostname()
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
		return host, port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		port = 80
	case "https":
		port = 443
	}
	return host, port
}

func buildMSearch(st string, mx int) []byte {
	return []byte(strings.Join([]string{
		"M-SEARCH * HTTP/1.1",
		"HOST: " + multicastAddr,
		`MAN: "ssdp:discover"`,
		"MX: " + strconv.Itoa(mx),
		"ST: " + st,
		"",
		"",
	}, "\r\n"))
}

// parseResponse parses an SSDP HTTP-over-UDP response. Returns ok=false for
// anything that isn't a 200 response with a LOCATION header.
func parseResponse(data []byte) (*Response, bool) {
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(data)), nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, false
	}
	defer resp.Body.Close()
	loc := strings.TrimSpace(resp.Header.Get("Location"))
	if loc == "" {
		return nil, false
	}
	return &Response{
		Location: loc,
		ST:       resp.Header.Get("ST"),
		USN:      resp.Header.Get("USN"),
		Server:   resp.Header.Get("Server"),
		Headers:  resp.Header,
	}, true
}

type rootDesc struct {
	Device struct {
		FriendlyName string `xml:"friendlyName"`
		Manufacturer string `xml:"manufacturer"`
		ModelName    string `xml:"modelName"`
		UDN          string `xml:"UDN"`
	} `xml:"device"`
}

func parseDescription(body []byte, location string) (Description, error) {
	var desc rootDesc
	if err := xml.Unmarshal(body, &desc); err != nil {
		return Description{}, fmt.Errorf("parse descriptor: %w", err)
	}
	return Description{
		FriendlyName: strings.TrimSpace(desc.Device.FriendlyName),
		Manufacturer: strings.TrimSpace(desc.Device.Manufacturer),
		ModelName:    strings.TrimSpace(desc.Device.ModelName),
		UDN:          desc.Device.UDN,
		Location:     location,
	}, nil
}
