package ui

import (
	"context"
	"os"

	"charm.land/huh/v2/spinner"
)

// Spin runs fn under a spinner on stderr. Non-interactive sessions run fn
// directly with no output, so pipes and scripts stay clean.
func Spin(ctx context.Context, title string, fn func(context.Context) error) error {
	if !Interactive() {
		return fn(ctx)
	}
	return spinner.New().
		Title(title).
		WithOutput(os.Stderr).
		Context(ctx).
		ActionWithErr(fn).
		Run()
}
