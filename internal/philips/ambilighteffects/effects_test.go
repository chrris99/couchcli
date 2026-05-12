package ambilighteffects

import (
	"math"
	"testing"
	"time"

	"github.com/chrris99/couchcli/internal/philips"
)

// Real device shape (65OLED706): 3-sided, no bottom.
var topo3sided = philips.AmbilightTopology{Layers: 1, Top: 8, Right: 3, Bottom: 0, Left: 3}

// Hypothetical 4-sided device.
var topo4sided = philips.AmbilightTopology{Layers: 1, Top: 8, Right: 4, Bottom: 8, Left: 4}

func countLEDs(f Frame) int {
	layer := f["layer1"]
	return len(layer.Top) + len(layer.Right) + len(layer.Bottom) + len(layer.Left)
}

func TestHelpers_TotalLEDs(t *testing.T) {
	if got := totalLEDs(topo3sided); got != 14 {
		t.Errorf("3-sided total=%d want 14", got)
	}
	if got := totalLEDs(topo4sided); got != 24 {
		t.Errorf("4-sided total=%d want 24", got)
	}
}

func TestHelpers_BuildFrame_OmitsEmptyEdge(t *testing.T) {
	f := buildFrame(topo3sided, func(int) philips.AmbilightColor {
		return philips.AmbilightColor{R: 1}
	})
	if f["layer1"].Bottom != nil {
		t.Errorf("bottom should be nil for 3-sided: %+v", f["layer1"].Bottom)
	}
	if len(f["layer1"].Top) != 8 || len(f["layer1"].Right) != 3 || len(f["layer1"].Left) != 3 {
		t.Errorf("edge counts wrong: %+v", f["layer1"])
	}
}

func TestHelpers_HSVToRGB(t *testing.T) {
	// Hue 0 with full sat+val should be pure red.
	r := hsvToRGB(0, 1, 1)
	if r.R != 255 || r.G != 0 || r.B != 0 {
		t.Errorf("hue=0 → %+v want {255,0,0}", r)
	}
	// Hue 1/3 → green.
	g := hsvToRGB(1.0/3.0, 1, 1)
	if g.G != 255 || g.R > 1 || g.B > 1 {
		t.Errorf("hue=1/3 → %+v want pure green", g)
	}
	// Hue wraps.
	w := hsvToRGB(1.0, 1, 1)
	if w.R != 255 || w.G != 0 || w.B != 0 {
		t.Errorf("hue=1 → %+v want red (wraps)", w)
	}
}

func TestHelpers_ScaleChannel_Clamps(t *testing.T) {
	if scaleChannel(100, 10) != 255 {
		t.Errorf("clamp high failed")
	}
	if scaleChannel(100, -1) != 0 {
		t.Errorf("clamp low failed")
	}
}

func TestWave_FrameShape(t *testing.T) {
	w := Wave{Color: philips.AmbilightColor{G: 255}, Period: 2 * time.Second}
	f := w.Frame(topo3sided, 0)
	if countLEDs(f) != 14 {
		t.Errorf("LED count=%d want 14", countLEDs(f))
	}
	// At t=0, idx=0: theta=0, sin=0 → intensity=0.55. So G ≈ 140.
	first := f["layer1"].Top["0"]
	if first.G < 130 || first.G > 150 {
		t.Errorf("Top[0].G=%d want ~140", first.G)
	}
	if first.R != 0 || first.B != 0 {
		t.Errorf("non-green channels nonzero: %+v", first)
	}
}

func TestPulse_AllSameColor(t *testing.T) {
	p := Pulse{Color: philips.AmbilightColor{R: 200}, Period: time.Second, Floor: 0.1}
	f := p.Frame(topo4sided, 0)
	if countLEDs(f) != 24 {
		t.Errorf("LED count=%d want 24", countLEDs(f))
	}
	// At t=0, sin=0 → intensity = 0.1 + 0.9*0.5 = 0.55. R ≈ 110.
	want := f["layer1"].Top["0"]
	if want.R < 105 || want.R > 115 {
		t.Errorf("Top[0].R=%d want ~110", want.R)
	}
	// All LEDs should be identical.
	for _, m := range []map[string]philips.AmbilightColor{f["layer1"].Top, f["layer1"].Right, f["layer1"].Bottom, f["layer1"].Left} {
		for k, c := range m {
			if c != want {
				t.Errorf("LED %s differs: %+v vs %+v", k, c, want)
			}
		}
	}
}

func TestRainbow_FirstLEDIsRed(t *testing.T) {
	r := Rainbow{Period: 5 * time.Second, Saturation: 1, Value: 1}
	f := r.Frame(topo3sided, 0)
	first := f["layer1"].Top["0"]
	if first.R != 255 || first.G > 5 || first.B > 5 {
		t.Errorf("Top[0]=%+v want red", first)
	}
}

