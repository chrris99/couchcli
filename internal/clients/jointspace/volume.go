package jointspace

import (
	"context"
	"time"
)

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
	if err := s.client.doGet(ctx, "/6/audio/volume", &v); err != nil {
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
	return s.client.doPost(ctx, "/6/audio/volume", body, nil)
}

// SetLevel sets the absolute volume, preserving the current mute state. The
// /audio/volume POST carries level and mute together, so this reads the present
// state first to leave mute untouched.
func (s *VolumeService) SetLevel(ctx context.Context, level int) error {
	v, err := s.Get(ctx)
	if err != nil {
		return err
	}
	return s.Set(ctx, level, v.Muted)
}

// SetMuted sets the mute state, preserving the current level (same level+mute
// coupling as SetLevel).
func (s *VolumeService) SetMuted(ctx context.Context, muted bool) error {
	v, err := s.Get(ctx)
	if err != nil {
		return err
	}
	return s.Set(ctx, v.Current, muted)
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

func (s *VolumeService) step(ctx context.Context, key Key, count int) error {
	count = max(count, 1)
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
