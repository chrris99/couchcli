// Package mdns is a generic DNS-SD scanner. It queries service types and
// returns raw entries.
package mdns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	hmdns "github.com/hashicorp/mdns"
)

// Entry is one DNS-SD answer, close to the wire format.
type Entry struct {
	Instance string     // service instance name, e.g. "Living Room TV"
	Service  string     // service type that produced this entry
	Host     string     // advertised hostname, trailing dot removed
	Addr     netip.Addr // IPv4 preferred over IPv6; zero if unresolved
	Port     int
	TXT      map[string]string
}

// Options configures a scan.
type Options struct {
	Services []string
	// Timeout is the listen window per service query. Defaults to 4s.
	Timeout time.Duration
}

// silentLogger discards library log output so it doesn't pollute CLI stdout/stderr.
var silentLogger = log.New(io.Discard, "", 0)

// Scan queries all service types in parallel and returns deduplicated
// entries. Partial failure returns the entries found alongside the joined
// per-query errors, so one failed query never hides the rest.
func Scan(ctx context.Context, opts Options) ([]Entry, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 4 * time.Second
	}

	var (
		mu   sync.Mutex
		seen = map[string]Entry{}
		errs = make([]error, len(opts.Services))
		wg   sync.WaitGroup
	)
	for i, svc := range opts.Services {
		wg.Add(1)
		go func(i int, svc string) {
			defer wg.Done()
			entries, err := query(ctx, svc, timeout)
			if err != nil {
				errs[i] = fmt.Errorf("mdns %s: %w", svc, err)
				return
			}
			mu.Lock()
			for _, e := range entries {
				seen[e.key()] = e
			}
			mu.Unlock()
		}(i, svc)
	}
	wg.Wait()

	result := make([]Entry, 0, len(seen))
	for _, e := range seen {
		result = append(result, e)
	}
	return result, errors.Join(errs...)
}

func (e Entry) key() string {
	return e.Service + "|" + e.Instance + "|" + e.Addr.String() + "|" + strconv.Itoa(e.Port)
}

func query(ctx context.Context, service string, timeout time.Duration) ([]Entry, error) {
	ch := make(chan *hmdns.ServiceEntry, 32)
	var entries []Entry
	done := make(chan struct{})
	go func() {
		defer close(done)
		for raw := range ch {
			entries = append(entries, fromServiceEntry(raw, service))
		}
	}()

	params := hmdns.DefaultParams(service + ".local")
	params.Entries = ch
	params.Timeout = timeout
	params.DisableIPv6 = true
	params.WantUnicastResponse = false
	params.Logger = silentLogger

	err := hmdns.QueryContext(ctx, params)
	close(ch)
	<-done
	return entries, err
}

func fromServiceEntry(raw *hmdns.ServiceEntry, service string) Entry {
	return Entry{
		Instance: instanceName(raw.Name, service),
		Service:  service,
		Host:     strings.TrimSuffix(raw.Host, "."),
		Addr:     pickAddr(raw),
		Port:     raw.Port,
		TXT:      parseTXT(raw.InfoFields),
	}
}

// instanceName strips the "<service>.local." suffix from a full mDNS name.
// Falls back to the first label when the name doesn't match the queried
// service (some devices answer with unrelated record names).
func instanceName(name, service string) string {
	if trimmed, ok := strings.CutSuffix(name, "."+service+".local."); ok {
		return trimmed
	}
	if i := strings.Index(name, "."); i > 0 {
		return name[:i]
	}
	return name
}

func pickAddr(raw *hmdns.ServiceEntry) netip.Addr {
	if a, ok := netip.AddrFromSlice(raw.AddrV4); ok {
		return a.Unmap()
	}
	if a, ok := netip.AddrFromSlice(raw.AddrV6); ok {
		return a
	}
	return netip.Addr{}
}

func parseTXT(fields []string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		k, v, _ := strings.Cut(f, "=")
		out[k] = v
	}
	return out
}
