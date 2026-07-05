package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func load(t *testing.T, dir string) *Registry {
	t.Helper()
	t.Setenv("COUCH_CONFIG_HOME", dir)
	r, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return r
}

func TestLoad_MissingFileIsFirstRun(t *testing.T) {
	dir := t.TempDir()
	r := load(t, dir)
	if r.Path() != filepath.Join(dir, "devices.json") {
		t.Errorf("path=%q", r.Path())
	}
	if r.Version != Version {
		t.Errorf("version=%d", r.Version)
	}
	if len(r.Devices) != 0 {
		t.Errorf("expected empty devices map")
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := load(t, dir)
	r.Default = "lr"
	settings := json.RawMessage(`{"port":1926,"secure":true,"device_id":"abc","auth_key":"key"}`)
	r.Put("lr", &Device{
		Driver:     "philips",
		Address:    "192.168.0.10",
		MAC:        "aa:bb:cc:dd:ee:ff",
		HardwareID: "serial-1",
		Label:      "Living Room TV",
		Room:       "living-room",
		PairedAt:   time.Date(2026, 7, 5, 18, 30, 0, 0, time.UTC),
		Settings:   settings,
	})
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := load(t, dir)
	if got.Default != "lr" {
		t.Errorf("Default=%q", got.Default)
	}
	d, ok := got.Get("lr")
	if !ok {
		t.Fatal("missing entry lr")
	}
	if d.Driver != "philips" || d.Room != "living-room" || d.HardwareID != "serial-1" {
		t.Errorf("core fields mismatch: %+v", d)
	}
	var blob map[string]any
	if err := json.Unmarshal(d.Settings, &blob); err != nil {
		t.Fatalf("settings blob: %v", err)
	}
	if blob["auth_key"] != "key" {
		t.Errorf("settings round-trip mismatch: %v", blob)
	}
}

func TestLoad_UnknownFieldsSurvive(t *testing.T) {
	dir := t.TempDir()
	doc := `{
  "version": 1,
  "devices": {
    "x": {"driver": "sonos", "address": "10.0.0.9", "settings": {"future": true}}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	r := load(t, dir)
	d, ok := r.Get("x")
	if !ok {
		t.Fatal("missing entry")
	}
	if string(d.Driver) != "sonos" {
		t.Errorf("unknown driver not preserved: %q", d.Driver)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := load(t, dir)
	d, _ = got.Get("x")
	if string(d.Settings) == "" || string(d.Driver) != "sonos" {
		t.Errorf("unknown driver entry did not round-trip: %+v", d)
	}
}

func TestSave_Mode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits not meaningful on Windows")
	}
	dir := t.TempDir()
	r := load(t, dir)
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st, err := os.Stat(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode=%o want 600", mode)
	}
}

func TestAliases_Sorted(t *testing.T) {
	r := &Registry{Devices: map[string]*Device{}}
	r.Put("zeta", &Device{})
	r.Put("alpha", &Device{})
	got := r.Aliases()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "zeta" {
		t.Errorf("Aliases()=%v", got)
	}
}

func TestFindByAddress(t *testing.T) {
	r := &Registry{Devices: map[string]*Device{}}
	r.Put("alpha", &Device{Address: "10.0.0.1"})
	r.Put("beta", &Device{Address: "10.0.0.2"})
	if a := r.FindByAddress("10.0.0.2"); a != "beta" {
		t.Errorf("FindByAddress=%q", a)
	}
	if a := r.FindByAddress("nope"); a != "" {
		t.Errorf("expected empty, got %q", a)
	}
}
