package ui

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	huh "charm.land/huh/v2"

	"github.com/chrris99/couchcli/internal/device"
)

// ErrNotInteractive is returned when a prompt is needed but stdin/stderr is
// not a terminal. Callers should suggest the non-interactive alternative.
var ErrNotInteractive = errors.New("interactive prompt requires a terminal")

// PickCandidate lets the user choose one discovered device.
func PickCandidate(title string, candidates []device.Candidate) (device.Candidate, error) {
	if !Interactive() {
		return device.Candidate{}, ErrNotInteractive
	}
	options := make([]huh.Option[int], len(candidates))
	for i, c := range candidates {
		name := c.Name
		if name == "" {
			name = "(unnamed)"
		}
		options[i] = huh.NewOption(fmt.Sprintf("%s  %s", name, Subtle.Render(c.Address)), i)
	}
	var idx int
	if err := huh.NewSelect[int]().Title(title).Options(options...).Value(&idx).Run(); err != nil {
		return device.Candidate{}, err
	}
	return candidates[idx], nil
}

var pinRE = regexp.MustCompile(`^\d{4}$`)

// PromptPIN asks for the 4-digit pairing PIN shown on the TV. Cancelling ctx
// (e.g. the pairing window expiring) aborts the prompt.
func PromptPIN(ctx context.Context) (string, error) {
	if !Interactive() {
		return "", ErrNotInteractive
	}
	var pin string
	input := huh.NewInput().
		Title("Enter the PIN shown on the TV").
		CharLimit(4).
		Validate(func(s string) error {
			if !pinRE.MatchString(s) {
				return errors.New("the PIN is 4 digits")
			}
			return nil
		}).
		Value(&pin)

	done := make(chan error, 1)
	go func() { done <- input.Run() }()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-done:
		return pin, err
	}
}

// Confirm asks a yes/no question, defaulting to no.
func Confirm(question string) (bool, error) {
	if !Interactive() {
		return false, ErrNotInteractive
	}
	var ok bool
	err := huh.NewConfirm().Title(question).Value(&ok).Run()
	return ok, err
}
