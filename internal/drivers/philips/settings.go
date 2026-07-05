package philips

import (
	"encoding/json"
	"fmt"
)

// Settings is this driver's blob in a registry entry: JointSpace endpoint
// details, digest credentials, and pair-time system metadata.
type Settings struct {
	Port     int    `json:"port,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	AuthKey  string `json:"auth_key,omitempty"`

	Name            string `json:"name,omitempty"`
	Model           string `json:"model,omitempty"`
	SerialNumber    string `json:"serial_number,omitempty"`
	SoftwareVersion string `json:"software_version,omitempty"`
	APIVersion      int    `json:"api_version,omitempty"`
}

// ParseSettings decodes a registry entry's Settings blob. An empty blob is
// valid and yields zero Settings.
func ParseSettings(raw json.RawMessage) (Settings, error) {
	if len(raw) == 0 {
		return Settings{}, nil
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, fmt.Errorf("philips settings: %w", err)
	}
	return s, nil
}

// Encode marshals the settings for storage in a registry entry.
func (s Settings) Encode() (json.RawMessage, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("philips settings: %w", err)
	}
	return b, nil
}
