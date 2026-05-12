package ambilighteffects

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrris99/couchcli/internal/philips"
)

// fakeDriver records every call. It is safe for the runner's single
// goroutine — no inter-goroutine access in Run.
type fakeDriver struct {
	mu       sync.Mutex
	calls    []string
	topology philips.AmbilightTopology
	frames   []Frame
	topoErr  error
	modeErr  error
	cachedErr error
}

func (f *fakeDriver) Topology(_ context.Context) (philips.AmbilightTopology, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Topology")
	return f.topology, f.topoErr
}

func (f *fakeDriver) SetMode(_ context.Context, m philips.AmbilightMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "SetMode:"+string(m))
	return f.modeErr
}

func (f *fakeDriver) SetCached(_ context.Context, c philips.AmbilightColors) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "SetCached")
	f.frames = append(f.frames, c)
	return f.cachedErr
}

func TestRun_SequenceAndRestore(t *testing.T) {
	d := &fakeDriver{topology: philips.AmbilightTopology{Layers: 1, Top: 4, Right: 2, Bottom: 0, Left: 2}}
	eff := Wave{Color: philips.AmbilightColor{R: 0, G: 255, B: 0}, Period: 500 * time.Millisecond}

	if err := Run(context.Background(), d, eff, 10, 250*time.Millisecond, true); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(d.calls) < 3 {
		t.Fatalf("calls=%v (want at least topology+mode+cached+restoreMode)", d.calls)
	}
	if d.calls[0] != "Topology" {
		t.Errorf("first call %q want Topology", d.calls[0])
	}
	if d.calls[1] != "SetMode:manual" {
		t.Errorf("second call %q want SetMode:manual", d.calls[1])
	}
	// last call should be the restore.
	if last := d.calls[len(d.calls)-1]; last != "SetMode:internal" {
		t.Errorf("last call %q want SetMode:internal", last)
	}

	cached := 0
	for _, c := range d.calls {
		if c == "SetCached" {
			cached++
		}
	}
	if cached < 1 {
		t.Errorf("got %d SetCached calls, want >= 1", cached)
	}
}

func TestRun_NoRestore(t *testing.T) {
	d := &fakeDriver{topology: philips.AmbilightTopology{Layers: 1, Top: 2}}
	eff := Wave{Color: philips.AmbilightColor{R: 255}, Period: time.Second}

	if err := Run(context.Background(), d, eff, 10, 150*time.Millisecond, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, c := range d.calls {
		if c == "SetMode:internal" {
			t.Errorf("found SetMode:internal in %v — restore should be skipped", d.calls)
		}
	}
}

func TestRun_RejectsBadFPS(t *testing.T) {
	d := &fakeDriver{}
	err := Run(context.Background(), d, Wave{Period: time.Second}, 0, time.Second, true)
	if err == nil || !strings.Contains(err.Error(), "fps") {
		t.Errorf("expected fps error, got %v", err)
	}
	if len(d.calls) != 0 {
		t.Errorf("driver should not be called: %v", d.calls)
	}
}

func TestRun_RejectsZeroDuration(t *testing.T) {
	d := &fakeDriver{}
	err := Run(context.Background(), d, Wave{Period: time.Second}, 10, 0, true)
	if err == nil || !strings.Contains(err.Error(), "duration") {
		t.Errorf("expected duration error, got %v", err)
	}
}

func TestRun_EmptyTopology(t *testing.T) {
	d := &fakeDriver{topology: philips.AmbilightTopology{Layers: 1}}
	err := Run(context.Background(), d, Wave{Period: time.Second}, 10, 100*time.Millisecond, false)
	if err == nil || !strings.Contains(err.Error(), "0 LEDs") {
		t.Errorf("expected 0-LEDs error, got %v", err)
	}
}

func TestRun_CtxCancellation(t *testing.T) {
	d := &fakeDriver{topology: philips.AmbilightTopology{Layers: 1, Top: 2}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := Run(ctx, d, Wave{Period: time.Second}, 10, time.Second, true)
	// First Topology call uses ctx, but our fake ignores ctx — so the
	// runner will hit the select after the first SetCached and exit on
	// ctx.Done. Either path yields a non-nil error.
	if err == nil {
		t.Errorf("expected non-nil error after cancel")
	}
}
