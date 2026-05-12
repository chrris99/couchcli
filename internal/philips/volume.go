package philips

import (
	"context"
	"net/http"
	"time"
)

// VolumeService handles JointSpace audio volume endpoints + step convenience.
type VolumeService service

// Volume is the state returned by GET /6/audio/volume.
type Volume struct {
	Muted   bool `json:"muted"`
	Current int  `json:"current"`
	Min     int  `json:"min"`
	Max     int  `json:"max"`
}

// Get returns the current volume state.
func (s *VolumeService) Get(ctx context.Context) (Volume, error) {
	var v Volume
	if err := s.client.Do(ctx, http.MethodGet, "/6/audio/volume", nil, &v); err != nil {
		return Volume{}, err
	}
	return v, nil
}

// Set writes absolute volume + mute state. current is clamped server-side.
func (s *VolumeService) Set(ctx context.Context, current int, muted bool) error {
	body := struct {
		Muted   bool `json:"muted"`
		Current int  `json:"current"`
	}{Muted: muted, Current: current}
	return s.client.Do(ctx, http.MethodPost, "/6/audio/volume", body, nil)
}

// Up presses the VolumeUp key count times (count<1 is normalized to 1). A
// 120ms gap between keypresses gives the TV time to process each step.
func (s *VolumeService) Up(ctx context.Context, count int) error {
	return s.step(ctx, KeyVolumeUp, count)
}

// Down presses the VolumeDown key count times.
func (s *VolumeService) Down(ctx context.Context, count int) error {
	return s.step(ctx, KeyVolumeDown, count)
}

// Mute toggles the mute state via the Mute key.
func (s *VolumeService) Mute(ctx context.Context) error {
	return s.client.Keys.Send(ctx, KeyMute)
}

func (s *VolumeService) step(ctx context.Context, key string, count int) error {
	if count < 1 {
		count = 1
	}
	for i := 0; i < count; i++ {
		if err := s.client.Keys.Send(ctx, key); err != nil {
			return err
		}
		if i < count-1 {
			time.Sleep(120 * time.Millisecond)
		}
	}
	return nil
}
