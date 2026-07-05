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
package jointspace

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
	"time"

	"github.com/chrris99/couchcli/internal/httpauth"
)

type Client struct {
	baseURL *url.URL

	http *http.Client

	common service

	System    *SystemService
	Pair      *PairService
	Volume    *VolumeService
	Keys      *KeysService
	Power     *PowerService
	Ambilight *AmbilightService
}

type service struct {
	client *Client
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

// HTTPError is returned for any non-2xx response whose body is not a JointSpace
// APIError. It exposes the raw status code so callers can branch on, e.g., 404
// (endpoint not present on this firmware).
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
	c.common.client = c
	c.System = (*SystemService)(&c.common)
	c.Pair = (*PairService)(&c.common)
	c.Volume = (*VolumeService)(&c.common)
	c.Keys = (*KeysService)(&c.common)
	c.Power = (*PowerService)(&c.common)
	c.Ambilight = (*AmbilightService)(&c.common)
	return c
}

// mustParseURL parses a URL we just built ourselves. A panic here means a
// programmer error in the constructor, not bad user input.
func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("jointspace: bad baseURL %q: %v", raw, err))
	}
	return u
}

// WithDigest installs Digest credentials as a transport wrapper, so every
// subsequent request answers a 401 challenge transparently.
// Returns the same client for chaining.
func (c *Client) WithDigest(user, pass string) *Client {
	c.http.Transport = &httpauth.Digest{
		Username: user,
		Password: pass,
		Base:     c.http.Transport,
	}
	return c
}

// Secure reports whether this client is talking over HTTPS (Android TV).
func (c *Client) Secure() bool { return c.baseURL.Scheme == "https" }

// BaseURL returns the scheme://host:port prefix.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// Host returns the host portion (without scheme or port).
func (c *Client) Host() string { return c.baseURL.Hostname() }

// Port returns the port from baseURL, defaulting to the JointSpace standard
// ports when the URL has no explicit port.
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

func (c *Client) doGet(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) doPost(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// do issues a JSON request to path (e.g. "/6/system"). A non-nil body is
// JSON-encoded into the request; a non-nil out receives the decoded response. A
// non-2xx response with a parseable JSON error_id is returned as *APIError,
// otherwise as *HTTPError. Digest auth, when configured, is handled by the
// transport (see WithDigest).
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyBytes = b
	}

	resp, err := c.doOnce(ctx, method, path, bodyBytes)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode/100 != 2 {
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

func (c *Client) doOnce(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL.String()+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return c.http.Do(req)
}
