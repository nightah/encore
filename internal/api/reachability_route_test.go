package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

// encoreLike answers like Encore behind a proxy: 400 for an unsigned Alexa request.
func encoreLike() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/alexa" {
			http.Error(w, "bad request", http.StatusBadRequest)

			return
		}

		w.WriteHeader(http.StatusUnauthorized)
	})
}

// ecdsaServer serves TLS with only an ECDSA certificate, as Cloudflare's proxy does, from minVersion up, and returns a client that trusts it.
func ecdsaServer(t *testing.T, minVersion uint16) (*httptest.Server, *http.Client) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	srv := httptest.NewUnstartedServer(encoreLike())
	srv.TLS = &tls.Config{MinVersion: minVersion, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return srv, &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}}}
}

func checkReachability(t *testing.T, h *harness, url string) map[string]ReachabilityCheck {
	t.Helper()

	var res Reachability

	h.client().json(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": url, "setup_token": h.setupToken()}, http.StatusOK, &res)

	out := map[string]ReachabilityCheck{}
	for _, c := range res.Checks {
		out[c.Name] = c
	}

	return out
}

// Alexa offers ECDSA cipher suites and signature algorithms (seen in a captured ClientHello), so an ECDSA-only endpoint is fine.
func TestReachabilityAcceptsECDSACertificate(t *testing.T) {
	srv, client := ecdsaServer(t, tls.VersionTLS12)
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.HTTPClient = client }})

	checks := checkReachability(t, h, srv.URL)
	assert.Equal(t, "ok", checks["alexa_endpoint"].Level)
	assert.Equal(t, "ok", checks["certificate"].Level, "%+v", checks["certificate"])
}

// Alexa's ClientHello offers TLS 1.2 and nothing newer.
func TestReachabilityRejectsTLS13Only(t *testing.T) {
	srv, client := ecdsaServer(t, tls.VersionTLS13)
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.HTTPClient = client }})

	checks := checkReachability(t, h, srv.URL)
	assert.Equal(t, "ok", checks["alexa_endpoint"].Level, "this server's own client speaks TLS 1.3")
	assert.Equal(t, "fail", checks["certificate"].Level)
	assert.Contains(t, checks["certificate"].Detail, "refuses TLS 1.2")
	assert.Contains(t, checks["certificate"].Note, "Minimum TLS Version")
}

func TestReachabilityAcceptsRSACertificate(t *testing.T) {
	srv := httptest.NewTLSServer(encoreLike()) // httptest's certificate is RSA
	t.Cleanup(srv.Close)

	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.HTTPClient = srv.Client() }})

	checks := checkReachability(t, h, srv.URL)
	assert.Equal(t, "ok", checks["certificate"].Level, "%+v", checks["certificate"])
	assert.Equal(t, "ok", checks["alexa_endpoint"].Level)
}

func TestReachabilityPrivateAddressFromPublicDNS(t *testing.T) {
	srv := httptest.NewServer(encoreLike())
	t.Cleanup(srv.Close)

	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)

	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.PublicLookup = func(context.Context, string) ([]string, error) { return []string{"127.0.0.1"}, nil }
	}})

	checks := checkReachability(t, h, "http://encore.example.test:"+port)
	assert.Equal(t, "fail", checks["alexa_endpoint"].Level)
	assert.Contains(t, checks["alexa_endpoint"].Detail, "private address (127.0.0.1)")
}

func TestRouteNotesSplitDNS(t *testing.T) {
	c := ReachabilityCheck{Name: "alexa_endpoint", Level: "ok"}
	endpointRoute{host: "dev.example.net", addrs: []string{"203.0.113.7"}, local: []string{"10.10.50.2"}}.annotate(&c)

	assert.Equal(t, "ok", c.Level)
	assert.Equal(t, "This server resolves dev.example.net to 10.10.50.2.\nThe check used public DNS, which resolves it to 203.0.113.7, as Alexa sees it.", c.Note)
}

func TestSameAddrs(t *testing.T) {
	t.Parallel()

	a := []string{"203.0.113.7", "2001:db8::7"}
	b := []string{"2001:db8::7", "203.0.113.7"}

	assert.True(t, sameAddrs(a, b), "order doesn't matter")
	assert.Equal(t, []string{"203.0.113.7", "2001:db8::7"}, a, "the inputs aren't sorted in place")
	assert.True(t, sameAddrs(nil, []string{}))
	assert.False(t, sameAddrs(a, a[:1]))
	assert.False(t, sameAddrs([]string{"203.0.113.7", "203.0.113.7"}, []string{"203.0.113.7", "203.0.113.8"}))
}

