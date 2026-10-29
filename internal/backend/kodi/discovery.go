package kodi

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
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

	entries, err := browser.Browse(ctx, []string{ServiceJSONRPC, ServiceJSONRPCHTTP}, cmp.Or(d.Timeout, DefaultDiscoveryTimeout))
	if err != nil {
		return nil, err
	}

	byKey := map[string]*Instance{}

	for _, e := range entries {
		key := strings.ToLower(strings.TrimSuffix(e.Host, "."))
		if key == "" {
			key = "name:" + e.Instance
		}

		inst := byKey[key]
		if inst == nil {
			inst = &Instance{Name: deviceName(e.Instance), Host: e.Host}
			byKey[key] = inst
		}

		for _, ip := range e.Addrs {
			if !slices.ContainsFunc(inst.Addrs, ip.Equal) {
				inst.Addrs = append(inst.Addrs, ip)
			}
		}

		if e.Service == ServiceJSONRPC {
			inst.WebSocketPort = e.Port
		} else {
			inst.Port = e.Port
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

// Browse implements Browser. It asks for every service type in one query from the mDNS port, so responders and repeaters answer by multicast, asks again halfway through (adding the addresses still missing), and resolves services from everything heard once the time is up.
func (m MDNSBrowser) Browse(ctx context.Context, services []string, timeout time.Duration) ([]ServiceEntry, error) {
	ifaces, err := m.interfaces()
	if err != nil {
		return nil, err
	}

	browseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		conns []*mdnsConn
		errs  []error
	)

	for _, family := range []string{"udp4", "udp6"} {
		c, err := listenMDNS(browseCtx, family, ifaces)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		conns = append(conns, c)
	}

	if len(conns) == 0 {
		return nil, errors.Join(errs...)
	}

	packets := make(chan []byte, 64)

	var wg sync.WaitGroup

	for _, c := range conns {
		wg.Go(func() { c.read(browseCtx, packets) })
	}

	// Cancelling frees a read waiting to hand over a packet, and closing the sockets ends the rest.
	defer func() {
		cancel()

		for _, c := range conns {
			_ = c.conn.Close()
		}

		wg.Wait()
	}()

	answers := newMDNSAnswers()

	query := func() error {
		msg, err := answers.query(services)
		if err != nil {
			return err
		}

		sent := false

		for _, c := range conns {
			if c.send(msg, ifaces) {
				sent = true
			}
		}

		if !sent {
			return errMDNSSend
		}

		return nil
	}

	if err := query(); err != nil {
		return nil, err
	}

	again := time.NewTimer(timeout / 2)
	defer again.Stop()

	for {
		select {
		case p := <-packets:
			answers.add(p)
		case <-again.C:
			_ = query()
		case <-browseCtx.Done():
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			return answers.entries(services), nil
		}
	}
}

func (m MDNSBrowser) interfaces() ([]net.Interface, error) {
	if m.Interface != nil {
		return []net.Interface{*m.Interface}, nil
	}

	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	out := all[:0]

	for _, iface := range all {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagMulticast != 0 && iface.Flags&net.FlagLoopback == 0 {
			out = append(out, iface)
		}
	}

	if len(out) == 0 {
		return nil, errNoMulticastInterface
	}

	return out, nil
}

// listenMDNS opens family's socket on the mDNS port, shared with any other mDNS software on the host, and joins the group on every interface that allows it. When the port can't be had it falls back to a random one, from which responders answer by unicast instead (RFC 6762 §6.7).
func listenMDNS(ctx context.Context, family string, ifaces []net.Interface) (*mdnsConn, error) {
	group := mdnsGroupV4
	if family == "udp6" {
		group = mdnsGroupV6
	}

	lc := net.ListenConfig{Control: reuseMDNSPort}

	conn, err := lc.ListenPacket(ctx, family, net.JoinHostPort("", strconv.Itoa(mdnsPort)))

	bound := err == nil
	if !bound {
		if conn, err = lc.ListenPacket(ctx, family, ":0"); err != nil {
			return nil, err
		}
	}

	c := &mdnsConn{conn: conn, group: &net.UDPAddr{IP: group, Port: mdnsPort}}

	joined := 0

	if family == "udp6" {
		p := ipv6.NewPacketConn(conn)
		_ = p.SetMulticastHopLimit(255)
		c.setInterface = p.SetMulticastInterface

		for i := range ifaces {
			if bound && p.JoinGroup(&ifaces[i], c.group) == nil {
				joined++
			}
		}
	} else {
		p := ipv4.NewPacketConn(conn)
		_ = p.SetMulticastTTL(255)
		c.setInterface = p.SetMulticastInterface

		for i := range ifaces {
			if bound && p.JoinGroup(&ifaces[i], c.group) == nil {
				joined++
			}
		}
	}

	if bound && joined == 0 {
		_ = conn.Close()

		return nil, fmt.Errorf("%w (%s)", errMDNSJoin, group)
	}

	return c, nil
}

// send writes msg to the group on every interface, reporting whether any write succeeded.
func (c *mdnsConn) send(msg []byte, ifaces []net.Interface) bool {
	sent := false

	for i := range ifaces {
		if c.setInterface(&ifaces[i]) != nil {
			continue
		}

		if _, err := c.conn.WriteTo(msg, c.group); err == nil {
			sent = true
		}
	}

	return sent
}

func (c *mdnsConn) read(ctx context.Context, packets chan<- []byte) {
	buf := make([]byte, 9000)

	for {
		n, _, err := c.conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}

			continue
		}

		select {
		case packets <- bytes.Clone(buf[:n]):
		case <-ctx.Done():
			return
		}
	}
}

