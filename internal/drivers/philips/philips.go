// Package philips drives Philips JointSpace TVs: discovery via mDNS/SSDP and
// control by adapting the internal/clients/jointspace protocol client to the
// device capability interfaces.
package philips

import (
	"strings"

	"github.com/chrris99/couchcli/internal/device"
)

// ID is this driver's identifier in candidates and config entries.
const ID device.DriverID = "philips"

// mDNS service types advertised by Philips JointSpace TVs.
const (
	// ServicePhilipsSecureRPC is advertised by Android TVs (2016+): HTTPS +
	// digest auth on port 1926.
	ServicePhilipsSecureRPC = "_philipstv_s_rpc._tcp"
	// ServicePhilipsRPC is advertised by legacy non-Android TVs: plain HTTP
	// on port 1925.
	ServicePhilipsRPC = "_philipstv_rpc._tcp"
)

// isPhilipsManufacturer matches the manufacturer strings real Philips TVs
// advertise over SSDP: "Royal Philips Electronics", "Philips Consumer
// Lifestyle", and "TP Vision" (the OEM that builds Philips-branded TVs).
func isPhilipsManufacturer(m string) bool {
	lower := strings.ToLower(strings.TrimSpace(m))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "philips") || strings.Contains(lower, "tp vision")
}