func TestRoutePrivate(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		addrs []string
		want  bool
	}{
		"no answer":       {nil, false},
		"lan":             {[]string{"192.168.1.20", "10.0.0.5", "172.16.0.1"}, true},
		"loopback":        {[]string{"127.0.0.1", "::1"}, true},
		"link-local":      {[]string{"169.254.10.1", "fe80::1"}, true},
		"unique local v6": {[]string{"fd12:3456::1"}, true},
		"unspecified":     {[]string{"0.0.0.0"}, true},
		"public":          {[]string{"203.0.113.7"}, false},
		"one public":      {[]string{"192.168.1.20", "2606:4700::1111"}, false},
		"not an address":  {[]string{"192.168.1.20", "encore.example.net"}, false},
	} {
		assert.Equal(t, tc.want, endpointRoute{addrs: tc.addrs}.private(), name)
	}
}

func TestPublicRoute(t *testing.T) {
	t.Parallel()

	lookups := func(addrs []string, err error) *API {
		return &API{opts: Options{PublicLookup: func(_ context.Context, host string) ([]string, error) {
			if host != "localhost" && host != "encore.example.test" {
				t.Errorf("looked up %q", host)
			}

			return addrs, err
		}}}
	}

	t.Run("an address needs no lookup", func(t *testing.T) {
		t.Parallel()

		a := &API{opts: Options{PublicLookup: func(context.Context, string) ([]string, error) {
			t.Error("an IP address was looked up")

			return nil, nil
		}}}

		assert.Equal(t, endpointRoute{host: "2001:db8::1"}, a.publicRoute(t.Context(), "2001:db8::1"))
	})

	t.Run("lookup fails", func(t *testing.T) {
		t.Parallel()

		r := lookups(nil, errors.New("i/o timeout")).publicRoute(t.Context(), "encore.example.test")
		require.EqualError(t, r.lookupErr, "i/o timeout")
		assert.Empty(t, r.addrs, "the transport resolves the host itself")
	})

	t.Run("no addresses", func(t *testing.T) {
		t.Parallel()

		r := lookups([]string{}, nil).publicRoute(t.Context(), "encore.example.test")
		require.EqualError(t, r.lookupErr, "no addresses")
		assert.Empty(t, r.addrs)
	})

	local, err := net.DefaultResolver.LookupHost(t.Context(), "localhost")
	require.NoError(t, err)

	t.Run("split dns", func(t *testing.T) {
		t.Parallel()

		r := lookups([]string{"203.0.113.7"}, nil).publicRoute(t.Context(), "localhost")
		require.NoError(t, r.lookupErr)
		assert.Equal(t, []string{"203.0.113.7"}, r.addrs)
		assert.ElementsMatch(t, local, r.local, "what this server sees is noted when it differs")
	})

	t.Run("same answer", func(t *testing.T) {
		t.Parallel()

		reversed := slices.Clone(local)
		slices.Reverse(reversed)

		r := lookups(reversed, nil).publicRoute(t.Context(), "localhost")
		assert.Equal(t, reversed, r.addrs)
		assert.Empty(t, r.local, "nothing to note when both agree")
	})
}

// fakeDNS answers A queries for every name with addr, or SERVFAIL when addr is empty.
func fakeDNS(t *testing.T, addr string) string {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pc.Close() })

	go func() {
		buf := make([]byte, 512)

		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}

			var p dnsmessage.Parser

			hdr, err := p.Start(buf[:n])
			if err != nil {
				continue
			}

			q, err := p.Question()
			if err != nil {
				continue
			}

			res := dnsmessage.Message{Header: dnsmessage.Header{ID: hdr.ID, Response: true, Authoritative: true}, Questions: []dnsmessage.Question{q}}

			switch {
			case addr == "":
				res.RCode = dnsmessage.RCodeServerFailure
			case q.Type == dnsmessage.TypeA:
				res.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.AResource{A: netip.MustParseAddr(addr).As4()},
				}}
			}

			out, err := res.Pack()
			if err != nil {
				continue
			}

			_, _ = pc.WriteTo(out, from)
		}
	}()

	return pc.LocalAddr().String()
}

