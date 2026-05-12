package ambilighteffects

import (
	"math"
	"time"

	"github.com/chrris99/couchcli/internal/philips"
)

// Pulse animates a whole-strip breathing pulse in a single color.
// Intensity = floor + (1-floor) * (1 + sin) / 2.
type Pulse struct {
	Color  philips.AmbilightColor
	Period time.Duration // time for one breath in→out cycle
	Floor  float64       // minimum intensity, 0..1 (use ~0.05–0.2 to avoid pitch-black dips)
}

func (p Pulse) Frame(topo philips.AmbilightTopology, t time.Duration) Frame {
	period := p.Period.Seconds()
	if period <= 0 {
		period = 1.5
	}
	floor := p.Floor
	if floor < 0 {
		floor = 0
	} else if floor > 1 {
		floor = 1
	}

	phase := 2 * math.Pi * (t.Seconds() / period)
	s := (1 + math.Sin(phase)) / 2
	intensity := floor + (1-floor)*s
	c := scaleColor(p.Color, intensity)

	return buildFrame(topo, func(_ int) philips.AmbilightColor {
		return c
	})
}
