package philips

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type AmbilightPower string

const (
	AmbilightPowerOn  AmbilightPower = "On"
	AmbilightPowerOff AmbilightPower = "Off"
)

type AmbilightMode string

const (
	AmbilightModeInternal AmbilightMode = "internal"
	AmbilightModeManual   AmbilightMode = "manual"
	AmbilightModeExpert   AmbilightMode = "expert"
)

type AmbilightTopology struct {
	Layers int `json:"layers"`
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

// UnmarshalJSON accepts the wire shape used by 2017+ Android Philips
// firmware, which sends "layers" as a JSON string ("1") rather than the
// number 1 documented elsewhere. Other fields are always numbers.
func (t *AmbilightTopology) UnmarshalJSON(data []byte) error {
	var raw struct {
		Layers any `json:"layers"`
		Left   int `json:"left"`
		Top    int `json:"top"`
		Right  int `json:"right"`
		Bottom int `json:"bottom"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch v := raw.Layers.(type) {
	case nil:
		t.Layers = 0
	case float64:
		t.Layers = int(v)
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("ambilight topology: invalid layers %q: %w", v, err)
		}
		t.Layers = n
	default:
		return fmt.Errorf("ambilight topology: unexpected layers type %T", v)
	}
	t.Left, t.Top, t.Right, t.Bottom = raw.Left, raw.Top, raw.Right, raw.Bottom
	return nil
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

// AmbilightColors is the RootModel — just a named map, no wrapper struct.
// Marshals/unmarshals directly as a JSON object.
type AmbilightColors map[string]AmbilightLayer

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

// AmbilightConfiguration mirrors GET/POST /6/ambilight/currentconfiguration.
// MenuSetting is the per-style variant ("STANDARD", "NATURAL", "VIVID", …) —
// the TV validates the value, this package passes it through verbatim.
type AmbilightConfiguration struct {
	StyleName   AmbilightStyleName `json:"styleName"`
	IsExpert    bool               `json:"isExpert,omitempty"`
	MenuSetting string             `json:"menuSetting,omitempty"`
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
