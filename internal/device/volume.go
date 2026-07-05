package device

import (
	"context"
	"fmt"
)

// VolumeController is implemented by devices whose audio volume can be read and set.
type VolumeController interface {
	Volume(ctx context.Context) (Volume, error)
	// SetVolume sets the absolute level on the device's scale.
	// Out-of-range levels are clamped by the device.
	SetVolume(ctx context.Context, level int) error
	SetMuted(ctx context.Context, muted bool) error
}

// Volume is a snapshot of a device's audio state. Level is expressed on the
// device's own scale, bounded by [Min, Max] as scales differ per brand, so callers
// that need a normalized value should use Percent.
type Volume struct {
	Level int  `json:"level"`
	Min   int  `json:"min"`
	Max   int  `json:"max"`
	Muted bool `json:"muted"`
}

// Percent returns Level mapped onto 0–100 within [Min, Max]. Returns 0 when
// the range is empty (Max <= Min), which indicates a driver bug or a device
// that doesn't report its scale.
func (v Volume) Percent() int {
	if v.Max <= v.Min {
		return 0
	}
	return (v.Level - v.Min) * 100 / (v.Max - v.Min)
}

// String renders the state for human output, e.g. "12/60" or "12/60 (muted)".
func (v Volume) String() string {
	if v.Muted {
		return fmt.Sprintf("%d/%d (muted)", v.Level, v.Max)
	}
	return fmt.Sprintf("%d/%d", v.Level, v.Max)
}