func TestComet_HeadAndTail(t *testing.T) {
	c := Comet{Color: philips.AmbilightColor{R: 200, G: 100}, Period: time.Second, TailLen: 4}
	f := c.Frame(topo3sided, 0) // headPos = 0
	// Top[0] is the head — should be at full intensity.
	head := f["layer1"].Top["0"]
	if head.R != 200 || head.G != 100 {
		t.Errorf("head=%+v want full color", head)
	}
	// Top[5] (and beyond) should be dark — outside tail.
	cold := f["layer1"].Top["5"]
	if cold.R != 0 || cold.G != 0 {
		t.Errorf("Top[5]=%+v want dark", cold)
	}
	// Last LED on Left edge is one step "behind" the head (wraps); intensity = 1 - 1/4 = 0.75.
	tail := f["layer1"].Left["2"]
	if tail.R < 140 || tail.R > 160 {
		t.Errorf("Left[2].R=%d want ~150 (75%% of 200)", tail.R)
	}
}

func TestFire_StableShapeAndChannels(t *testing.T) {
	f := NewFire(55, 120, 42)
	// Run a handful of ticks to let the simulation populate.
	var frame Frame
	for i := 0; i < 30; i++ {
		frame = f.Frame(topo3sided, time.Duration(i)*100*time.Millisecond)
	}
	if countLEDs(frame) != 14 {
		t.Errorf("LED count=%d want 14", countLEDs(frame))
	}
	// At least one LED should have lit up by now (deterministic seed → guaranteed).
	any := false
	for _, m := range []map[string]philips.AmbilightColor{frame["layer1"].Top, frame["layer1"].Right, frame["layer1"].Left} {
		for _, c := range m {
			if c.R > 0 || c.G > 0 || c.B > 0 {
				any = true
			}
			// Fire palette: blue is only present when both R and G are at 255.
			if c.B > 0 && (c.R != 255 || c.G != 255) {
				t.Errorf("blue present without full R+G: %+v", c)
			}
		}
	}
	if !any {
		t.Errorf("no LEDs lit after 30 ticks — fire seems extinguished")
	}
}

func TestFire_DeterministicWithSeed(t *testing.T) {
	a := NewFire(55, 120, 7)
	b := NewFire(55, 120, 7)
	var fa, fb Frame
	for i := 0; i < 10; i++ {
		fa = a.Frame(topo3sided, time.Duration(i)*100*time.Millisecond)
		fb = b.Frame(topo3sided, time.Duration(i)*100*time.Millisecond)
	}
	for edge, m := range fa["layer1"].Top {
		if fb["layer1"].Top[edge] != m {
			t.Errorf("same seed → different output at Top[%s]", edge)
			break
		}
	}
}

// Sanity: rainbow output around the perimeter should be roughly continuous
// — adjacent LEDs should differ by less than ~half the channel range.
func TestRainbow_AdjacentLEDsAreClose(t *testing.T) {
	r := Rainbow{Period: 5 * time.Second, Saturation: 1, Value: 1}
	f := r.Frame(topo3sided, 0)
	// Walk top edge: indices 0..7.
	top := f["layer1"].Top
	for i := 0; i < 7; i++ {
		a := top[itoa(i)]
		b := top[itoa(i+1)]
		if abs(int(a.R)-int(b.R))+abs(int(a.G)-int(b.G))+abs(int(a.B)-int(b.B)) > 200 {
			t.Errorf("rainbow discontinuity at Top[%d]→Top[%d]: %+v %+v", i, i+1, a, b)
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func itoa(i int) string {
	return string(rune('0' + i))
}

// Catches a regression where buildFrame wraps an empty topology by
// returning nil; we want it to return a non-nil Frame{layer1:{}}.
func TestBuildFrame_ZeroLEDs(t *testing.T) {
	f := buildFrame(philips.AmbilightTopology{Layers: 1}, func(int) philips.AmbilightColor {
		return philips.AmbilightColor{}
	})
	if f == nil {
		t.Fatal("nil frame")
	}
	if _, ok := f["layer1"]; !ok {
		t.Errorf("missing layer1: %+v", f)
	}
}

// Quick sanity: wave covers the full intensity range over enough samples.
func TestWave_IntensityRange(t *testing.T) {
	w := Wave{Color: philips.AmbilightColor{G: 255}, Period: time.Second}
	min, max := 256, -1
	for i := 0; i < 100; i++ {
		f := w.Frame(topo3sided, time.Duration(i)*10*time.Millisecond)
		for _, c := range f["layer1"].Top {
			g := int(c.G)
			if g < min {
				min = g
			}
			if g > max {
				max = g
			}
		}
	}
	if min > 60 {
		t.Errorf("wave min=%d should be near floor (~25)", min)
	}
	if max < 200 {
		t.Errorf("wave max=%d should approach 255", max)
	}
	if math.Abs(float64(max-min)) < 100 {
		t.Errorf("wave range too small: %d..%d", min, max)
	}
}
