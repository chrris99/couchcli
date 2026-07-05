package jointspace

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	return newClient(u, &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
		Timeout:   2 * time.Second,
	})
}

func TestProbe_V6Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/system" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  "65OLED706/12",
			"model": "65OLED706/12",
			"api_version": map[string]any{
				"Major": 6, "Minor": 4, "Patch": 0,
			},
			"os_type": "MSAF_2017_LTS",
			"featuring": map[string]any{
				"systemfeatures": map[string]any{
					"pairing_type": "digest_auth_pairing",
				},
			},
		})
	}))
	defer srv.Close()

	info, err := newTestClient(t, srv).System.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.Name != "65OLED706/12" {
		t.Errorf("Name=%q", info.Name)
	}
	if info.APIVersionMajor != 6 || info.APIVersionMinor != 4 {
		t.Errorf("api=%d.%d", info.APIVersionMajor, info.APIVersionMinor)
	}
	if info.PairingType != "digest_auth_pairing" {
		t.Errorf("PairingType=%q", info.PairingType)
	}
}

func TestProbe_FallsBackToV1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/6/system", "/5/system":
			http.Error(w, "not found", http.StatusNotFound)
		case "/1/system":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":              "Legacy TV",
				"api_version_major": 1,
				"api_version_minor": 0,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	info, err := newTestClient(t, srv).System.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.APIVersionMajor != 1 {
		t.Fatalf("expected API v1, got %d", info.APIVersionMajor)
	}
	if info.Name != "Legacy TV" {
		t.Errorf("Name=%q", info.Name)
	}
}

func TestProbe_NoEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv).System.Probe(context.Background()); err == nil {
		t.Fatal("expected ErrNoTVAtHost")
	}
}

func TestIsReachable_TrueOn200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/system" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"api_version":{"Major":6}}`))
	}))
	defer srv.Close()

	if !newTestClient(t, srv).System.IsReachable(context.Background(), 2*time.Second) {
		t.Fatal("IsReachable=false, want true")
	}
}

func TestIsReachable_FalseOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if newTestClient(t, srv).System.IsReachable(context.Background(), 30*time.Millisecond) {
		t.Fatal("IsReachable=true, want false on timeout")
	}
}

func TestIsReachable_FalseOnConnRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c := newTestClient(t, srv)
	srv.Close()

	if c.System.IsReachable(context.Background(), 500*time.Millisecond) {
		t.Fatal("IsReachable=true, want false on connection refused")
	}
}
