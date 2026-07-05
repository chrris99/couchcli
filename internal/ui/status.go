package ui

// StatusBadge renders a device connection/power status as a colored glyph +
// word. state is the computed status string ("on", "standby", "reachable",
// "unreachable") — power state plus the two connectivity outcomes, which are
// not device.PowerState values.
func StatusBadge(state string) string {
	switch state {
	case "on":
		return Success.Render("● on")
	case "standby":
		return Warn.Render("◐ standby")
	case "reachable":
		return Success.Render("● reachable")
	case "unreachable":
		return Error.Render("○ unreachable")
	default:
		return Error.Render(state)
	}
}
