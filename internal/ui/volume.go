package ui

import (
	"fmt"
	"image/color"

	"charm.land/bubbles/v2/progress"

	"github.com/chrris99/couchcli/internal/device"
)

const volumeBarWidth = 22

// Two bars sharing the palette: accent when live, subtle when muted. Bubble
// Models are cheap value types; ViewAs renders statically with no tea loop.
var (
	volBar      = newVolumeBar(accent)
	volBarMuted = newVolumeBar(subtle)
)

func newVolumeBar(fill color.Color) progress.Model {
	return progress.New(
		progress.WithWidth(volumeBarWidth),
		progress.WithColors(fill),
		progress.WithFillCharacters('█', '░'),
	)
}

// VolumeLine renders a device's volume as "alias  ██████ 50%  30/60", with a
// "muted" tag and dimmed bar when muted.
func VolumeLine(alias string, v device.Volume) string {
	bar := volBar
	if v.Muted {
		bar = volBarMuted
	}
	frac := float64(max(0, min(100, v.Percent()))) / 100
	tail := fmt.Sprintf("%d/%d", v.Level, v.Max)
	if v.Muted {
		tail += " " + Warn.Render("muted")
	}
	return fmt.Sprintf("%s  %s  %s", Title.Render(alias), bar.ViewAs(frac), Subtle.Render(tail))
}
