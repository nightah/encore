package kodi

import (
	"bytes"
	"cmp"
	"context"
	"io"
	"log"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

// Config returns a connection config for the instance, preferring an IPv4 address over the mDNS host name.
func (i Instance) Config() Config {
	host := strings.TrimSuffix(i.Host, ".")

	for _, a := range i.Addrs {
		if a.To4() != nil {
			host = a.String()

			break
		}
	}

	return Config{Host: host, Port: i.Port, WebSocketPort: i.WebSocketPort}.withDefaults()
}

// Discover browses both Kodi service types at once and merges the answers for one Kodi, sorted by name. Answers belong to the same Kodi when they share a host name, or else an instance name; Kodi names each service after the device with a suffix ("Lounge jsonrpc-http", "Lounge jsonrpc-tcp"), which is dropped.
func (d Discoverer) Discover(ctx context.Context) ([]Instance, error) {
	browser := d.Browser
	if browser == nil {
		browser = MDNSBrowser{}
	}

	timeout := cmp.Or(d.Timeout, DefaultDiscoveryTimeout)

	type answer struct {
		service string
		entry   ServiceEntry
	}

	var (
		mu      sync.Mutex
		answers []answer
		wg      sync.WaitGroup
		errs    = make([]error, 2)
	)

	for i, service := range []string{ServiceJSONRPC, ServiceJSONRPCHTTP} {
		entries := make(chan ServiceEntry)
		done := make(chan struct{})

		go func() {
			defer close(done)

			for e := range entries {
				mu.Lock()
				answers = append(answers, answer{service, e})
				mu.Unlock()
			}
		}()

		wg.Go(func() {
			errs[i] = browser.Browse(ctx, service, timeout, entries)
			close(entries)
			<-done
		})
	}

	wg.Wait()

	if errs[0] != nil && errs[1] != nil {
		return nil, errs[0]
	}

	byKey := map[string]*Instance{}

	for _, a := range answers {
		key := strings.ToLower(strings.TrimSuffix(a.entry.Host, "."))
		if key == "" {
			key = "name:" + a.entry.Instance
		}

		inst := byKey[key]
		if inst == nil {
			inst = &Instance{Name: deviceName(a.entry.Instance), Host: a.entry.Host}
			byKey[key] = inst
		}

		for _, ip := range a.entry.Addrs {
			if !slices.ContainsFunc(inst.Addrs, ip.Equal) {
				inst.Addrs = append(inst.Addrs, ip)
			}
		}

		if a.service == ServiceJSONRPC {
			inst.WebSocketPort = a.entry.Port
		} else {
			inst.Port = a.entry.Port
		}
	}

	out := make([]Instance, 0, len(byKey))
	for _, inst := range byKey {
		slices.SortFunc(inst.Addrs, compareIP)
		out = append(out, *inst)
	}

	slices.SortFunc(out, func(a, b Instance) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}

var serviceSuffixes = []string{" jsonrpc-http", " jsonrpc-tcp"}

func deviceName(instance string) string {
	for _, s := range serviceSuffixes {
		if name, ok := strings.CutSuffix(instance, s); ok && name != "" {
			return name
		}
	}

	return instance
}

// IPv4 sorts before IPv6.
func compareIP(a, b net.IP) int {
	a4, b4 := a.To4() != nil, b.To4() != nil
	if a4 != b4 {
		if a4 {
			return -1
		}

		return 1
	}

	return bytes.Compare(a.To16(), b.To16())
}

// Browse implements Browser.
func (m MDNSBrowser) Browse(ctx context.Context, service string, timeout time.Duration, entries chan<- ServiceEntry) error {
	// The library drops answers rather than block, so buffer generously and drain concurrently.
	raw := make(chan *mdns.ServiceEntry, 64)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for e := range raw {
			// Responders send every record they have, so answers for other services (Chromecast, printers...) arrive too.
			if isService(e.Name, service) {
				entries <- convertEntry(e, service)
			}
		}
	}()

	err := mdns.QueryContext(ctx, &mdns.QueryParam{
		Service:   service,
		Domain:    "local",
		Timeout:   timeout,
		Interface: m.Interface,
		Entries:   raw,
		Logger:    log.New(io.Discard, "", 0),
	})

	close(raw)
	<-done

	return err
}

// full is an instance name such as "Kodi._xbmc-jsonrpc._tcp.local.".
func isService(full, service string) bool {
	return strings.Contains(strings.ToLower(full), "."+service+".")
}

func convertEntry(e *mdns.ServiceEntry, service string) ServiceEntry {
	out := ServiceEntry{Instance: instanceName(e.Name, service), Host: e.Host, Port: e.Port}

	if e.AddrV4 != nil {
		out.Addrs = append(out.Addrs, e.AddrV4)
	}

	if e.AddrV6IPAddr != nil {
		out.Addrs = append(out.Addrs, e.AddrV6IPAddr.IP)
	}

	return out
}

// Strips the service and domain from a name such as "Kodi\ (lounge)._xbmc-jsonrpc._tcp.local." and undoes DNS escaping.
func instanceName(full, service string) string {
	name := full
	if i := strings.Index(name, "."+service); i >= 0 {
		name = name[:i]
	}

	var b strings.Builder

	for i := 0; i < len(name); i++ {
		c := name[i]
		if c != '\\' || i+1 >= len(name) {
			b.WriteByte(c)

			continue
		}

		// \DDD is a decimal byte, anything else a literal.
		if i+3 < len(name) && isDigit(name[i+1]) && isDigit(name[i+2]) && isDigit(name[i+3]) {
			b.WriteByte((name[i+1]-'0')*100 + (name[i+2]-'0')*10 + (name[i+3] - '0'))
			i += 3
		} else {
			b.WriteByte(name[i+1])
			i++
		}
	}

	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
