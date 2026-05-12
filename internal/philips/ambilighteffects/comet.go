package ambilighteffects

import (
	"math"
	"time"

	"github.com/chrris99/couchcli/internal/philips"
)

// Comet animates a bright head looping the perimeter, with a fading tail
// of length TailLen behind it. Period is the time for one full lap.
type Comet struct {
	Color   philips.AmbilightColor
	Period  time.Duration
	TailLen int // length of fading tail in LEDs (must be >= 1)
}

func (c Comet) Frame(topo philips.AmbilightTopology, t time.Duration) Frame {
	total := totalLEDs(topo)
	if total == 0 {
		return buildFrame(topo, func(int) philips.AmbilightColor { return philips.AmbilightColor{} })
	}
	period := c.Period.Seconds()
	if period <= 0 {
		period = 3
	}
	tail := c.TailLen
	if tail < 1 {
		tail = 1
	}

	// Continuous head position (fractional) wraps over [0, total).
	headPos := math.Mod(t.Seconds()/period*float64(total), float64(total))

	return buildFrame(topo, func(idx int) philips.AmbilightColor {
		// Distance from this LED backwards to the head, wrapping at total.
		// We want LEDs behind the head (head-1, head-2, ...) to glow.
		d := math.Mod(headPos-float64(idx)+float64(total), float64(total))
		if d > float64(tail) {
			return philips.AmbilightColor{}
		}
		// 1.0 at the head, falling linearly to 0 at idx = head - tail.
		intensity := 1 - d/float64(tail)
		if intensity < 0 {
			intensity = 0
		}
		return scaleColor(c.Color, intensity)
	})
}
