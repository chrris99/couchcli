package philips

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// SystemService handles JointSpace /system endpoints — discovery probing
// across the v6/v5/v1 API versions.
type SystemService service

// ErrNoTVAtHost is returned by DetectEndpoint when neither HTTPS:1926 nor
// HTTP:1925 returns a valid JointSpace /system response.
var ErrNoTVAtHost = errors.New("no JointSpace endpoint found")

// SystemInfo is the subset of /system fields we care about plus the raw body.
type SystemInfo struct {
	Name             string         `json:"name"`
	Model            string         `json:"model"`
	SerialNumber     string         `json:"serial_number"`
	SoftwareVersion  string         `json:"softwareversion"`
	OSType           string         `json:"os_type"`
	Country          string         `json:"country"`
	APIVersionMajor  int            `json:"-"`
	APIVersionMinor  int            `json:"-"`
	PairingType      string         `json:"-"` // "digest_auth_pairing" if needs pairing
	SecuredTransport bool           `json:"-"`
	Raw              map[string]any `json:"-"`
}

// Probe tries /6/system, /5/system, /1/system on this client and returns the
// first valid response. Returns ErrNoTVAtHost if none respond with JSON
// containing api_version.
func (s *SystemService) Probe(ctx context.Context) (SystemInfo, error) {
	c := s.client
	for _, api := range []string{"/6/system", "/5/system", "/1/system"} {
		raw := map[string]any{}
		// Use a short per-attempt context.
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := c.Do(attemptCtx, http.MethodGet, api, nil, &raw)
		cancel()
		if err != nil {
			continue
		}
		if !hasAPIVersion(raw) {
			continue
		}
		return parseSystem(raw), nil
	}
	return SystemInfo{}, ErrNoTVAtHost
}

// DetectEndpoint races HTTPS:1926 and HTTP:1925 in parallel and returns the
// first working Client + SystemInfo. Both attempts use a 2.5s ceiling.
func DetectEndpoint(ctx context.Context, host string) (*Client, SystemInfo, error) {
	type result struct {
		client *Client
		info   SystemInfo
		err    error
	}

	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	results := make(chan result, 2)
	var wg sync.WaitGroup

	for _, c := range []*Client{NewSecure(host), NewPlain(host)} {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			info, err := client.System.Probe(probeCtx)
			results <- result{client: client, info: info, err: err}
		}(c)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var lastErr error
	// Prefer the secure endpoint if both succeed: drain until we find one
	// success, then keep draining quickly for a HTTPS upgrade if available.
	var winner *result
	for r := range results {
		if r.err != nil {
			lastErr = r.err
			continue
		}
		if winner == nil {
			rr := r
			winner = &rr
			continue
		}
		// Prefer secure over plain.
		if r.client.Secure() && !winner.client.Secure() {
			rr := r
			winner = &rr
		}
	}
	if winner != nil {
		return winner.client, winner.info, nil
	}
	if lastErr != nil {
		return nil, SystemInfo{}, fmt.Errorf("%w: %v", ErrNoTVAtHost, lastErr)
	}
	return nil, SystemInfo{}, ErrNoTVAtHost
}

// hasAPIVersion reports whether the raw response carries an api_version object
// or top-level api_version_major/minor.
func hasAPIVersion(raw map[string]any) bool {
	if _, ok := raw["api_version"]; ok {
		return true
	}
	_, hasMajor := raw["api_version_major"]
	return hasMajor
}

func parseSystem(raw map[string]any) SystemInfo {
	info := SystemInfo{Raw: raw}

	info.Name = strFrom(raw, "name")
	info.Model = strFrom(raw, "model")
	info.SerialNumber = strFrom(raw, "serialnumber_encrypted")
	if info.SerialNumber == "" {
		info.SerialNumber = strFrom(raw, "serialnumber")
	}
	info.SoftwareVersion = strFrom(raw, "softwareversion_encrypted")
	if info.SoftwareVersion == "" {
		info.SoftwareVersion = strFrom(raw, "softwareversion")
	}
	info.OSType = strFrom(raw, "os_type")
	info.Country = strFrom(raw, "country")

	// api_version may be either nested or flat.
	if nested, ok := raw["api_version"].(map[string]any); ok {
		info.APIVersionMajor = intFrom(nested, "Major")
		if info.APIVersionMajor == 0 {
			info.APIVersionMajor = intFrom(nested, "major")
		}
		info.APIVersionMinor = intFrom(nested, "Minor")
		if info.APIVersionMinor == 0 {
			info.APIVersionMinor = intFrom(nested, "minor")
		}
	} else {
		info.APIVersionMajor = intFrom(raw, "api_version_major")
		info.APIVersionMinor = intFrom(raw, "api_version_minor")
	}

	// featuring.systemfeatures.pairing_type — varies by firmware. Try a couple
	// of shapes.
	info.PairingType = extractPairingType(raw)
	// Some firmwares advertise this directly. Also infer from os_type+API>=6.
	if v, ok := raw["secured_transport"].(bool); ok {
		info.SecuredTransport = v
	} else {
		// Heuristic: API v6+ on Android always requires HTTPS+digest.
		if info.APIVersionMajor >= 6 && info.OSType != "" {
			info.SecuredTransport = true
		}
	}

	return info
}

func extractPairingType(raw map[string]any) string {
	feat, ok := raw["featuring"].(map[string]any)
	if !ok {
		return ""
	}
	sf, ok := feat["systemfeatures"].(map[string]any)
	if !ok {
		return ""
	}
	return strFrom(sf, "pairing_type")
}

func strFrom(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func intFrom(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}
