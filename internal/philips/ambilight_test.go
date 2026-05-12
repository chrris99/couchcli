package philips

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestAmbilight_GetPower(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/power" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"power":"On"}`))
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).Ambilight.GetPower(context.Background())
	if err != nil {
		t.Fatalf("GetPower: %v", err)
	}
	if got != AmbilightPowerOn {
		t.Errorf("got %q want %q", got, AmbilightPowerOn)
	}
}

func TestAmbilight_SetPower_BodyShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/power" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
	}))
	defer srv.Close()

	if err := newTestClient(t, srv).Ambilight.Off(context.Background()); err != nil {
		t.Fatalf("Off: %v", err)
	}
	if captured["power"] != "Off" {
		t.Errorf("body=%+v", captured)
	}
}

func TestAmbilight_GetMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/mode" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"current":"manual"}`))
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).Ambilight.GetMode(context.Background())
	if err != nil {
		t.Fatalf("GetMode: %v", err)
	}
	if got != AmbilightModeManual {
		t.Errorf("got %q want %q", got, AmbilightModeManual)
	}
}

func TestAmbilight_SetMode_BodyShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/mode" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
	}))
	defer srv.Close()

	if err := newTestClient(t, srv).Ambilight.SetMode(context.Background(), AmbilightModeExpert); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if captured["current"] != "expert" {
		t.Errorf("body=%+v", captured)
	}
}

func TestAmbilight_Topology(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/topology" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"layers":1,"left":4,"top":8,"right":4,"bottom":0}`))
	}))
	defer srv.Close()

	top, err := newTestClient(t, srv).Ambilight.Topology(context.Background())
	if err != nil {
		t.Fatalf("Topology: %v", err)
	}
	if top.Layers != 1 || top.Left != 4 || top.Top != 8 || top.Right != 4 || top.Bottom != 0 {
		t.Errorf("topology=%+v", top)
	}
}

func TestAmbilight_SupportedStyles_UnwrapsWrapper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/supportedstyles" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"supportedStyles": []map[string]any{
				{"styleName": "FOLLOW_VIDEO", "algorithm": "PIXEL_DETECT"},
				{"styleName": "OFF"},
			},
		})
	}))
	defer srv.Close()

	styles, err := newTestClient(t, srv).Ambilight.SupportedStyles(context.Background())
	if err != nil {
		t.Fatalf("SupportedStyles: %v", err)
	}
	if len(styles) != 2 {
		t.Fatalf("want 2 styles, got %d (%+v)", len(styles), styles)
	}
	if styles[0].StyleName != AmbilightStyleFollowVideo || styles[0].Algorithm != "PIXEL_DETECT" {
		t.Errorf("styles[0]=%+v", styles[0])
	}
	if styles[1].StyleName != AmbilightStyleOff {
		t.Errorf("styles[1]=%+v", styles[1])
	}
}

func TestAmbilight_SetConfiguration_BodyShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/currentconfiguration" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
	}))
	defer srv.Close()

	cfg := AmbilightConfiguration{
		StyleName:   AmbilightStyleFollowVideo,
		MenuSetting: "STANDARD",
	}
	if err := newTestClient(t, srv).Ambilight.SetConfiguration(context.Background(), cfg); err != nil {
		t.Fatalf("SetConfiguration: %v", err)
	}
	if captured["styleName"] != "FOLLOW_VIDEO" || captured["menuSetting"] != "STANDARD" {
		t.Errorf("body=%+v", captured)
	}
}

func TestAmbilight_Cached_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/cached" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"layer1":{"left":{"0":{"r":255,"g":0,"b":0}}}}`))
		case http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			// Echo the body so we can verify the raw bytes are not wrapped.
			_, _ = w.Write(b)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	got, err := c.Ambilight.Cached(context.Background())
	if err != nil {
		t.Fatalf("Cached: %v", err)
	}
	red, ok := got["layer1"].Left["0"]
	if !ok || red.R != 255 || red.G != 0 || red.B != 0 {
		t.Errorf("decoded=%+v", got)
	}

	out := AmbilightColors{"layer1": AmbilightLayer{Right: map[string]AmbilightColor{"0": {R: 0, G: 0, B: 255}}}}
	if err := c.Ambilight.SetCached(context.Background(), out); err != nil {
		t.Fatalf("SetCached: %v", err)
	}
}

func TestAmbilight_SetSolidColor_OrderAndBody(t *testing.T) {
	var (
		mu    sync.Mutex
		hits  []string
		modeC map[string]any
		cache map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/6/ambilight/topology":
			_, _ = w.Write([]byte(`{"layers":1,"left":2,"top":3,"right":2,"bottom":0}`))
		case "/6/ambilight/mode":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &modeC)
		case "/6/ambilight/cached":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &cache)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	colors, err := newTestClient(t, srv).Ambilight.SetSolidColor(
		context.Background(),
		AmbilightColor{R: 0x10, G: 0x20, B: 0x30},
	)
	if err != nil {
		t.Fatalf("SetSolidColor: %v", err)
	}

	want := []string{
		"GET /6/ambilight/topology",
		"POST /6/ambilight/mode",
		"POST /6/ambilight/cached",
	}
	if len(hits) != len(want) {
		t.Fatalf("hits=%v want %v", hits, want)
	}
	for i := range want {
		if hits[i] != want[i] {
			t.Errorf("hit[%d]=%q want %q", i, hits[i], want[i])
		}
	}

	if modeC["current"] != "manual" {
		t.Errorf("mode body=%+v", modeC)
	}

	// Verify the returned structure matches what the server saw.
	layer1, ok := cache["layer1"].(map[string]any)
	if !ok {
		t.Fatalf("cache body missing layer1: %+v", cache)
	}
	left, ok := layer1["left"].(map[string]any)
	if !ok || len(left) != 2 {
		t.Errorf("left edge wrong count: %+v", left)
	}
	top, ok := layer1["top"].(map[string]any)
	if !ok || len(top) != 3 {
		t.Errorf("top edge wrong count: %+v", top)
	}
	if _, hasBottom := layer1["bottom"]; hasBottom {
		t.Errorf("bottom should be omitted (0 LEDs): %+v", layer1)
	}

	// Spot-check one returned color.
	gotColor := colors["layer1"].Top["1"]
	if gotColor.R != 0x10 || gotColor.G != 0x20 || gotColor.B != 0x30 {
		t.Errorf("returned color wrong: %+v", gotColor)
	}
}

func TestAmbilight_Lounge_RoundTrip(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/ambilight/lounge" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"color":{"hue":120,"saturation":200,"brightness":180},"speed":3}`))
		case http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &captured)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	got, err := c.Ambilight.GetLounge(context.Background())
	if err != nil {
		t.Fatalf("GetLounge: %v", err)
	}
	if got.Color.Hue != 120 || got.Speed != 3 {
		t.Errorf("lounge=%+v", got)
	}

	if err := c.Ambilight.SetLounge(context.Background(), AmbilightLounge{
		Color: AmbilightHSB{Hue: 30, Saturation: 240, Brightness: 200},
		Speed: 5,
	}); err != nil {
		t.Fatalf("SetLounge: %v", err)
	}
	color, ok := captured["color"].(map[string]any)
	if !ok || color["hue"].(float64) != 30 {
		t.Errorf("posted body=%+v", captured)
	}
	if captured["speed"].(float64) != 5 {
		t.Errorf("speed=%+v", captured["speed"])
	}
}