// TestLookupPublic changes publicDNSServers, so it doesn't run in parallel.
func TestLookupPublic(t *testing.T) {
	saved := publicDNSServers
	t.Cleanup(func() { publicDNSServers = saved })

	failing, answering := fakeDNS(t, ""), fakeDNS(t, "203.0.113.7")

	publicDNSServers = []string{failing, answering}

	addrs, err := lookupPublic(t.Context(), "encore.example.test")
	require.NoError(t, err)
	assert.Equal(t, []string{"203.0.113.7"}, addrs, "the next server is asked when one fails")

	publicDNSServers = []string{failing, failing}

	_, err = lookupPublic(t.Context(), "encore.example.test")
	require.Error(t, err)

	var dnsErr *net.DNSError

	require.ErrorAs(t, err, &dnsErr)
	assert.Len(t, strings.Split(err.Error(), "\n"), 2, "each server's error is kept")
}

func TestRouteDial(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			_ = c.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	_, closedPort, err := net.SplitHostPort(closed.Addr().String())
	require.NoError(t, err)
	require.NoError(t, closed.Close())

	t.Run("public addresses replace the host and keep the port", func(t *testing.T) {
		t.Parallel()

		r := endpointRoute{host: "encore.example.test", addrs: []string{"127.0.0.2", "127.0.0.1"}}

		conn, err := r.dial(t.Context(), "tcp", net.JoinHostPort("encore.example.test", port))
		require.NoError(t, err, "nothing listens on 127.0.0.2, so the next address is tried")

		assert.Equal(t, ln.Addr().String(), conn.RemoteAddr().String())
		require.NoError(t, conn.Close())
	})

	t.Run("without public addresses the host is dialled", func(t *testing.T) {
		t.Parallel()

		conn, err := endpointRoute{}.dial(t.Context(), "tcp", ln.Addr().String())
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	})

	t.Run("every address fails", func(t *testing.T) {
		t.Parallel()

		_, err := endpointRoute{addrs: []string{"127.0.0.1", "127.0.0.1"}}.dial(t.Context(), "tcp", "encore.example.test:"+closedPort)
		require.Error(t, err)
		assert.Len(t, strings.Split(err.Error(), "\n"), 2, "each address's error is kept")
	})

	t.Run("an address without a port", func(t *testing.T) {
		t.Parallel()

		_, err := endpointRoute{addrs: []string{"127.0.0.1"}}.dial(t.Context(), "tcp", "encore.example.test")
		require.Error(t, err)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRouteTransport(t *testing.T) {
	t.Parallel()

	r := endpointRoute{addrs: []string{"127.0.0.1"}}

	base := &http.Transport{TLSClientConfig: &tls.Config{ServerName: "kept", MinVersion: tls.VersionTLS12}, Proxy: http.ProxyFromEnvironment}

	got, ok := r.transport(base).(*http.Transport)
	require.True(t, ok)
	assert.NotSame(t, base, got, "a copy")
	assert.Equal(t, "kept", got.TLSClientConfig.ServerName, "TLS settings carry over")
	assert.Nil(t, got.Proxy, "no proxy: the check connects where Alexa would")
	assert.NotNil(t, got.DialContext)
	assert.NotNil(t, base.Proxy, "the original is untouched")
	assert.Nil(t, base.DialContext)

	for name, other := range map[string]http.RoundTripper{"nil": nil, "not a transport": roundTripperFunc(nil), "nil transport": (*http.Transport)(nil)} {
		got, ok := r.transport(other).(*http.Transport)
		require.True(t, ok, name)
		assert.NotSame(t, http.DefaultTransport, got, name)
		assert.Nil(t, got.Proxy, name)
		assert.NotNil(t, got.DialContext, name)
	}

	assert.NotNil(t, http.DefaultTransport.(*http.Transport).Proxy, "the default transport is untouched") //nolint:forcetypeassert // the standard library's
}

func TestRouteAnnotate(t *testing.T) {
	t.Parallel()

	ok := ReachabilityCheck{Name: "alexa_endpoint", Level: "ok", Detail: "fine"}

	c := ok
	endpointRoute{host: "encore.example.test", lookupErr: errors.New("timeout")}.annotate(&c)
	assert.Equal(t, "ok", c.Level)
	assert.Contains(t, c.Note, "Public DNS couldn't be asked")

	c = ok
	endpointRoute{host: "encore.example.test", addrs: []string{"10.0.0.2"}, local: []string{"10.0.0.3"}}.annotate(&c)
	assert.Equal(t, "fail", c.Level, "a private public answer fails, whatever else")
	assert.Equal(t, "public DNS resolves encore.example.test to a private address (10.0.0.2), which Alexa can't reach", c.Detail)

	c = ok
	endpointRoute{host: "encore.example.test", addrs: []string{"203.0.113.7"}}.annotate(&c)
	assert.Equal(t, ok, c, "nothing surprising, nothing noted")
}
