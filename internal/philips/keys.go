package philips

import (
	"context"
	"net/http"
)

// KeysService handles JointSpace /6/input/key — single-key remote presses.
type KeysService service

// Key names accepted by /6/input/key.
const (
	KeyVolumeUp   = "VolumeUp"
	KeyVolumeDown = "VolumeDown"
	KeyMute       = "Mute"

	KeyPlay        = "Play"
	KeyPause       = "Pause"
	KeyPlayPause   = "PlayPause"
	KeyStop        = "Stop"
	KeyFastForward = "FastForward"
	KeyRewind      = "Rewind"
	KeyNext        = "Next"
	KeyPrevious    = "Previous"

	KeyHome   = "Home"
	KeySource = "Source"

	// KeyStandby toggles the TV's power state. Modern firmwares should
	// prefer the explicit /6/powerstate endpoint (see PowerService) —
	// Standby is kept as a fallback for sets where /6/powerstate 404s.
	KeyStandby = "Standby"
)

// Send simulates a single remote-control keypress on the TV.
func (s *KeysService) Send(ctx context.Context, key string) error {
	return s.client.Do(ctx, http.MethodPost, "/6/input/key", map[string]string{"key": key}, nil)
}

// Standby sends the Standby keypress as a power toggle. Used as a fallback
// when /6/powerstate is not available; new code should prefer
// PowerService.Standby / PowerService.On.
func (s *KeysService) Standby(ctx context.Context) error {
	return s.Send(ctx, KeyStandby)
}
