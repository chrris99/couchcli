package jointspace

import (
	"context"
)

// KeysService handles single-key remote presses.
type KeysService service

type Key string

const (
	KeyVolumeUp    Key = "VolumeUp"
	KeyVolumeDown  Key = "VolumeDown"
	KeyMute        Key = "Mute"
	KeyPlay        Key = "Play"
	KeyPause       Key = "Pause"
	KeyPlayPause   Key = "PlayPause"
	KeyStop        Key = "Stop"
	KeyFastForward Key = "FastForward"
	KeyRewind      Key = "Rewind"
	KeyNext        Key = "Next"
	KeyPrevious    Key = "Previous"
	KeyHome        Key = "Home"
	KeySource      Key = "Source"
	KeyStandby     Key = "Standby"
)

// Send simulates a single remote-control keypress on the TV.
func (s *KeysService) Send(ctx context.Context, key Key) error {
	body := struct {
		Key Key `json:"key"`
	}{Key: key}
	return s.client.doPost(ctx, "/6/input/key", body, nil)
}
