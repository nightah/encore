package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
