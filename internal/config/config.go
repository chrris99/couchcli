// Package config persists couchcli's paired-device list to disk in an
// XDG-aware location. The file holds plaintext digest credentials, so it is
// always written with mode 0600.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// CurrentSchemaVersion is the on-disk schema revision. Bumped on
// backwards-incompatible layout changes.
const CurrentSchemaVersion = 1

// Schema is the entire on-disk document.
type Schema struct {
	Version int                `json:"version"`
	Default string             `json:"default,omitempty"`
	Devices map[string]*Device `json:"devices"`
}

// Device is one paired entry. Only Philips JointSpace is supported today, so
// the credentials are stored as flat fields rather than a typed blob.
type Device struct {
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Secure   bool   `json:"secure"`
	DeviceID string `json:"device_id,omitempty"`
	AuthKey  string `json:"auth_key,omitempty"`

	// Label is a user-supplied friendly name (e.g. "Living Room TV"). Set
	// at pair time via --label. Independent of the TV-reported Name.
	Label    string `json:"label,omitempty"`
	Location string `json:"location,omitempty"`

	Name            string    `json:"name,omitempty"`
	Model           string    `json:"model,omitempty"`
	SerialNumber    string    `json:"serial_number,omitempty"`
	SoftwareVersion string    `json:"software_version,omitempty"`
	APIVersion      int       `json:"api_version,omitempty"`
	PairedAt        time.Time `json:"paired_at"`

	// Legacy fields tolerated on read; never written by current code.
	// Older builds wrote a "kind" tag and a credentials map; we fold those
	// into DeviceID/AuthKey on Load.
	LegacyKind        string            `json:"kind,omitempty"`
	LegacyCredentials map[string]string `json:"credentials,omitempty"`
}

// New returns an empty Schema at the current version.
func New() *Schema {
	return &Schema{Version: CurrentSchemaVersion, Devices: map[string]*Device{}}
}

// Path resolves the config file path. Order:
//
//  1. $COUCH_CONFIG_HOME, if set, used as the config directory directly
//  2. macOS:   ~/Library/Application Support/couchcli/devices.json
//  3. Linux:   $XDG_CONFIG_HOME/couchcli/devices.json (default ~/.config)
//  4. Windows: %APPDATA%/couchcli/devices.json
func Path() (string, error) {
	if dir := os.Getenv("COUCH_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "devices.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "couchcli", "devices.json"), nil
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "couchcli", "devices.json"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "couchcli", "devices.json"), nil
	default:
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			return filepath.Join(x, "couchcli", "devices.json"), nil
		}
		return filepath.Join(home, ".config", "couchcli", "devices.json"), nil
	}
}

// Load reads the config from disk. Returns (schema, path, error). If the file
// does not exist, returns an empty schema and the resolved path with nil
// error — first-run behavior.
func Load() (*Schema, string, error) {
	path, err := Path()
	if err != nil {
		return nil, "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return New(), path, nil
		}
		return nil, path, fmt.Errorf("read config: %w", err)
	}
	s := &Schema{}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, path, fmt.Errorf("parse config: %w", err)
	}
	if s.Devices == nil {
		s.Devices = map[string]*Device{}
	}
	if s.Version == 0 {
		s.Version = CurrentSchemaVersion
	}
	for _, d := range s.Devices {
		migrateLegacy(d)
	}
	return s, path, nil
}

// migrateLegacy folds the old kind+credentials-map shape into the current
// DeviceID/AuthKey fields. Idempotent.
func migrateLegacy(d *Device) {
	if d == nil {
		return
	}
	if d.DeviceID == "" {
		d.DeviceID = d.LegacyCredentials["device_id"]
	}
	if d.AuthKey == "" {
		d.AuthKey = d.LegacyCredentials["auth_key"]
	}
	// Wipe legacy fields so subsequent Save() emits the clean shape.
	d.LegacyKind = ""
	d.LegacyCredentials = nil
}

// Save atomically writes the schema to path with mode 0600.
// Creates the parent directory with mode 0700 if missing.
func (s *Schema) Save(path string) error {
	if s.Version == 0 {
		s.Version = CurrentSchemaVersion
	}
	if s.Devices == nil {
		s.Devices = map[string]*Device{}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "devices-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op if we successfully rename

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install config: %w", err)
	}
	// Belt-and-suspenders: ensure final mode is 0600 (rename preserves the
	// temp file's mode, but on some filesystems umask may have intervened).
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod config: %w", err)
	}
	return nil
}

// Put inserts or replaces a device entry under the given alias.
func (s *Schema) Put(alias string, d *Device) {
	if s.Devices == nil {
		s.Devices = map[string]*Device{}
	}
	s.Devices[alias] = d
}

// Get returns the device entry for an alias.
func (s *Schema) Get(alias string) (*Device, bool) {
	d, ok := s.Devices[alias]
	return d, ok
}

// FindByAddress returns the alias of the first entry matching the given
// address, or "" if none.
func (s *Schema) FindByAddress(addr string) string {
	for alias, d := range s.Devices {
		if d.Address == addr {
			return alias
		}
	}
	return ""
}
