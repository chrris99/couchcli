package philips

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsOn_TrueWhenPowerstateOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/powerstate" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"powerstate":"On"}`))
	}))
	defer srv.Close()

	if !newTestClient(t, srv).IsOn(context.Background(), 1*time.Second) {
		t.Fatal("IsOn=false, want true")
	}
}

func TestIsOn_FalseWhenPowerstateStandby(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"powerstate":"Standby"}`))
	}))
	defer srv.Close()

	if newTestClient(t, srv).IsOn(context.Background(), 1*time.Second) {
		t.Fatal("IsOn=true, want false (standby)")
	}
}

func TestIsOn_FallbackToReachableOn404(t *testing.T) {
	// /6/powerstate is unsupported but /6/system answers — IsOn should
	// fall back to IsReachable and return true.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/6/powerstate":
			http.Error(w, "not found", http.StatusNotFound)
		case "/6/system":
			_, _ = w.Write([]byte(`{"api_version":{"Major":6}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if !newTestClient(t, srv).IsOn(context.Background(), 1*time.Second) {
		t.Fatal("IsOn=false, want true (fallback to IsReachable)")
	}
}

func TestIsOn_FalseOnUnreachable(t *testing.T) {
	// Server is closed — connection refused for both endpoints.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c := newTestClient(t, srv)
	srv.Close()

	if c.IsOn(context.Background(), 500*time.Millisecond) {
		t.Fatal("IsOn=true, want false on unreachable")
	}
}
