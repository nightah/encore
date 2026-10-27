package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

// publicDNSServers answer the endpoint check's lookups as the internet sees a host name, unlike this server's resolver, which may give a LAN address (split DNS).
var publicDNSServers = []string{"1.1.1.1:53", "8.8.8.8:53"}

func lookupPublic(ctx context.Context, host string) ([]string, error) {
	var errs []error

	for _, server := range publicDNSServers {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, server)
		}}

		addrs, err := r.LookupHost(ctx, host)
		if err == nil {
			return addrs, nil
		}

		errs = append(errs, err)
	}

	return nil, errors.Join(errs...)
}

func (a *API) publicRoute(ctx context.Context, host string) endpointRoute {
	r := endpointRoute{host: host}

	if _, err := netip.ParseAddr(host); err == nil {
		return r
	}

	lookup := a.opts.PublicLookup
	if lookup == nil {
		lookup = lookupPublic
	}

	addrs, err := lookup(ctx, host)
	if err != nil || len(addrs) == 0 {
		r.lookupErr = err
		if r.lookupErr == nil {
			r.lookupErr = errors.New("no addresses")
		}

		return r
	}

	r.addrs = addrs

	if local, err := net.DefaultResolver.LookupHost(ctx, host); err == nil && !sameAddrs(local, addrs) {
		r.local = local
	}

	return r
}

func sameAddrs(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)

	return slices.Equal(a, b)
}

// private reports whether every public address is one the internet can't reach.
func (r endpointRoute) private() bool {
	if len(r.addrs) == 0 {
		return false
	}

	for _, s := range r.addrs {
		ip, err := netip.ParseAddr(s)
		if err != nil {
			return false
		}

		if !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() {
			return false
		}
	}

	return true
}

// dial connects to the first public address that answers, keeping the requested port.
func (r endpointRoute) dial(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer

	if len(r.addrs) == 0 {
		return d.DialContext(ctx, network, address)
	}

	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	var errs []error

	for _, ip := range r.addrs {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip, port))
		if err == nil {
			return conn, nil
		}

		errs = append(errs, err)
	}

	return nil, errors.Join(errs...)
}

// transport returns a copy of base that connects through the route. TLS still verifies the certificate against the host name.
func (r endpointRoute) transport(base http.RoundTripper) http.RoundTripper {
	t, ok := base.(*http.Transport)
	if !ok || t == nil {
		t = http.DefaultTransport.(*http.Transport) //nolint:errcheck,forcetypeassert // the standard library's default transport
	}

	t = t.Clone()
	t.DialContext = r.dial
	t.Proxy = nil

	return t
}

// annotate notes on a check how the address was resolved, when that might surprise.
func (r endpointRoute) annotate(c *ReachabilityCheck) {
	switch {
	case r.private():
		c.Level = "fail"
		c.Detail = fmt.Sprintf("public DNS resolves %s to a private address (%s), which Alexa can't reach", r.host, strings.Join(r.addrs, ", "))
	case r.lookupErr != nil:
		c.Note = "Public DNS couldn't be asked, so this used this server's own DNS, which may not be what Alexa sees."
	case len(r.local) > 0:
		c.Note = fmt.Sprintf("This server resolves %s to %s.\nThe check used public DNS, which resolves it to %s, as Alexa sees it.", r.host, strings.Join(r.local, ", "), strings.Join(r.addrs, ", "))
	}
}

// tls12Check fails the certificate check when the endpoint refuses TLS 1.2: Alexa's client offers TLS 1.2 and nothing newer.
func (a *API) tls12Check(ctx context.Context, c *ReachabilityCheck, base *url.URL, route endpointRoute) {
	if base.Scheme != "https" {
		return
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if t, ok := a.opts.HTTPClient.Transport.(*http.Transport); ok && t.TLSClientConfig != nil {
		cfg = t.TLSClientConfig.Clone()
	}

	cfg.ServerName = base.Hostname()
	cfg.MinVersion, cfg.MaxVersion = tls.VersionTLS12, tls.VersionTLS12

	port := base.Port()
	if port == "" {
		port = "443"
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	raw, err := route.dial(ctx, "tcp", net.JoinHostPort(base.Hostname(), port))
	if err != nil {
		return
	}

	conn := tls.Client(raw, cfg)
	defer conn.Close()

	if err := conn.HandshakeContext(ctx); err == nil {
		return
	}

	c.Level = "fail"
	c.Detail = "the endpoint refuses TLS 1.2, the only version Alexa uses; Alexa's requests won't reach Encore"
	c.Note = "Behind Cloudflare? Set Minimum TLS Version (SSL/TLS, Edge Certificates) to 1.2 or lower."
}
