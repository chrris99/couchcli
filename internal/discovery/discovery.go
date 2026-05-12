// Package discovery finds Philips smart TVs on the local network via mDNS
// and SSDP and emits Device records ready to feed into the pair flow.
package discovery

import (
	"context"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"

	"github.com/chrris99/couchcli/internal/philips"
)

// silentLogger discards library log output so it doesn't pollute CLI stdout/stderr.
var silentLogger = log.New(io.Discard, "", 0)

// mDNS service types we query.
const (
	// ServicePhilipsSecureRPC is the JointSpace RPC service advertised by
	// Philips Android TVs (2016+). HTTPS + digest auth on port 1926.
	ServicePhilipsSecureRPC = "_philipstv_s_rpc._tcp"
	// ServicePhilipsRPC is the JointSpace RPC service advertised by legacy
	// (non-Android) Philips TVs. Plain HTTP on port 1925.
	ServicePhilipsRPC = "_philipstv_rpc._tcp"
	// ServiceAndroidTVRemote is the Android TV Remote v2 service. Not
	// Philips-specific, but a Philips Android TV will advertise it.
	ServiceAndroidTVRemote = "_androidtvremote2._tcp"
)

// Source identifiers attached to each Device.
const (
	SourcePhilipsSecureRPC = "mdns:philipstv_s_rpc"
	SourcePhilipsRPC       = "mdns:philipstv_rpc"
	SourceAndroidTVRemote  = "mdns:androidtvremote2"
	SourceSSDPMediaRender  = "ssdp:mediarenderer"
)

// Device is one discovered Philips endpoint. Coverage is best-effort: the same
// physical TV may surface several times (one row per mDNS service or SSDP
// hit) so callers can see all the protocols it speaks.
type Device struct {
	Name    string            `json:"name"`
	Host    string            `json:"host,omitempty"`
	Address string            `json:"address"`
	Port    int               `json:"port"`
	Secure  bool              `json:"secure"`
	Source  string            `json:"source"`

	// Probe enrichment (optional).
	Model      string `json:"model,omitempty"`
	APIVersion int    `json:"api_version,omitempty"`

	TXT map[string]string `json:"txt,omitempty"`
}

// mdnsProfile maps an mDNS service type to a secure/source pair.
type mdnsProfile struct {
	source string
	secure bool
}

var mdnsProfiles = map[string]mdnsProfile{
	ServicePhilipsSecureRPC: {source: SourcePhilipsSecureRPC, secure: true},
	ServicePhilipsRPC:       {source: SourcePhilipsRPC, secure: false},
	// _androidtvremote2 is advertised by Philips Android TVs but the
	// pairing protocol behind it is Google's. We still surface the address
	// so it shows up in discovery output and the JointSpace probe can
	// confirm it.
	ServiceAndroidTVRemote: {source: SourceAndroidTVRemote, secure: true},
}

var defaultMDNSServices = []string{
	ServicePhilipsSecureRPC,
	ServicePhilipsRPC,
	ServiceAndroidTVRemote,
}

// Options configures a discovery run.
type Options struct {
	// Timeout per source. Defaults to 5s.
	Timeout time.Duration
	// Probe enables /6/system enrichment after the raw scan. Default false.
	Probe bool
}

// Discover scans the local network in parallel and returns a deduplicated
// list of Devices, dedup-keyed by (address, port, source).
func Discover(ctx context.Context, opts Options) ([]Device, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	var (
		mu      sync.Mutex
		devices = map[string]Device{}
		wg      sync.WaitGroup
	)

	emit := func(d Device) {
		if d.Address == "" {
			return
		}
		key := d.Address + ":" + strconv.Itoa(d.Port) + "|" + d.Source
		mu.Lock()
		devices[key] = d
		mu.Unlock()
	}

	for _, svc := range defaultMDNSServices {
		wg.Add(1)
		go func(service string) {
			defer wg.Done()
			mdnsScan(ctx, service, timeout, emit)
		}(svc)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ssdpScan(ctx, timeout, emit)
	}()

	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()
	select {
	case <-waitCh:
	case <-ctx.Done():
	}

	result := make([]Device, 0, len(devices))
	for _, d := range devices {
		result = append(result, d)
	}

	if opts.Probe && len(result) > 0 {
		enrich(ctx, result)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		if result[i].Address != result[j].Address {
			return result[i].Address < result[j].Address
		}
		return result[i].Source < result[j].Source
	})
	return result, nil
}

// enrich probes each unique address with philips.DetectEndpoint and copies
// Model + APIVersion into all rows sharing that address. Failures are
// silently ignored.
func enrich(ctx context.Context, devices []Device) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	type result struct {
		model string
		api   int
	}
	results := map[string]result{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	seen := map[string]bool{}
	for _, d := range devices {
		if d.Address == "" || seen[d.Address] {
			continue
		}
		seen[d.Address] = true
		addr := d.Address
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, info, err := philips.DetectEndpoint(probeCtx, addr)
			if err != nil {
				return
			}
			mu.Lock()
			results[addr] = result{model: info.Model, api: info.APIVersionMajor}
			mu.Unlock()
		}()
	}
	wg.Wait()

	for i := range devices {
		if r, ok := results[devices[i].Address]; ok {
			devices[i].Model = r.model
			devices[i].APIVersion = r.api
		}
	}
}

// mdnsScan runs a single mDNS query for the given service type.
func mdnsScan(ctx context.Context, service string, timeout time.Duration, emit func(Device)) {
	entries := make(chan *mdns.ServiceEntry, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for entry := range entries {
			emit(fromMDNSEntry(entry, service))
		}
	}()

	params := mdns.DefaultParams(service + ".local")
	params.Entries = entries
	params.Timeout = timeout
	params.DisableIPv6 = true
	params.WantUnicastResponse = false
	params.Logger = silentLogger

	_ = mdns.Query(params)
	close(entries)
	<-done

	_ = ctx
}

func fromMDNSEntry(entry *mdns.ServiceEntry, service string) Device {
	if entry == nil {
		return Device{}
	}
	prof := mdnsProfiles[service]
	addr := pickIPv4(entry)
	txt := parseTXT(entry.InfoFields)
	name := strings.TrimSuffix(entry.Name, "."+service+".local.")
	if name == entry.Name {
		if i := strings.Index(entry.Name, "."); i > 0 {
			name = entry.Name[:i]
		} else {
			name = entry.Name
		}
	}
	return Device{
		Name:    name,
		Host:    strings.TrimSuffix(entry.Host, "."),
		Address: addr,
		Port:    entry.Port,
		Secure:  prof.secure,
		Source:  prof.source,
		TXT:     txt,
	}
}

func pickIPv4(entry *mdns.ServiceEntry) string {
	if entry.AddrV4 != nil {
		return entry.AddrV4.String()
	}
	if entry.Addr != nil {
		if v4 := entry.Addr.To4(); v4 != nil {
			return v4.String()
		}
	}
	if entry.AddrV6 != nil {
		return net.IP(entry.AddrV6).String()
	}
	return ""
}

func parseTXT(fields []string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			out[f] = ""
			continue
		}
		out[k] = v
	}
	return out
}
