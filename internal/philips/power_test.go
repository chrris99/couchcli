package philips

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPower_Get_On(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/6/powerstate" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"powerstate":"On"}`))
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).Power.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != PowerOn {
		t.Errorf("got %q, want %q", got, PowerOn)
	}
}

func TestPower_Get_Standby(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"powerstate":"Standby"}`))
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).Power.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != PowerStandby {
		t.Errorf("got %q, want %q", got, PowerStandby)
	}
}

func TestPower_Get_404_ReturnsUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).Power.Get(context.Background())
	if !errors.Is(err, ErrPowerstateUnsupported) {
		t.Fatalf("got %v, want ErrPowerstateUnsupported", err)
	}
}

func TestPower_Set_On_PostsBody(t *testing.T) {
	var captured powerstatePayload
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
	}))
	defer srv.Close()

	if err := newTestClient(t, srv).Power.On(context.Background()); err != nil {
		t.Fatalf("On: %v", err)
	}
	if method != http.MethodPost || path != "/6/powerstate" {
		t.Errorf("got %s %s, want POST /6/powerstate", method, path)
	}
	if captured.Powerstate != PowerOn {
		t.Errorf("captured powerstate=%q, want %q", captured.Powerstate, PowerOn)
	}
}

func TestPower_Set_Standby_PostsBody(t *testing.T) {
	var captured powerstatePayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
	}))
	defer srv.Close()

	if err := newTestClient(t, srv).Power.Standby(context.Background()); err != nil {
		t.Fatalf("Standby: %v", err)
	}
	if captured.Powerstate != PowerStandby {
		t.Errorf("captured powerstate=%q, want %q", captured.Powerstate, PowerStandby)
	}
}

func TestPower_Set_404_ReturnsUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	err := newTestClient(t, srv).Power.Set(context.Background(), PowerOn)
	if !errors.Is(err, ErrPowerstateUnsupported) {
		t.Fatalf("got %v, want ErrPowerstateUnsupported", err)
	}
}
