// Package ambilighteffects renders animated frames on a Philips Ambilight
// LED ring. Each effect is a tiny stateless-or-stateful object that knows
// how to compute one Frame for a given (topology, t). The Run driver
// handles the boring side: fetching topology, switching to manual mode,
// ticking at fps, and (optionally) restoring mode=internal on exit.
//
// Effects don't talk to the TV — they're pure math. This keeps them
// trivially unit-testable and lets the runner be the single place that
// owns transport concerns.
package effects

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	jointspace "github.com/chrris99/couchcli/internal/clients/jointspace"
)

// Frame is the wire shape sent to /6/ambilight/cached. Aliased so effect
// signatures stay short.
type Frame = jointspace.AmbilightColors

// Effect renders one frame at elapsed time t against the given topology.
// Implementations may be stateful (e.g. Fire keeps a heat buffer + rng)
// but Frame should be safe to call repeatedly with monotonically
// increasing t.
type Effect interface {
	Frame(topo jointspace.AmbilightTopology, t time.Duration) Frame
}

// Color is an effect's RGB value, 0-255 per channel. Effects compute in this
// neutral type so callers never touch the jointspace wire color; buildFrame
// converts to the wire type at the edge.
type Color struct {
	R, G, B uint8
}

// Driver is the minimal slice of *jointspace.AmbilightService the runner
// needs. *jointspace.AmbilightService satisfies this interface structurally,
// so production callers pass c.Ambilight; tests pass an in-memory fake.
type Driver interface {
	Topology(ctx context.Context) (jointspace.AmbilightTopology, error)
	SetMode(ctx context.Context, m jointspace.AmbilightMode) error
	SetCachedColors(ctx context.Context, c jointspace.AmbilightColors) error
}

// Run drives Effect e for dur at fps frames per second.
//
//  1. Fetches topology once.
//  2. Sets mode=manual (so SetCached takes effect).
//  3. Ticks at 1/fps, pushing e.Frame(topo, elapsed).
//  4. On exit (dur elapsed, ctx cancelled, or push error), if restore is
//     true, posts mode=internal with a fresh 3s context — so we restore
//     even on ctx cancellation.
//
// fps must be in [1, 30]. dur must be > 0.
func Run(ctx context.Context, d Driver, e Effect, fps int, dur time.Duration, restore bool) error {
	if fps < 1 || fps > 30 {
		return fmt.Errorf("fps must be between 1 and 30 (got %d)", fps)
	}
	if dur <= 0 {
		return fmt.Errorf("duration must be positive (got %s)", dur)
	}

	topo, err := d.Topology(ctx)
	if err != nil {
		return fmt.Errorf("topology: %w", err)
	}
	if topo.Top+topo.Right+topo.Bottom+topo.Left == 0 {
		return errors.New("topology reports 0 LEDs — TV may not support Ambilight")
	}

	if err := d.SetMode(ctx, jointspace.AmbilightModeManual); err != nil {
		return fmt.Errorf("set manual mode: %w", err)
	}
	if restore {
		defer func() {
			// Use a fresh context: the caller's may have already been
			// cancelled, but we still want to leave the TV in a sane state.
			restoreCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = d.SetMode(restoreCtx, jointspace.AmbilightModeInternal)
		}()
	}

	interval := time.Second / time.Duration(fps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	start := time.Now()
	for {
		elapsed := time.Since(start)
		if elapsed >= dur {
			return nil
		}
		if err := d.SetCachedColors(ctx, e.Frame(topo, elapsed)); err != nil {
			return fmt.Errorf("set cached: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// --- Shared helpers ------------------------------------------------------

// scaleChannel multiplies a 0-255 channel by factor and clamps to 0-255.
func scaleChannel(c uint8, factor float64) uint8 {
	v := float64(c) * factor
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return uint8(v)
}

// scaleColor applies the same intensity to all three channels.
func scaleColor(c Color, intensity float64) Color {
	return Color{
		R: scaleChannel(c.R, intensity),
		G: scaleChannel(c.G, intensity),
		B: scaleChannel(c.B, intensity),
	}
}

// totalLEDs returns the number of controllable LEDs in topo (single layer).
func totalLEDs(topo jointspace.AmbilightTopology) int {
	return topo.Top + topo.Right + topo.Bottom + topo.Left
}

// edgeForIndex returns (edgeName, indexWithinEdge) for a perimeter index
// in the order top → right → bottom → left. Used by effects that walk the
// ring (wave, comet, rainbow).
func edgeForIndex(topo jointspace.AmbilightTopology, perimeterIdx int) (edge string, idx int) {
	if perimeterIdx < topo.Top {
		return "top", perimeterIdx
	}
	perimeterIdx -= topo.Top
	if perimeterIdx < topo.Right {
		return "right", perimeterIdx
	}
	perimeterIdx -= topo.Right
	if perimeterIdx < topo.Bottom {
		return "bottom", perimeterIdx
	}
	perimeterIdx -= topo.Bottom
	return "left", perimeterIdx
}

// buildFrame assembles a Frame by calling colorAt(perimeterIdx) for each
// LED. Edges with 0 LEDs are omitted (so they marshal as absent fields).
func buildFrame(topo jointspace.AmbilightTopology, colorAt func(perimeterIdx int) Color) Frame {
	total := totalLEDs(topo)
	if total == 0 {
		return Frame{"layer1": jointspace.AmbilightLayer{}}
	}

	makeEdge := func(count, offset int) map[string]jointspace.AmbilightColor {
		if count <= 0 {
			return nil
		}
		m := make(map[string]jointspace.AmbilightColor, count)
		for i := range count {
			c := colorAt(offset + i)
			m[strconv.Itoa(i)] = jointspace.AmbilightColor{R: c.R, G: c.G, B: c.B}
		}
		return m
	}

	layer := jointspace.AmbilightLayer{
		Top:    makeEdge(topo.Top, 0),
		Right:  makeEdge(topo.Right, topo.Top),
		Bottom: makeEdge(topo.Bottom, topo.Top+topo.Right),
		Left:   makeEdge(topo.Left, topo.Top+topo.Right+topo.Bottom),
	}
	return Frame{"layer1": layer}
}

// hsvToRGB converts hue ∈ [0,1) saturation ∈ [0,1] value ∈ [0,1] to RGB.
// Out-of-range hues are wrapped, s and v are clamped.
func hsvToRGB(h, s, v float64) Color {
	h = h - math.Floor(h) // wrap into [0, 1)
	if s < 0 {
		s = 0
	} else if s > 1 {
		s = 1
	}
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}

	sector := h * 6
	i := math.Floor(sector)
	f := sector - i
	p := v * (1 - s)
	q := v * (1 - s*f)
	tt := v * (1 - s*(1-f))

	var r, g, b float64
	switch int(i) % 6 {
	case 0:
		r, g, b = v, tt, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, tt
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = tt, p, v
	case 5:
		r, g, b = v, p, q
	}
	return Color{
		R: uint8(math.Round(r * 255)),
		G: uint8(math.Round(g * 255)),
		B: uint8(math.Round(b * 255)),
	}
}
