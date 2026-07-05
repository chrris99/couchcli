package jointspace

// AmbilightService manipulates the TV's Ambilight LED ring. The TV exposes
// three orthogonal axes:
//
//   - a hard power switch (/power)
//   - a render mode (/mode — internal | manual | expert)
//   - on Android firmware: a higher-level "style" picker
//     (/currentconfiguration — FOLLOW_VIDEO, OFF, LOUNGE, …)
//
// For driving individual LEDs the caller sets mode=manual and POSTs to
// /cached. The /lounge endpoint is the legacy-firmware fallback for the
// solid-color use case — older non-Android TVs don't expose /cached.

import (
	"context"
	"fmt"
	"strconv"
)

// AmbilightService is the sub-service hung off Client.Ambilight.
type AmbilightService service

type AmbilightPower string

const (
	AmbilightPowerOn  AmbilightPower = "On"
	AmbilightPowerOff AmbilightPower = "Off"
)

// GetPower returns the current on/off state of the Ambilight feature.
func (s *AmbilightService) GetPower(ctx context.Context) (AmbilightPower, error) {
	var resp struct {
		Power AmbilightPower `json:"power"`
	}
	if err := s.client.doGet(ctx, "/6/ambilight/power", &resp); err != nil {
		return "", err
	}
	return resp.Power, nil
}

// SetPower toggles Ambilight on or off.
func (s *AmbilightService) SetPower(ctx context.Context, p AmbilightPower) error {
	body := struct {
		Power AmbilightPower `json:"power"`
	}{Power: p}
	return s.client.doPost(ctx, "/6/ambilight/power", body, nil)
}

// On is shorthand for SetPower(AmbilightPowerOn).
func (s *AmbilightService) On(ctx context.Context) error {
	return s.SetPower(ctx, AmbilightPowerOn)
}

// Off is shorthand for SetPower(AmbilightPowerOff).
func (s *AmbilightService) Off(ctx context.Context) error {
	return s.SetPower(ctx, AmbilightPowerOff)
}

type AmbilightMode string

const (
	AmbilightModeInternal AmbilightMode = "internal"
	AmbilightModeManual   AmbilightMode = "manual"
	AmbilightModeExpert   AmbilightMode = "expert"
)

// GetMode reports the current render mode.
func (s *AmbilightService) GetMode(ctx context.Context) (AmbilightMode, error) {
	var resp struct {
		Current AmbilightMode `json:"current"`
	}
	if err := s.client.doGet(ctx, "/6/ambilight/mode", &resp); err != nil {
		return "", err
	}
	return resp.Current, nil
}

// SetMode switches between render modes.
func (s *AmbilightService) SetMode(ctx context.Context, m AmbilightMode) error {
	body := struct {
		Current AmbilightMode `json:"current"`
	}{Current: m}
	return s.client.doPost(ctx, "/6/ambilight/mode", body, nil)
}

type AmbilightTopology struct {
	Layers int `json:"layers"`
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

// Topology returns the LED layout (count per edge). Read once at startup
// and cache — the layout never changes for a given TV.
func (s *AmbilightService) Topology(ctx context.Context) (AmbilightTopology, error) {
	var t AmbilightTopology
	if err := s.client.doGet(ctx, "/6/ambilight/topology", &t); err != nil {
		return AmbilightTopology{}, err
	}
	return t, nil
}

type AmbilightColor struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
}

// AmbilightLayer mirrors one layer's four edges. Maps with string keys
// match the JSON shape ("0", "1", ...) the TV expects.
type AmbilightLayer struct {
	Left   map[string]AmbilightColor `json:"left,omitempty"`
	Top    map[string]AmbilightColor `json:"top,omitempty"`
	Right  map[string]AmbilightColor `json:"right,omitempty"`
	Bottom map[string]AmbilightColor `json:"bottom,omitempty"`
}

type AmbilightColors map[string]AmbilightLayer

