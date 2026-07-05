package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/discovery"
	"github.com/chrris99/couchcli/internal/drivers/android"
	philipsdriver "github.com/chrris99/couchcli/internal/drivers/philips"
)

// driverEntry is a driver that participates in discovery. Control is optional:
// an entry that also satisfies opener can build a controllable device from a
// stored registry entry.
type driverEntry interface {
	discovery.Matcher
	ID() device.DriverID
}

// opener is the control half of a driver — present only once the driver can
// build a device from a stored entry, absent while it can merely discover.
type opener interface {
	Open(addr string, settings json.RawMessage) (device.Device, error)
}

// drivers is the one registry of every linked-in driver. Discovery derives its
// matcher set from this list; openDevice derives its control map. Adding a
// driver is a single line here.
var drivers = []driverEntry{
	philipsdriver.Driver{},
	android.Driver{},
}

// matchers is the discovery view of drivers.
func matchers() []discovery.Matcher {
	m := make([]discovery.Matcher, len(drivers))
	for i, d := range drivers {
		m[i] = d
	}
	return m
}

// driverByID is the control view of drivers, keyed for openDevice.
var driverByID = func() map[device.DriverID]driverEntry {
	m := make(map[device.DriverID]driverEntry, len(drivers))
	for _, d := range drivers {
		m[d.ID()] = d
	}
	return m
}()

// openDevice resolves an alias to a controllable device through its driver.
// Whether a driver can control (vs merely discover) is discovered by asserting
// opener — the same type-assertion model capabilities use.
func openDevice(alias string) (device.Device, string, error) {
	entry, resolved, err := resolveDeviceEntry(alias)
	if err != nil {
		return nil, "", err
	}
	d, ok := driverByID[entry.Driver]
	if !ok {
		return nil, resolved, fmt.Errorf("device %q uses unknown driver %q", resolved, entry.Driver)
	}
	o, ok := d.(opener)
	if !ok {
		return nil, resolved, fmt.Errorf("%s: driver %q can discover but not control devices yet", resolved, entry.Driver)
	}
	dev, err := o.Open(entry.Address, entry.Settings)
	if err != nil {
		return nil, resolved, err
	}
	return dev, resolved, nil
}
