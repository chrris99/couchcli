// Package jointspace is a client for the reverse-engineered Philips JointSpace
// HTTP API (v6 + legacy v1/v5). Android Philips TVs (2016+) expose JointSpace
// over HTTPS on port 1926 with a self-signed cert and HTTP Digest auth.
// Legacy non-Android TVs expose it over plain HTTP on port 1925, no auth.
//
// Why InsecureSkipVerify: Philips TVs ship a self-signed certificate that we
// have no way to verify out-of-band. This package's transport sets
// InsecureSkipVerify=true on a *private* http.Transport instance — the default
// transport is untouched. The risk surface is limited to communication with
// the LAN device the user is intentionally pairing with.
package philips

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client talks to a single JointSpace endpoint (one TV). It is composed of
// per-domain Services (System, Pair, Volume, Apps, Inputs, Media, Keys) that
// share the same underlying transport.
type Client struct {
	baseURL *url.URL // parsed once; scheme implies transport (https = secure)

	http *http.Client

	mu   sync.Mutex
	auth *digestAuth // nil until WithDigest is called or a 401 challenge is received

	common service

	System    *SystemService
	Pair      *PairService
	Volume    *VolumeService
	Apps      *AppsService
	Inputs    *InputsService
	Media     *MediaService
	Keys      *KeysService
	Ambilight *AmbilightService
}

type service struct {
	client *Client
}

// wireServices installs all *Service pointers on c. Called by NewSecure /
// NewPlain after the transport is set up, and by test helpers.
func (c *Client) wireServices() {
	c.common.client = c
	c.System = (*SystemService)(&c.common)
	c.Pair = (*PairService)(&c.common)
	c.Volume = (*VolumeService)(&c.common)
	c.Apps = (*AppsService)(&c.common)
	c.Inputs = (*InputsService)(&c.common)
	c.Media = (*MediaService)(&c.common)
	c.Keys = (*KeysService)(&c.common)
	c.Ambilight = (*AmbilightService)(&c.common)
}

// APIError represents a JointSpace JSON error response.
type APIError struct {
	ID   string `json:"error_id"`
	Text string `json:"error_text"`
}

func (e *APIError) Error() string {
	if e.Text != "" {
		return fmt.Sprintf("jointspace: %s (%s)", e.Text, e.ID)
	}
	return fmt.Sprintf("jointspace: %s", e.ID)
}

// HTTPError is returned by Client.Do for any non-2xx response whose body is
// not a JointSpace APIError. It exposes the raw status code so callers can
// branch on, e.g., 404 (endpoint not present on this firmware).
type HTTPError struct {
	Status     int
	StatusText string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("jointspace: %s: %s", e.StatusText, e.Body)
	}
	return fmt.Sprintf("jointspace: %s", e.StatusText)
}

// insecureTransport is a dedicated transport for JointSpace traffic; the
// default http.DefaultTransport is intentionally not modified.
func insecureTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // see package doc
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
}

// NewSecure returns a Client for an Android TV (HTTPS on port 1926).
func NewSecure(host string) *Client {
	return newClient(
		mustParseURL(fmt.Sprintf("https://%s:1926", host)),
		&http.Client{Transport: insecureTransport(), Timeout: 10 * time.Second},
	)
}

// NewPlain returns a Client for a legacy TV (HTTP on port 1925).
func NewPlain(host string) *Client {
	return newClient(
		mustParseURL(fmt.Sprintf("http://%s:1925", host)),
		&http.Client{Timeout: 10 * time.Second},
	)
}

func newClient(baseURL *url.URL, h *http.Client) *Client {
	c := &Client{baseURL: baseURL, http: h}
	c.wireServices()
	return c
}

// mustParseURL parses a URL we just built ourselves. A panic here means a
// programmer error in the constructor, not bad user input.
func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("philips: bad baseURL %q: %v", raw, err))
	}
	return u
}

// Secure reports whether this client is talking over HTTPS (Android TV).
func (c *Client) Secure() bool { return c.baseURL.Scheme == "https" }

// BaseURL returns the scheme://host:port prefix.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// Host returns the host portion (without scheme or port).
func (c *Client) Host() string { return c.baseURL.Hostname() }

// Port returns the port number from the baseURL, defaulting to the
// JointSpace standard ports if the URL has no explicit port.
func (c *Client) Port() int {
	if p := c.baseURL.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0
		}
		return n
	}
	if c.Secure() {
		return 1926
	}
	return 1925
}

// WithDigest attaches credentials so subsequent requests retry with Digest auth
// on a 401 challenge. Returns the same client for chaining.
func (c *Client) WithDigest(user, pass string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.auth = &digestAuth{user: user, pass: pass}
	return c
}

// Do issues a JSON request to path (e.g. "/6/system"). If body is non-nil it is
// JSON-encoded into the request body. If out is non-nil the response body is
// JSON-decoded into it. A non-2xx response with a parseable JSON error_id is
// returned as *APIError; otherwise as a generic error including status text.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyBytes = b
	}

	auth := c.snapshotAuth()
	resp, err := c.doOnce(ctx, method, path, bodyBytes, auth)
	if err != nil {
		return err
	}
	// Digest auth challenge: if we have credentials and got 401, retry once.
	if resp.StatusCode == http.StatusUnauthorized && hasCreds(auth) {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		if err := c.installChallenge(challenge); err != nil {
			return fmt.Errorf("digest challenge: %w", err)
		}
		resp, err = c.doOnce(ctx, method, path, bodyBytes, c.snapshotAuth())
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		// Try to decode an APIError; otherwise fall back to HTTPError.
		if ae := decodeAPIError(respBytes); ae != nil {
			return ae
		}
		return &HTTPError{
			Status:     resp.StatusCode,
			StatusText: resp.Status,
			Body:       strings.TrimSpace(string(respBytes)),
		}
	}

	if out == nil || len(respBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBytes, out); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	return nil
}

func (c *Client) doOnce(ctx context.Context, method, path string, body []byte, auth *digestAuth) (*http.Response, error) {
	fullURL := c.baseURL.String() + path
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, rdr)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	if auth != nil && auth.realm != "" {
		// We've already seen a challenge — pre-authenticate.
		hdr, err := auth.authorize(method, path)
		if err != nil {
			return nil, fmt.Errorf("digest authorize: %w", err)
		}
		req.Header.Set("Authorization", hdr)
	}
	return c.http.Do(req)
}

// snapshotAuth returns the current *digestAuth under a single short-lived
// lock. Callers can read fields on the returned pointer without further
// locking — digestAuth is immutable after parseDigestChallenge, and the nc
// counter is atomic.
func (c *Client) snapshotAuth() *digestAuth {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.auth
}

func hasCreds(a *digestAuth) bool {
	return a != nil && a.user != "" && a.pass != ""
}

// installChallenge parses a WWW-Authenticate challenge and updates the
// existing auth with the realm/nonce/etc. Preserves the user+pass that the
// caller already provided.
func (c *Client) installChallenge(header string) error {
	parsed, err := parseDigestChallenge(header)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.auth == nil {
		return fmt.Errorf("digest challenge without credentials")
	}
	parsed.user = c.auth.user
	parsed.pass = c.auth.pass
	c.auth = parsed
	return nil
}

// decodeAPIError attempts to parse a body as {"error_id":"...","error_text":"..."}.
// Returns nil if the body is not a JSON error.
func decodeAPIError(b []byte) *APIError {
	var ae APIError
	if err := json.Unmarshal(b, &ae); err != nil {
		return nil
	}
	if ae.ID == "" && ae.Text == "" {
		return nil
	}
	return &ae
}
