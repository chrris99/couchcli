package jointspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SystemService handles the /system endpoints — discovery probing across the
// v6/v5/v1 API versions.
type SystemService service

// ErrNoTVAtHost is returned by DetectEndpoint when neither HTTPS:1926 nor
// HTTP:1925 returns a valid /system response.
var ErrNoTVAtHost = errors.New("jointspace: no endpoint found")

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
	PairingType      string         `json:"-"` // "digest_auth_pairing" if pairing is needed
	SecuredTransport bool           `json:"-"`
	Raw              map[string]any `json:"-"`
}

// Probe tries /6/system, /5/system, /1/system and returns the first response
// that carries api_version. Returns ErrNoTVAtHost if none do.
func (s *SystemService) Probe(ctx context.Context) (SystemInfo, error) {
	for _, api := range []string{"/6/system", "/5/system", "/1/system"} {
		raw := map[string]any{}
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := s.client.doGet(attemptCtx, api, &raw)
		cancel()
		if err != nil || !hasAPIVersion(raw) {
			continue
		}
		return parseSystem(raw), nil
	}
	return SystemInfo{}, ErrNoTVAtHost
}

// IsReachable reports whether the API answers a cheap /6/system request within
// timeout. On Android TVs the API stays up in soft standby, so IsReachable=true
// does NOT mean the panel is on — use PowerService.Get for that. A refused or
// timed-out connection returns false, which is the signal that WoL is needed.
func (s *SystemService) IsReachable(ctx context.Context, timeout time.Duration) bool {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var sink map[string]any
	return s.client.doGet(probeCtx, "/6/system", &sink) == nil
}

// DetectEndpoint races HTTPS:1926 and HTTP:1925 in parallel and returns the
// first working Client + SystemInfo, preferring the secure endpoint if both
// answer. Both attempts share a 3s ceiling.
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
	for _, client := range []*Client{NewSecure(host), NewPlain(host)} {
		wg.Go(func() {
			info, err := client.System.Probe(probeCtx)
			results <- result{client: client, info: info, err: err}
		})
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var lastErr error
	var winner *result
	for r := range results {
		if r.err != nil {
			lastErr = r.err
			continue
		}
		if winner == nil || (r.client.Secure() && !winner.client.Secure()) {
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

// hasAPIVersion reports whether raw carries an api_version object or a
// top-level api_version_major.
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

	// api_version may be nested or flat depending on firmware.
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

	info.PairingType = extractPairingType(raw)
	if v, ok := raw["secured_transport"].(bool); ok {
		info.SecuredTransport = v
	} else if info.APIVersionMajor >= 6 && info.OSType != "" {
		// API v6+ on Android always requires HTTPS+digest.
		info.SecuredTransport = true
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
