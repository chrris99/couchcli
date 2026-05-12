package ambilighteffects

import (
	"math/rand/v2"
	"time"

	"github.com/chrris99/couchcli/internal/philips"
)

// Fire is a FastLED-style flame effect over the concatenated perimeter:
//
//  1. cool — each cell loses 0..Cooling heat
//  2. diffuse — heat[i] = avg(heat[i+1], heat[i+2], heat[i+2])
//  3. spark — with probability Sparking/255, ignite a random LED in the
//     "bottom" third of the perimeter at heat 160..255
//  4. map heat → black-red-yellow-white palette
//
// Stateful: keeps a per-LED heat buffer and its own rng. Use NewFire to
// construct (so the rng seed is explicit).
type Fire struct {
	Cooling  int // 20..100 typical; higher = flame dies out faster
	Sparking int // 50..200 typical; higher = more frequent sparks
	rng      *rand.Rand
	heat     []byte
}

// NewFire returns a Fire seeded deterministically. Pass a non-zero seed
// for reproducible animations (handy in tests).
func NewFire(cooling, sparking int, seed uint64) *Fire {
	return &Fire{
		Cooling:  cooling,
		Sparking: sparking,
		rng:      rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)),
	}
}

func (f *Fire) Frame(topo philips.AmbilightTopology, _ time.Duration) Frame {
	n := totalLEDs(topo)
	if n == 0 {
		return buildFrame(topo, func(int) philips.AmbilightColor { return philips.AmbilightColor{} })
	}
	if len(f.heat) != n {
		f.heat = make([]byte, n)
	}
	if f.rng == nil {
		// Defensive: a Fire literal constructed without NewFire still works.
		f.rng = rand.New(rand.NewPCG(1, 2))
	}

	cooling := f.Cooling
	if cooling <= 0 {
		cooling = 55
	}
	sparking := f.Sparking
	if sparking <= 0 {
		sparking = 120
	}

	// 1. cool every cell a random amount, scaled by perimeter length so
	//    longer strips don't flicker too aggressively.
	maxCool := (cooling*10)/n + 2
	for i := range f.heat {
		drop := f.rng.IntN(maxCool)
		if int(f.heat[i]) < drop {
			f.heat[i] = 0
		} else {
			f.heat[i] = byte(int(f.heat[i]) - drop)
		}
	}

	// 2. diffuse upward — propagate heat away from the spark zone.
	for i := n - 1; i >= 2; i-- {
		f.heat[i] = byte((int(f.heat[i-1]) + int(f.heat[i-2]) + int(f.heat[i-2])) / 3)
	}

	// 3. spark — ignite a fresh LED in the bottom third of the strip.
	if f.rng.IntN(255) < sparking {
		zone := n / 3
		if zone < 1 {
			zone = 1
		}
		y := f.rng.IntN(zone)
		add := 160 + f.rng.IntN(96) // 160..255
		v := int(f.heat[y]) + add
		if v > 255 {
			v = 255
		}
		f.heat[y] = byte(v)
	}

	return buildFrame(topo, func(idx int) philips.AmbilightColor {
		return heatToColor(f.heat[idx])
	})
}

// heatToColor maps 0..255 heat to a black → red → yellow → white ramp.
// Same shape as FastLED's HeatColor — three equal-width bands.
func heatToColor(heat byte) philips.AmbilightColor {
	// Scale heat into three bands of 0..85.
	t := int(heat)
	t192 := (t * 191) / 255 // 0..191
	band := t192 & 0x3F     // bottom 6 bits → 0..63
	heatRamp := byte(band << 2) // 0..252

	switch {
	case t192 >= 128: // hottest third — red+green fully on, ramp blue
		return philips.AmbilightColor{R: 255, G: 255, B: heatRamp}
	case t192 >= 64: // middle third — red full, ramp green, no blue
		return philips.AmbilightColor{R: 255, G: heatRamp, B: 0}
	default: // coolest third — ramp red only
		return philips.AmbilightColor{R: heatRamp, G: 0, B: 0}
	}
}
