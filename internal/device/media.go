package device

import "context"

// MediaAction is a transport command sent to whatever is currently playing on a
// device. Support varies per device and per active app; an action a device does
// not honour yields ErrUnsupported.
type MediaAction string

const (
	MediaPlay        MediaAction = "play"
	MediaPause       MediaAction = "pause"
	MediaPlayPause   MediaAction = "play_pause"
	MediaStop        MediaAction = "stop"
	MediaNext        MediaAction = "next"
	MediaPrevious    MediaAction = "previous"
	MediaFastForward MediaAction = "fast_forward"
	MediaRewind      MediaAction = "rewind"
)

// MediaController is implemented by devices that accept media transport
// commands. One method keyed by MediaAction, rather than a method per verb,
// because a device typically supports only a subset — the unsupported ones
// return ErrUnsupported at call time.
type MediaController interface {
	Media(ctx context.Context, action MediaAction) error
}
