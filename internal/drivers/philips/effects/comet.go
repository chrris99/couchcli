package effects

import (
	"math"
	"time"

	jointspace "github.com/chrris99/couchcli/internal/clients/jointspace"
)

// Comet animates a bright head looping the perimeter, with a fading tail.
type Comet struct {
	Color   Color
	Period  time.Duration // Time for one full lap.
	TailLen int           // Length of the fading tail in LEDs.
}

func (c Comet) Frame(topo jointspace.AmbilightTopology, t time.Duration) Frame {
	total := totalLEDs(topo)
	if total == 0 {
		return buildFrame(topo, func(int) Color { return Color{} })
	}
	period := c.Period.Seconds()
	if period <= 0 {
		period = 3
	}
	tail := max(c.TailLen, 1)

	// Continuous head position (fractional) wraps over [0, total).
	headPos := math.Mod(t.Seconds()/period*float64(total), float64(total))

	return buildFrame(topo, func(idx int) Color {
		// Distance from this LED backwards to the head, wrapping at total.
		// We want LEDs behind the head (head-1, head-2, ...) to glow.
		d := math.Mod(headPos-float64(idx)+float64(total), float64(total))
		if d > float64(tail) {
			return Color{}
		}
		// 1.0 at the head, falling linearly to 0 at idx = head - tail.
		intensity := 1 - d/float64(tail)
		if intensity < 0 {
			intensity = 0
		}
		return scaleColor(c.Color, intensity)
	})
}
