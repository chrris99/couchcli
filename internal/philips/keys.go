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
)

// Send simulates a single remote-control keypress on the TV.
func (s *KeysService) Send(ctx context.Context, key string) error {
	return s.client.Do(ctx, http.MethodPost, "/6/input/key", map[string]string{"key": key}, nil)
}
