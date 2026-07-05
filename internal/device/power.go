package device

import "context"

// PowerState is a device's reported power state. The zero value is
// PowerUnknown, for devices that answer on the network but cannot report
// their state.
type PowerState string

const (
	PowerUnknown PowerState = ""
	PowerOn      PowerState = "on"
	PowerStandby PowerState = "standby"
)

func (s PowerState) String() string {
	if s == PowerUnknown {
		return "unknown"
	}
	return string(s)
}

// PowerController is implemented by devices that can report and change their
// power state. Power returns ErrUnsupported when the unit answers but cannot
// report its state; PowerOn and Standby do the driver's best to reach the
// target state, including any firmware-specific fallback.
type PowerController interface {
	Power(ctx context.Context) (PowerState, error)
	PowerOn(ctx context.Context) error
	Standby(ctx context.Context) error
}
