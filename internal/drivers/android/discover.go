package android

import (
	"context"
	"strconv"

	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/discovery"
)

// Driver discovers devices exposing the Android TV Remote v2 service. The
// zero value is ready to use. Control capabilities are not yet implemented.
type Driver struct{}

var _ discovery.Matcher = Driver{}

func (Driver) ID() device.DriverID { return ID }

func (Driver) Interest() discovery.Interest {
	return discovery.Interest{MDNSServices: []string{ServiceAndroidTVRemote}}
}

func (Driver) Match(_ context.Context, findings discovery.Findings) ([]device.Candidate, error) {
	var candidates []device.Candidate
	for _, e := range findings.MDNS {
		if e.Service != ServiceAndroidTVRemote || !e.Addr.IsValid() {
			continue
		}
		candidates = append(candidates, device.Candidate{
			Driver:  ID,
			Name:    e.Instance,
			Address: e.Addr.String(),
			Sources: []string{"mdns:" + e.Service},
			Hints:   map[string]string{"port": strconv.Itoa(e.Port)},
		})
	}
	return candidates, nil
}
