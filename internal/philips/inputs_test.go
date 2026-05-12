package philips

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInputs_List(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/sources" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hdmi2": map[string]any{"name": "Chromecast"},
			"hdmi1": map[string]any{"name": "HDMI 1"},
			"tv":    map[string]any{"name": "Watch TV"},
		})
	}))
	defer srv.Close()

	inputs, err := newTestClient(t, srv).Inputs.List(context.Background())
	if err != nil {
		t.Fatalf("Inputs.List: %v", err)
	}
	if len(inputs) != 3 {
		t.Fatalf("want 3 inputs, got %d (%+v)", len(inputs), inputs)
	}
	// Sorted by ID via the underlying source map.
	if inputs[0].ID != "hdmi1" || inputs[1].ID != "hdmi2" || inputs[2].ID != "tv" {
		t.Errorf("order=%+v", inputs)
	}
	if inputs[1].Label != "Chromecast" {
		t.Errorf("hdmi2 label=%q", inputs[1].Label)
	}
	if inputs[0].Kind != "hdmi" || inputs[2].Kind != "tv" {
		t.Errorf("kinds=%q,%q", inputs[0].Kind, inputs[2].Kind)
	}
}

func TestInputs_List_EmptySourcesFallsBackToAppsEmpty(t *testing.T) {
	// /6/sources empty + /6/applications 404 → empty result, no error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/6/sources":
			_, _ = w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	inputs, err := newTestClient(t, srv).Inputs.List(context.Background())
	if err != nil {
		t.Fatalf("Inputs.List: %v", err)
	}
	if len(inputs) != 0 {
		t.Errorf("want empty slice, got %+v", inputs)
	}
}

func TestInputs_Current(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/6/sources/current":
			_, _ = w.Write([]byte(`{"id":"hdmi2"}`))
		case "/6/sources":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hdmi2": map[string]any{"name": "Chromecast"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	in, err := newTestClient(t, srv).Inputs.Current(context.Background())
	if err != nil {
		t.Fatalf("Inputs.Current: %v", err)
	}
	if in.ID != "hdmi2" || in.Label != "Chromecast" {
		t.Errorf("Current=%+v", in)
	}
}

func TestInputs_Switch_PostsSourceBody(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/sources/current" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Inputs.Switch(context.Background(), Input{ID: "hdmi1"}); err != nil {
		t.Fatalf("Inputs.Switch: %v", err)
	}
	if captured["id"] != "hdmi1" {
		t.Errorf("captured=%+v", captured)
	}
}

func TestInputs_Switch_LaunchesIntentForAndroidHDMI(t *testing.T) {
	var hit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		hit = r.URL.Path
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	in := Input{
		ID:    "hdmi1",
		Label: "HDMI 1",
		Kind:  "hdmi",
		Intent: Intent{
			Component: Component{
				PackageName: "org.droidtv.playtv",
				ClassName:   "org.droidtv.playtv.PlayTvActivity",
			},
		},
	}
	if err := c.Inputs.Switch(context.Background(), in); err != nil {
		t.Fatalf("Inputs.Switch: %v", err)
	}
	if hit != "/6/activities/launch" {
		t.Errorf("expected intent-launch path, got %q", hit)
	}
}
