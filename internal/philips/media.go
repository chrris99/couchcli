package philips

import "context"

// MediaService dispatches transport keys to whatever app is in front. Whether
// a given key takes effect depends on the active app — Toggle (PlayPause) is
// the most universally honoured.
type MediaService service

// Play sends the Play key.
func (s *MediaService) Play(ctx context.Context) error { return s.client.Keys.Send(ctx, KeyPlay) }

// Pause sends the Pause key.
func (s *MediaService) Pause(ctx context.Context) error { return s.client.Keys.Send(ctx, KeyPause) }

// Toggle sends the PlayPause key — universal fallback.
func (s *MediaService) Toggle(ctx context.Context) error {
	return s.client.Keys.Send(ctx, KeyPlayPause)
}

// Stop sends the Stop key.
func (s *MediaService) Stop(ctx context.Context) error { return s.client.Keys.Send(ctx, KeyStop) }

// Next skips to the next item.
func (s *MediaService) Next(ctx context.Context) error { return s.client.Keys.Send(ctx, KeyNext) }

// Previous skips to the previous item.
func (s *MediaService) Previous(ctx context.Context) error {
	return s.client.Keys.Send(ctx, KeyPrevious)
}

// FastForward sends the FastForward key.
func (s *MediaService) FastForward(ctx context.Context) error {
	return s.client.Keys.Send(ctx, KeyFastForward)
}

// Rewind sends the Rewind key.
func (s *MediaService) Rewind(ctx context.Context) error { return s.client.Keys.Send(ctx, KeyRewind) }
