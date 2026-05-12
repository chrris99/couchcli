package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPath_EnvOverride(t *testing.T) {
	t.Setenv("COUCH_CONFIG_HOME", "/tmp/couch-test")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/couch-test/devices.json" {
		t.Errorf("got %q", got)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")

	s := New()
	s.Default = "lr"
	s.Put("lr", &Device{
		Address:    "192.168.0.10",
		Port:       1926,
		Secure:     true,
		DeviceID:   "abc",
		AuthKey:    "key",
		Name:       "Living Room",
		PairedAt:   time.Date(2026, 5, 11, 18, 30, 0, 0, time.UTC),
		APIVersion: 6,
	})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv("COUCH_CONFIG_HOME", dir)
	got, gotPath, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if gotPath != path {
		t.Errorf("path=%q want %q", gotPath, path)
	}
	if got.Default != "lr" {
		t.Errorf("Default=%q", got.Default)
	}
	d, ok := got.Get("lr")
	if !ok {
		t.Fatal("missing entry lr")
	}
	if d.DeviceID != "abc" || d.AuthKey != "key" {
		t.Errorf("device round-trip mismatch: %+v", d)
	}
}

func TestLoad_LegacyMigration_CredentialsMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	legacy := map[string]any{
		"version": 1,
		"default": "old",
		"devices": map[string]any{
			"old": map[string]any{
				"address": "10.0.0.99",
				"port":    1926,
				"secure":  true,
				"kind":    "philips",
				"credentials": map[string]string{
					"device_id": "legacy-id",
					"auth_key":  "legacy-key",
				},
			},
		},
	}
	b, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COUCH_CONFIG_HOME", dir)
	s, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := s.Get("old")
	if !ok {
		t.Fatal("missing legacy entry")
	}
	if d.DeviceID != "legacy-id" || d.AuthKey != "legacy-key" {
		t.Errorf("migration didn't populate creds: %+v", d)
	}
	if d.LegacyKind != "" || d.LegacyCredentials != nil {
		t.Errorf("legacy fields not cleared: %+v", d)
	}
}

func TestLoad_LegacyMigration_FlatFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	legacy := map[string]any{
		"version": 1,
		"default": "old",
		"devices": map[string]any{
			"old": map[string]any{
				"address":   "10.0.0.99",
				"port":      1926,
				"secure":    true,
				"device_id": "flat-id",
				"auth_key":  "flat-key",
			},
		},
	}
	b, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COUCH_CONFIG_HOME", dir)
	s, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	d, _ := s.Get("old")
	if d.DeviceID != "flat-id" || d.AuthKey != "flat-key" {
		t.Errorf("flat fields not read: %+v", d)
	}
}

func TestSave_Mode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits not meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	s := New()
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode=%o want 600", mode)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COUCH_CONFIG_HOME", dir)
	s, path, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if path != filepath.Join(dir, "devices.json") {
		t.Errorf("path=%q", path)
	}
	if s.Version != CurrentSchemaVersion {
		t.Errorf("version=%d", s.Version)
	}
	if len(s.Devices) != 0 {
		t.Errorf("expected empty devices map")
	}
}

func TestFindByAddress(t *testing.T) {
	s := New()
	s.Put("alpha", &Device{Address: "10.0.0.1"})
	s.Put("beta", &Device{Address: "10.0.0.2"})
	if a := s.FindByAddress("10.0.0.2"); a != "beta" {
		t.Errorf("FindByAddress=%q", a)
	}
	if a := s.FindByAddress("nope"); a != "" {
		t.Errorf("expected empty, got %q", a)
	}
}
