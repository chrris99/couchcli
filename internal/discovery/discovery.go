// Package discovery sweeps the local network once with generic scanners
// (mdns, ssdp subpackages) and hands the raw findings to driver Matchers,
// which claim the devices they recognize as Candidates.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/chrris99/couchcli/internal/device"
	"github.com/chrris99/couchcli/internal/discovery/mdns"
	"github.com/chrris99/couchcli/internal/discovery/ssdp"
)

// Interest declares which network signals a driver wants in the sweep.
type Interest struct {
	MDNSServices []string
	SSDPTargets  []string
}

// SSDPFinding pairs a search response with its fetched descriptor.
type SSDPFinding struct {
	Response ssdp.Response
	// Description is the zero value when the descriptor fetch failed;
	// matchers should treat FriendlyName/Manufacturer as unknown then.
	Description ssdp.Description
}

// Findings is everything one sweep saw. Every matcher receives the full set
// and must filter for its own devices — findings are shared, not routed.
type Findings struct {
	MDNS []mdns.Entry
	SSDP []SSDPFinding
}

// Matcher is implemented by drivers that can recognize their devices in a
// sweep. Match may probe claimed addresses for confirmation or enrichment,
// and returns one Candidate per physical device.
type Matcher interface {
	Interest() Interest
	Match(ctx context.Context, findings Findings) ([]device.Candidate, error)
}

// Options configures a sweep.
type Options struct {
	// Timeout is the scanners' response listen window, not an overall bound.
	// Defaults to 4s; cancel ctx to abort early.
	Timeout time.Duration
}

const descFetchTimeout = 3 * time.Second

// Sweep scans the network once for the union of all matchers' interests,
// then lets every matcher claim candidates from the findings. Scan and match
// failures are joined and returned alongside whatever was found — partial
// results are normal on flaky home networks.
func Sweep(ctx context.Context, opts Options, matchers ...Matcher) ([]device.Candidate, error) {
	var services, targets []string
	for _, m := range matchers {
		in := m.Interest()
		services = appendMissing(services, in.MDNSServices...)
		targets = appendMissing(targets, in.SSDPTargets...)
	}

	findings, err := scan(ctx, opts, services, targets)

	var (
		mu         sync.Mutex
		candidates []device.Candidate
		errs       = []error{err}
		wg         sync.WaitGroup
	)
	for _, m := range matchers {
		wg.Go(func() {
			claimed, merr := m.Match(ctx, findings)
			mu.Lock()
			defer mu.Unlock()
			candidates = append(candidates, claimed...)
			if merr != nil {
				errs = append(errs, fmt.Errorf("match %T: %w", m, merr))
			}
		})
	}
	wg.Wait()

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Address != candidates[j].Address {
			return candidates[i].Address < candidates[j].Address
		}
		return candidates[i].Driver < candidates[j].Driver
	})
	return candidates, errors.Join(errs...)
}

// scan runs both scanners in parallel and fetches each unique SSDP
// descriptor once, so multiple matchers never trigger duplicate traffic.
func scan(ctx context.Context, opts Options, services, targets []string) (Findings, error) {
	var (
		f       Findings
		mdnsErr error
		ssdpErr error
		wg      sync.WaitGroup
	)

	if len(services) > 0 {
		wg.Go(func() {
			f.MDNS, mdnsErr = mdns.Scan(ctx, mdns.Options{Services: services, Timeout: opts.Timeout})
		})
	}
	if len(targets) > 0 {
		wg.Go(func() {
			responses, err := ssdp.Search(ctx, ssdp.Options{Targets: targets, Timeout: opts.Timeout})
			f.SSDP = fetchDescriptions(ctx, responses)
			ssdpErr = err
		})
	}
	wg.Wait()
	return f, errors.Join(mdnsErr, ssdpErr)
}

// fetchDescriptions fetches each unique descriptor location once; devices
// answer multiple search targets with the same descriptor URL.
func fetchDescriptions(ctx context.Context, responses []ssdp.Response) []SSDPFinding {
	byLocation := map[string]ssdp.Description{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range responses {
		if _, seen := byLocation[r.Location]; seen {
			continue
		}
		byLocation[r.Location] = ssdp.Description{}
		loc := r.Location
		wg.Go(func() {
			fetchCtx, cancel := context.WithTimeout(ctx, descFetchTimeout)
			defer cancel()
			desc, err := ssdp.FetchDescription(fetchCtx, loc)
			if err != nil {
				return
			}
			mu.Lock()
			byLocation[loc] = desc
			mu.Unlock()
		})
	}
	wg.Wait()

	findings := make([]SSDPFinding, len(responses))
	for i, r := range responses {
		findings[i] = SSDPFinding{Response: r, Description: byLocation[r.Location]}
	}
	return findings
}

func appendMissing(dst []string, values ...string) []string {
	for _, v := range values {
		if !slices.Contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}
