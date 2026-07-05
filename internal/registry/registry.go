// Package registry persists the paired-device registry (devices.json) in an
// XDG-aware location. Entries hold plaintext credentials inside their
// driver-owned Settings blob, so the file is always written with mode 0600.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/chrris99/couchcli/internal/device"
)

// Version is the on-disk schema revision. Bumped on backwards-incompatible
// layout changes.
const Version = 1

const (
	devicesFileName = "devices.json"
)

// Registry is the on-disk document plus the path it was loaded from.
type Registry struct {
	Version int `json:"version"`
	// Default is the alias commands fall back to when --device is omitted.
	Default string             `json:"default,omitempty"`
	Devices map[string]*Device `json:"devices"`

	path string
}

// Device is one paired entry. Core fields are shared among drivers.
// Anything brand-specific (including credentials) lives in the Settings blob,
// owned and interpreted by the driver.
type Device struct {
	// Driver selects which driver controls this device. Unknown values are
	// preserved so registries from newer builds round-trip; they fail at use
	// time, not load time.
	Driver  device.DriverID `json:"driver"`
	Address string          `json:"address"`
	// MAC enables Wake-on-LAN and, with HardwareID, re-identifying the
	// device when DHCP hands it a new address.
	MAC string `json:"mac,omitempty"`
	// HardwareID is a driver-reported stable identifier (serial, UDN, …).
	HardwareID string    `json:"hardware_id,omitempty"`
	Label      string    `json:"label,omitempty"`
	Room       string    `json:"room,omitempty"`
	PairedAt   time.Time `json:"paired_at"`

	Settings json.RawMessage `json:"settings,omitempty"`
}

// Load reads the registry from its platform path.
func Load() (*Registry, error) {
	path, err := filePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Registry{Version: Version, Devices: map[string]*Device{}, path: path}, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}
	r := &Registry{path: path}
	if err := json.Unmarshal(b, r); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", path, err)
	}
	if r.Devices == nil {
		r.Devices = map[string]*Device{}
	}
	if r.Version == 0 {
		r.Version = Version
	}
	return r, nil
}

// Path returns where this registry is (or will be) stored on disk.
func (r *Registry) Path() string { return r.path }

// Save atomically writes the registry to its path.
func (r *Registry) Save() error {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "devices-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp registry: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after a successful rename

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp registry: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp registry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp registry: %w", err)
	}
	if err := os.Rename(tmpPath, r.path); err != nil {
		return fmt.Errorf("install registry: %w", err)
	}
	// Rename preserves the temp file's mode, but on some filesystems umask
	// may have intervened.
	if err := os.Chmod(r.path, 0o600); err != nil {
		return fmt.Errorf("chmod registry: %w", err)
	}
	return nil
}

// Get returns the entry for alias.
func (r *Registry) Get(alias string) (*Device, bool) {
	d, ok := r.Devices[alias]
	return d, ok
}

// Put inserts or replaces the entry under alias.
func (r *Registry) Put(alias string, d *Device) {
	if r.Devices == nil {
		r.Devices = map[string]*Device{}
	}
	r.Devices[alias] = d
}

// Aliases returns all aliases, sorted.
func (r *Registry) Aliases() []string {
	return slices.Sorted(maps.Keys(r.Devices))
}

// FindByAddress returns the alias of the first entry with the given address,
// or "" if none.
func (r *Registry) FindByAddress(addr string) string {
	for alias, d := range r.Devices {
		if d.Address == addr {
			return alias
		}
	}
	return ""
}

// filePath resolves the on-disk location.
func filePath() (string, error) {
	if dir := os.Getenv("COUCH_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, devicesFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "couchcli", devicesFileName), nil
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "couchcli", devicesFileName), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "couchcli", devicesFileName), nil
	default:
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			return filepath.Join(x, "couchcli", devicesFileName), nil
		}
		return filepath.Join(home, ".config", "couchcli", devicesFileName), nil
	}
}