// MeasuredColors returns the raw per-LED color the TV currently measures from the
// picture, before smoothing
func (s *AmbilightService) MeasuredColors(ctx context.Context) (AmbilightColors, error) {
	out := AmbilightColors{}
	if err := s.client.doGet(ctx, "/6/ambilight/measured", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ProcessedColors returns the per-LED color after smoothing — what the LEDs are
// actually displaying.
func (s *AmbilightService) ProcessedColors(ctx context.Context) (AmbilightColors, error) {
	out := AmbilightColors{}
	if err := s.client.doGet(ctx, "/6/ambilight/processed", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CachedColors returns the user-supplied color buffer that drives manual/expert
// mode. In internal mode the contents are typically zeroes.
func (s *AmbilightService) CachedColors(ctx context.Context) (AmbilightColors, error) {
	out := AmbilightColors{}
	if err := s.client.doGet(ctx, "/6/ambilight/cached", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetCachedColors writes the per-LED color buffer. The TV ignores this write
// unless mode is manual or expert — see SetSolidColor for the safe wrapper.
func (s *AmbilightService) SetCachedColors(ctx context.Context, c AmbilightColors) error {
	return s.client.doPost(ctx, "/6/ambilight/cached", c, nil)
}

// AmbilightStyleName is one of the style identifiers accepted by
// /6/ambilight/currentconfiguration. Older non-Android firmware does not
// support styles at all (the endpoint 404s).
type AmbilightStyleName string

const (
	AmbilightStyleOff         AmbilightStyleName = "OFF"
	AmbilightStyleFollowVideo AmbilightStyleName = "FOLLOW_VIDEO"
	AmbilightStyleFollowAudio AmbilightStyleName = "FOLLOW_AUDIO"
	AmbilightStyleFollowColor AmbilightStyleName = "FOLLOW_COLOR"
	AmbilightStyleLounge      AmbilightStyleName = "LOUNGE"
	AmbilightStyleManual      AmbilightStyleName = "MANUAL"
	AmbilightStyleExpert      AmbilightStyleName = "EXPERT"
	AmbilightStyleGrid        AmbilightStyleName = "GRID"
)

// AmbilightStyle is one entry from GET /6/ambilight/supportedstyles. The
// wire response wraps these in {"supportedStyles":[…]}.
type AmbilightStyle struct {
	StyleName AmbilightStyleName `json:"styleName"`
	Algorithm string             `json:"algorithm,omitempty"`
}

// SupportedStyles returns the style catalogue the TV advertises. On
// older non-Android firmware the endpoint 404s — callers should treat
// IsNotFound(err) as "no styles available" rather than an error.
func (s *AmbilightService) SupportedStyles(ctx context.Context) ([]AmbilightStyle, error) {
	var resp struct {
		SupportedStyles []AmbilightStyle `json:"supportedStyles"`
	}
	if err := s.client.doGet(ctx, "/6/ambilight/supportedstyles", &resp); err != nil {
		return nil, err
	}
	return resp.SupportedStyles, nil
}

// AmbilightConfiguration mirrors GET/POST /6/ambilight/currentconfiguration.
// MenuSetting is the per-style variant ("STANDARD", "NATURAL", "VIVID", …) —
// the TV validates the value, this package passes it through verbatim.
type AmbilightConfiguration struct {
	StyleName   AmbilightStyleName `json:"styleName"`
	IsExpert    bool               `json:"isExpert,omitempty"`
	MenuSetting string             `json:"menuSetting,omitempty"`
}

// Configuration returns the active style + menu variant.
func (s *AmbilightService) Configuration(ctx context.Context) (AmbilightConfiguration, error) {
	var cfg AmbilightConfiguration
	if err := s.client.doGet(ctx, "/6/ambilight/currentconfiguration", &cfg); err != nil {
		return AmbilightConfiguration{}, err
	}
	return cfg, nil
}

// SetConfiguration switches to a different style (e.g. FOLLOW_VIDEO).
func (s *AmbilightService) SetConfiguration(ctx context.Context, cfg AmbilightConfiguration) error {
	return s.client.doPost(ctx, "/6/ambilight/currentconfiguration", cfg, nil)
}

// AmbilightLounge is the GET/POST shape for /6/ambilight/lounge — the
// lounge-light effect on older firmware.
type AmbilightLounge struct {
	Color AmbilightHSB `json:"color"`
	Speed int          `json:"speed,omitempty"`
}

// AmbilightHSB is the hue/saturation/brightness triple used by the lounge
// endpoint. All three fields are 0–255 on most firmware.
type AmbilightHSB struct {
	Hue        int `json:"hue"`
	Saturation int `json:"saturation"`
	Brightness int `json:"brightness"`
}

// GetLounge returns the lounge-light HSB color + speed.
func (s *AmbilightService) GetLounge(ctx context.Context) (AmbilightLounge, error) {
	var l AmbilightLounge
	if err := s.client.doGet(ctx, "/6/ambilight/lounge", &l); err != nil {
		return AmbilightLounge{}, err
	}
	return l, nil
}

// SetLounge updates the lounge-light HSB color + speed.
func (s *AmbilightService) SetLounge(ctx context.Context, l AmbilightLounge) error {
	return s.client.doPost(ctx, "/6/ambilight/lounge", l, nil)
}

// SetSolidColor paints every LED of layer "layer1" the same color. Sequencing:
//
//  1. Read /topology so we know how many LEDs per edge to fill.
//  2. POST mode=manual — without this /cached writes are silently ignored.
//  3. Build the per-edge map keyed "0", "1", … and POST /cached.
//
// Returns the AmbilightColors structure that was sent, so callers can
// modify it incrementally without re-fetching topology.
func (s *AmbilightService) SetSolidColor(ctx context.Context, color AmbilightColor) (AmbilightColors, error) {
	top, err := s.Topology(ctx)
	if err != nil {
		return nil, fmt.Errorf("topology: %w", err)
	}
	if err := s.SetMode(ctx, AmbilightModeManual); err != nil {
		return nil, fmt.Errorf("set manual mode: %w", err)
	}
	colors := AmbilightColors{"layer1": buildSolidLayer(top, color)}
	if err := s.SetCachedColors(ctx, colors); err != nil {
		return nil, fmt.Errorf("set cached: %w", err)
	}
	return colors, nil
}

// buildSolidLayer fills every per-edge map with the same color.
func buildSolidLayer(t AmbilightTopology, color AmbilightColor) AmbilightLayer {
	fill := func(n int) map[string]AmbilightColor {
		if n <= 0 {
			return nil
		}
		m := make(map[string]AmbilightColor, n)
		for i := 0; i < n; i++ {
			m[strconv.Itoa(i)] = color
		}
		return m
	}
	return AmbilightLayer{
		Left:   fill(t.Left),
		Top:    fill(t.Top),
		Right:  fill(t.Right),
		Bottom: fill(t.Bottom),
	}
}
