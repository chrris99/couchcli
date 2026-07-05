package effects

import (
	"time"

	jointspace "github.com/chrris99/couchcli/internal/clients/jointspace"
)

// Rainbow maps perimeter index → hue and rotates the hue offset over
// time. One full hue rotation per Period.
type Rainbow struct {
	Period     time.Duration
	Saturation float64
	Value      float64
}

func (r Rainbow) Frame(topo jointspace.AmbilightTopology, t time.Duration) Frame {
	total := totalLEDs(topo)
	period := r.Period.Seconds()
	if period <= 0 {
		period = 5
	}
	sat := r.Saturation
	if sat <= 0 {
		sat = 1
	}
	val := r.Value
	if val <= 0 {
		val = 1
	}
	offset := t.Seconds() / period
	if total == 0 {
		return buildFrame(topo, func(int) Color { return Color{} })
	}

	return buildFrame(topo, func(idx int) Color {
		hue := float64(idx)/float64(total) + offset
		return hsvToRGB(hue, sat, val)
	})
}
