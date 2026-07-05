package philips

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/chrris99/couchcli/internal/clients/jointspace"
	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/discovery"
	"github.com/chrris99/couchcli/internal/discovery/ssdp"
)

// Driver discovers, and once paired controls, Philips JointSpace TVs. The
// zero value is ready to use.
type Driver struct{}

var _ discovery.Matcher = Driver{}

func (Driver) ID() device.DriverID { return ID }

// ssdpTargets: some Philips TVs only answer one of these, so both are searched.
var ssdpTargets = []string{
	"urn:schemas-upnp-org:device:MediaRenderer:1",
	"upnp:rootdevice",
}

// probeTimeout bounds the DetectEndpoint enrichment pass across all claimed
// candidates.
const probeTimeout = 3 * time.Second

func (Driver) Interest() discovery.Interest {
	return discovery.Interest{
		MDNSServices: []string{ServicePhilipsSecureRPC, ServicePhilipsRPC},
		SSDPTargets:  ssdpTargets,
	}
}

// Match claims JointSpace mDNS entries and Philips-manufactured SSDP
// findings, merges hits by address into one Candidate per TV, and probes
// each address with the JointSpace client to fill in Hints (model, API
// version, transport). Probe failures are non-fatal — the candidate is still
// returned, just without those hints.
func (Driver) Match(ctx context.Context, findings discovery.Findings) ([]device.Candidate, error) {
	m := &merger{found: map[string]device.Candidate{}}

	for _, e := range findings.MDNS {
		if e.Service != ServicePhilipsSecureRPC && e.Service != ServicePhilipsRPC {
			continue
		}
		if !e.Addr.IsValid() {
			continue
		}
		m.add(e.Addr.String(), e.Instance, "mdns:"+e.Service, map[string]string{
			"secure": strconv.FormatBool(e.Service == ServicePhilipsSecureRPC),
			"port":   strconv.Itoa(e.Port),
		})
	}

	for _, f := range findings.SSDP {
		if !isPhilipsManufacturer(f.Description.Manufacturer) {
			continue
		}
		host, port := ssdp.HostPort(f.Response.Location)
		m.add(host, f.Description.FriendlyName, "ssdp:"+f.Response.Location, map[string]string{
			"model": f.Description.ModelName,
			"port":  strconv.Itoa(port),
		})
	}

	result := m.list()
	enrich(ctx, result)
	sort.Slice(result, func(i, j int) bool { return result[i].Address < result[j].Address })
	return result, nil
}

// enrich probes each candidate's address with the JointSpace protocol client
// and copies model/API/transport into its Hints.
func enrich(ctx context.Context, candidates []device.Candidate) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for i := range candidates {
		c := &candidates[i]
		wg.Go(func() {
			_, info, err := jointspace.DetectEndpoint(probeCtx, c.Address)
			if err != nil {
				return
			}
			c.Hints["model"] = info.Model
			c.Hints["api"] = strconv.Itoa(info.APIVersionMajor)
			c.Hints["secure"] = strconv.FormatBool(info.SecuredTransport)
		})
	}
	wg.Wait()
}

// merger deduplicates candidates by address — the same TV usually shows up
// in both the mDNS and SSDP findings.
type merger struct {
	found map[string]device.Candidate
}

func (m *merger) add(addr, name, source string, hints map[string]string) {
	if addr == "" {
		return
	}
	c, ok := m.found[addr]
	if !ok {
		c = device.Candidate{Driver: ID, Address: addr, Hints: map[string]string{}}
	}
	if c.Name == "" && name != "" {
		c.Name = name
	}
	c.Sources = appendUnique(c.Sources, source)
	maps.Copy(c.Hints, hints)
	m.found[addr] = c
}

func (m *merger) list() []device.Candidate {
	out := make([]device.Candidate, 0, len(m.found))
	for _, c := range m.found {
		out = append(out, c)
	}
	return out
}

func appendUnique(sources []string, s string) []string {
	if slices.Contains(sources, s) {
		return sources
	}
	return append(sources, s)
}
