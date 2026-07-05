package philips

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chrris99/couchcli/internal/clients/jointspace"
	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/drivers/philips/effects"
)

// AmbilightController is the Philips-specific ambilight capability. It is not a
// device.* interface because Ambilight is a single-driver feature — commands
// assert this driver-owned type instead.
type AmbilightController interface {
	AmbilightOn(ctx context.Context) error
	AmbilightOff(ctx context.Context) error
	SetAmbilightStyle(ctx context.Context, style string) error
	SetAmbilightColor(ctx context.Context, r, g, b uint8) error
	RunAmbilightEffect(ctx context.Context, e effects.Effect, fps int, dur time.Duration, restore bool) error
}

// Device is a controllable Philips TV: a jointspace protocol client adapted to
// the couchcli capability interfaces. The set of capabilities it satisfies is
// what makes it a "TV" — there is no TV type.
type Device struct {
	c    *jointspace.Client
	addr string
}

var (
	_ device.Device           = (*Device)(nil)
	_ device.VolumeController = (*Device)(nil)
	_ device.PowerController  = (*Device)(nil)
	_ device.MediaController  = (*Device)(nil)
	_ AmbilightController     = (*Device)(nil)
)

func (d *Device) AmbilightOn(ctx context.Context) error  { return d.c.Ambilight.On(ctx) }
func (d *Device) AmbilightOff(ctx context.Context) error { return d.c.Ambilight.Off(ctx) }

func (d *Device) SetAmbilightStyle(ctx context.Context, style string) error {
	return d.c.Ambilight.SetConfiguration(ctx, jointspace.AmbilightConfiguration{
		StyleName: jointspace.AmbilightStyleName(style),
	})
}

func (d *Device) SetAmbilightColor(ctx context.Context, r, g, b uint8) error {
	_, err := d.c.Ambilight.SetSolidColor(ctx, jointspace.AmbilightColor{R: r, G: g, B: b})
	return err
}

// RunAmbilightEffect animates the LED ring until dur elapses or ctx is
// cancelled. *jointspace.AmbilightService satisfies effects.Driver structurally.
func (d *Device) RunAmbilightEffect(ctx context.Context, e effects.Effect, fps int, dur time.Duration, restore bool) error {
	return effects.Run(ctx, d.c.Ambilight, e, fps, dur, restore)
}

// Open builds a controllable device from this driver's matcher value, wiring
// the discovery and control halves through one opener.
func (Driver) Open(addr string, settings json.RawMessage) (device.Device, error) {
	d, err := Open(addr, settings)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// Open builds a controllable device from a registry entry's address and this
// driver's settings blob. Secure entries talk HTTPS+digest (Android TVs);
// plain entries talk HTTP (legacy sets).
func Open(addr string, settings json.RawMessage) (*Device, error) {
	s, err := ParseSettings(settings)
	if err != nil {
		return nil, err
	}
	var c *jointspace.Client
	if s.Secure {
		c = jointspace.NewSecure(addr)
	} else {
		c = jointspace.NewPlain(addr)
	}
	if s.DeviceID != "" && s.AuthKey != "" {
		c.WithDigest(s.DeviceID, s.AuthKey)
	}
	return &Device{c: c, addr: addr}, nil
}

func (d *Device) Driver() device.DriverID { return ID }
func (d *Device) Address() string         { return d.addr }

func (d *Device) Volume(ctx context.Context) (device.Volume, error) {
	v, err := d.c.Volume.Get(ctx)
	if err != nil {
		return device.Volume{}, err
	}
	return device.Volume{Level: v.Current, Min: v.Min, Max: v.Max, Muted: v.Muted}, nil
}

func (d *Device) SetVolume(ctx context.Context, level int) error {
	return d.c.Volume.SetLevel(ctx, level)
}

func (d *Device) SetMuted(ctx context.Context, muted bool) error {
	return d.c.Volume.SetMuted(ctx, muted)
}

func (d *Device) Power(ctx context.Context) (device.PowerState, error) {
	s, err := d.c.Power.Get(ctx)
	if errors.Is(err, jointspace.ErrPowerstateUnsupported) {
		return device.PowerUnknown, fmt.Errorf("powerstate: %w", device.ErrUnsupported)
	}
	if err != nil {
		return device.PowerUnknown, err
	}
	switch s {
	case jointspace.PowerOn:
		return device.PowerOn, nil
	case jointspace.PowerStandby:
		return device.PowerStandby, nil
	default:
		return device.PowerUnknown, nil
	}
}

// PowerOn and Standby fall back to the Standby key on firmware without
// /6/powerstate, where that key toggles power.
func (d *Device) PowerOn(ctx context.Context) error {
	err := d.c.Power.On(ctx)
	if errors.Is(err, jointspace.ErrPowerstateUnsupported) {
		return d.c.Keys.Send(ctx, jointspace.KeyStandby)
	}
	return err
}

func (d *Device) Standby(ctx context.Context) error {
	err := d.c.Power.Standby(ctx)
	if errors.Is(err, jointspace.ErrPowerstateUnsupported) {
		return d.c.Keys.Send(ctx, jointspace.KeyStandby)
	}
	return err
}

// mediaKeys maps domain transport actions to JointSpace remote keys. Whether a
// key takes effect depends on the active app; PlayPause is the most universal.
var mediaKeys = map[device.MediaAction]jointspace.Key{
	device.MediaPlay:        jointspace.KeyPlay,
	device.MediaPause:       jointspace.KeyPause,
	device.MediaPlayPause:   jointspace.KeyPlayPause,
	device.MediaStop:        jointspace.KeyStop,
	device.MediaNext:        jointspace.KeyNext,
	device.MediaPrevious:    jointspace.KeyPrevious,
	device.MediaFastForward: jointspace.KeyFastForward,
	device.MediaRewind:      jointspace.KeyRewind,
}

func (d *Device) Media(ctx context.Context, action device.MediaAction) error {
	key, ok := mediaKeys[action]
	if !ok {
		return fmt.Errorf("media action %q: %w", action, device.ErrUnsupported)
	}
	return d.c.Keys.Send(ctx, key)
}
