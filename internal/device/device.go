// Package device defines the driver-neutral vocabulary of couchcli: the
// value types that capability interfaces speak (Volume, PowerState) and the
// errors shared across drivers.
//
// It imports no driver or protocol packages.
// Drivers depend on this package, never the other way around.
package device

import "errors"

// ErrUnsupported is returned by a driver when the device model nominally has
// a capability but this particular unit or firmware does not support the
// requested operation.
// Callers detect it with errors.Is and fall back or report accordingly.
//
// Drivers should wrap it with context: fmt.Errorf("powerstate: %w", device.ErrUnsupported)
var ErrUnsupported = errors.New("operation not supported by this device")

// Device is the identity every driver-built device shares. A concrete device
// additionally implements whatever capability interfaces its driver supports
// (VolumeController, and others as they land); callers discover a capability
// with a type assertion:
//
//	if vc, ok := dev.(VolumeController); ok { … }
//
// Capability presence is two-level: the assertion reports whether the driver
// implements the capability at all, while ErrUnsupported reports that a
// specific unit or firmware cannot perform an operation it nominally has.
type Device interface {
	Driver() DriverID
	Address() string
}
