package effects

import (
	"math"
	"time"

	jointspace "github.com/chrris99/couchcli/internal/clients/jointspace"
)

// Wave animates a sinusoidal brightness sweep around the perimeter in a
// single color. Adjacent LEDs see adjacent phases — a wave looks like a
// wave regardless of the (concatenated) edge ordering.
type Wave struct {
	Color  Color
	Period time.Duration // time for one full revolution
}

func (w Wave) Frame(topo jointspace.AmbilightTopology, t time.Duration) Frame {
	total := totalLEDs(topo)
	period := w.Period.Seconds()
	if period <= 0 {
		period = 2 // safety default
	}
	phase := 2 * math.Pi * (t.Seconds() / period)

	return buildFrame(topo, func(idx int) Color {
		theta := 2*math.Pi*float64(idx)/float64(total) - phase
		// (1+sin)/2 → 0..1; add a floor so LEDs aren't pitch-black between peaks.
		s := (1 + math.Sin(theta)) / 2
		intensity := 0.10 + 0.90*s
		return scaleColor(w.Color, intensity)
	})
}
