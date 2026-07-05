// Package android discovers devices exposing the Android TV Remote v2
// service. The protocol behind it is Google's, not brand-specific — a
// Philips, Sony, or TCL Android TV may all advertise it alongside their own
// brand's control protocol, so this lives as its own driver rather than a
// detail of internal/drivers/philips.
package android

import "github.com/chrris99/couchcli/internal/device"

// ID is this driver's identifier in candidates and config entries.
const ID device.DriverID = "android"

// ServiceAndroidTVRemote is the Android TV Remote v2 mDNS service type.
const ServiceAndroidTVRemote = "_androidtvremote2._tcp"