func newMDNSAnswers() *mdnsAnswers {
	return &mdnsAnswers{instances: map[string]map[string]bool{}, srv: map[string]*dns.SRV{}, addrs: map[string][]net.IP{}}
}

// query packs a query asking for each service type's instances and for the addresses of hosts already named by an SRV record but not yet resolved.
func (a *mdnsAnswers) query(services []string) ([]byte, error) {
	msg := new(dns.Msg)
	msg.RecursionDesired = false

	for _, s := range services {
		msg.Question = append(msg.Question, dns.Question{Name: dns.Fqdn(s + "." + mdnsDomain), Qtype: dns.TypePTR, Qclass: dns.ClassINET})
	}

	var hosts []string

	for _, srv := range a.srv {
		host := strings.ToLower(srv.Target)
		if len(a.addrs[host]) == 0 && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}

	slices.Sort(hosts)

	for _, h := range hosts {
		msg.Question = append(msg.Question,
			dns.Question{Name: h, Qtype: dns.TypeA, Qclass: dns.ClassINET},
			dns.Question{Name: h, Qtype: dns.TypeAAAA, Qclass: dns.ClassINET})
	}

	return msg.Pack()
}

// add records every PTR, SRV, A and AAAA record in an answer. Queries, including other hosts' queries carrying known answers, are ignored.
func (a *mdnsAnswers) add(packet []byte) {
	msg := new(dns.Msg)
	if msg.Unpack(packet) != nil || !msg.Response {
		return
	}

	for _, rr := range slices.Concat(msg.Answer, msg.Ns, msg.Extra) {
		name := strings.ToLower(rr.Header().Name)

		switch r := rr.(type) {
		case *dns.PTR:
			// A zero TTL is a goodbye (RFC 6762 §10.1): the instance has left.
			if r.Hdr.Ttl == 0 {
				delete(a.instances[name], r.Ptr)
				delete(a.srv, strings.ToLower(r.Ptr))

				continue
			}

			if a.instances[name] == nil {
				a.instances[name] = map[string]bool{}
			}

			a.instances[name][r.Ptr] = true
		case *dns.SRV:
			if r.Hdr.Ttl == 0 {
				delete(a.srv, name)

				continue
			}

			a.srv[name] = r
		case *dns.A:
			a.addAddr(name, r.A)
		case *dns.AAAA:
			a.addAddr(name, r.AAAA)
		}
	}
}

func (a *mdnsAnswers) addAddr(host string, ip net.IP) {
	if !slices.ContainsFunc(a.addrs[host], ip.Equal) {
		a.addrs[host] = append(a.addrs[host], ip)
	}
}

// entries resolves every instance of services that has an SRV record, sorted by service and instance. Instances come from PTR records, or from SRV records alone when a responder sent those without the PTR.
func (a *mdnsAnswers) entries(services []string) []ServiceEntry {
	var out []ServiceEntry

	for _, service := range services {
		// Keyed by lower-cased name, as responders needn't keep a name's case.
		names := map[string]string{}

		for name := range a.instances[strings.ToLower(dns.Fqdn(service+"."+mdnsDomain))] {
			names[strings.ToLower(name)] = name
		}

		for key, srv := range a.srv {
			if isService(key, service) {
				names[key] = srv.Hdr.Name
			}
		}

		for _, key := range slices.Sorted(maps.Keys(names)) {
			srv := a.srv[key]
			if srv == nil {
				continue
			}

			name := names[key]

			out = append(out, ServiceEntry{
				Service:  service,
				Instance: instanceName(name, service),
				Host:     srv.Target,
				Addrs:    slices.Clone(a.addrs[strings.ToLower(srv.Target)]),
				Port:     int(srv.Port),
			})
		}
	}

	return out
}

// full is an instance name such as "Kodi._xbmc-jsonrpc._tcp.local.".
func isService(full, service string) bool {
	return strings.Contains(strings.ToLower(full), "."+service+".")
}

// Strips the service and domain from a name such as "Kodi\ (lounge)._xbmc-jsonrpc._tcp.local." and undoes DNS escaping.
func instanceName(full, service string) string {
	name := full
	if i := strings.Index(asciiLower(name), "."+strings.ToLower(service)); i >= 0 {
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

// asciiLower lower-cases only ASCII letters, keeping byte offsets the same as s; DNS compares names that way (RFC 4343).
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}

	return string(b)
}
