package philips

import (
	"context"
	"errors"
	"net/http"
)

// PowerService handles JointSpace /6/powerstate — the canonical power-control
// endpoint on Android Philips TVs. Unlike /6/input/key Standby (a toggle),
// /6/powerstate exposes explicit Get and Set semantics so we can branch on
// the current state.
type PowerService service

// Power state values returned by /6/powerstate.
const (
	PowerOn      = "On"
	PowerStandby = "Standby"
)

// ErrPowerstateUnsupported is returned when the firmware does not expose
// /6/powerstate (older non-Android sets / API < 6). Callers should fall
// back to the Standby key.
var ErrPowerstateUnsupported = errors.New("philips: /6/powerstate not supported")

type powerstatePayload struct {
	Powerstate string `json:"powerstate"`
}

// Get returns the current power state ("On" or "Standby"). Returns
// ErrPowerstateUnsupported on a 404 so callers can fall back to Keys.Standby
// without inspecting the underlying *HTTPError.
func (s *PowerService) Get(ctx context.Context) (string, error) {
	var out powerstatePayload
	if err := s.client.Do(ctx, http.MethodGet, "/6/powerstate", nil, &out); err != nil {
		if isHTTP404(err) {
			return "", ErrPowerstateUnsupported
		}
		return "", err
	}
	return out.Powerstate, nil
}

// Set posts the desired power state to /6/powerstate. Same fallback rule as
// Get — returns ErrPowerstateUnsupported on a 404.
func (s *PowerService) Set(ctx context.Context, state string) error {
	err := s.client.Do(ctx, http.MethodPost, "/6/powerstate", powerstatePayload{Powerstate: state}, nil)
	if err != nil && isHTTP404(err) {
		return ErrPowerstateUnsupported
	}
	return err
}

// On is a convenience wrapper for Set(PowerOn).
func (s *PowerService) On(ctx context.Context) error { return s.Set(ctx, PowerOn) }

// Standby is a convenience wrapper for Set(PowerStandby).
func (s *PowerService) Standby(ctx context.Context) error { return s.Set(ctx, PowerStandby) }

// isHTTP404 reports whether err is an *HTTPError with status 404.
func isHTTP404(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == http.StatusNotFound
}
