package api

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCertificateCheck(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	a := &API{now: func() time.Time { return now }}
	base := &url.URL{Scheme: "https", Host: "encore.example.net"}

	leaf := func(org string, notAfter time.Time) *tls.ConnectionState {
		issuer := pkix.Name{CommonName: "R11"}
		if org != "" {
			issuer.Organization = []string{org}
		}

		return &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Issuer: issuer, NotAfter: notAfter}}}
	}

	urlErr := func(err error) error { return &url.Error{Op: "Post", URL: base.String(), Err: err} }

	for name, tc := range map[string]struct {
		base   *url.URL
		state  *tls.ConnectionState
		err    error
		level  string
		detail string
	}{
		"plain http":         {base: &url.URL{Scheme: "http", Host: "encore.example.net"}, state: leaf("Let's Encrypt", now.AddDate(0, 2, 0)), level: "fail", detail: "Alexa only calls HTTPS endpoints"},
		"self-signed":        {err: urlErr(x509.UnknownAuthorityError{}), level: "fail", detail: "not signed by a public certificate authority"},
		"wrong host":         {err: urlErr(x509.HostnameError{Certificate: &x509.Certificate{}, Host: "encore.example.net"}), level: "fail", detail: "does not cover encore.example.net"},
		"expired":            {err: urlErr(x509.CertificateInvalidError{Reason: x509.Expired, Detail: "it ran out"}), level: "fail", detail: "the certificate is invalid: x509: certificate has expired or is not yet valid: it ran out"},
		"no connection":      {err: errors.New("connection refused"), level: "warn", detail: "could not read the certificate"},
		"no certificates":    {state: &tls.ConnectionState{}, level: "warn", detail: "could not read the certificate"},
		"issuer with org":    {state: leaf("Let's Encrypt", now.AddDate(0, 2, 0)), level: "ok", detail: "issued by Let's Encrypt (R11), expires 2026-12-28"},
		"issuer without org": {state: leaf("", now.AddDate(0, 2, 0)), level: "ok", detail: "issued by R11, expires 2026-12-28"},
		"expires soon":       {state: leaf("Let's Encrypt", now.Add(10*24*time.Hour+time.Hour)), level: "warn", detail: "issued by Let's Encrypt (R11), expires 2026-11-07 (in 10 days)"},
		"warning boundary":   {state: leaf("", now.Add(certWarnWithin)), level: "ok", detail: "issued by R11, expires 2026-11-11"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			b := base
			if tc.base != nil {
				b = tc.base
			}

			c := a.certificateCheck(b, tc.state, tc.err)
			assert.Equal(t, "certificate", c.Name)
			assert.Equal(t, tc.level, c.Level)
			assert.Contains(t, c.Detail, tc.detail)
		})
	}
}

func TestProbeRefusesABadRequest(t *testing.T) {
	t.Parallel()

	_, _, err := probe(t.Context(), http.DefaultClient, "BAD METHOD", "http://127.0.0.1/", nil)
	require.Error(t, err)
}

// proxied answers as a reverse proxy in front of something would, recording the paths it was asked for.
type proxied struct {
	alexa, settings int

	mu    sync.Mutex
	paths []string
}

func (p *proxied) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.paths = append(p.paths, r.Method+" "+r.URL.RequestURI())
	p.mu.Unlock()

	status := p.settings
	if strings.HasSuffix(r.URL.Path, "/api/v1/alexa") {
		status = p.alexa
	}

	if status == http.StatusMovedPermanently {
		http.Redirect(w, r, "https://elsewhere.example/", status)

		return
	}

	w.WriteHeader(status)
}

func reachabilityOf(t *testing.T, h *harness, target string) (Reachability, map[string]ReachabilityCheck) {
	t.Helper()

	var res Reachability

	h.client().json(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": target, "setup_token": h.setupToken()}, http.StatusOK, &res)

	out := map[string]ReachabilityCheck{}
	for _, c := range res.Checks {
		out[c.Name] = c
	}

	require.Len(t, out, 3, "every check is reported once: %+v", res.Checks)

	return res, out
}

func TestReachabilityOutcomes(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	for name, tc := range map[string]struct {
		alexa, settings int
		alexaLevel      string
		alexaDetail     string
		adminDetail     string
		adminNote       string
	}{
		"encore behind a proxy": {alexa: 400, settings: 401, alexaLevel: "ok", alexaDetail: "refused an unsigned request", adminDetail: "web interface and admin API are reachable", adminNote: "Every admin route needs a sign-in."},
		"only alexa forwarded":  {alexa: 400, settings: 404, alexaLevel: "ok", adminDetail: "only the Alexa endpoint is reachable"},
		"admin blocked":         {alexa: 400, settings: 403, alexaLevel: "ok", adminDetail: "only the Alexa endpoint is reachable"},
		"proxy without backend": {alexa: 503, settings: 503, alexaLevel: "warn", alexaDetail: "answered 503", adminDetail: "other routes answered 503, not the admin API"},
		"something else":        {alexa: 200, settings: 200, alexaLevel: "fail", alexaDetail: "expected Encore's 400 for an unsigned request but got 200", adminDetail: "other routes answered 200"},
		"a redirect":            {alexa: 301, settings: 404, alexaLevel: "fail", alexaDetail: "got 301"},
	} {
		t.Run(name, func(t *testing.T) {
			p := &proxied{alexa: tc.alexa, settings: tc.settings}
			srv := httptest.NewServer(p)
			t.Cleanup(srv.Close)

			_, checks := reachabilityOf(t, h, srv.URL)
			assert.Equal(t, tc.alexaLevel, checks["alexa_endpoint"].Level)
			assert.Contains(t, checks["alexa_endpoint"].Detail, tc.alexaDetail)
			assert.Empty(t, checks["alexa_endpoint"].Note, "an IP address needs no DNS")
			assert.Equal(t, "fail", checks["certificate"].Level, "plain HTTP never passes")
			assert.Equal(t, "ok", checks["admin_exposed"].Level, "informational")
			assert.Contains(t, checks["admin_exposed"].Detail, tc.adminDetail)
			assert.Equal(t, tc.adminNote, checks["admin_exposed"].Note)

			p.mu.Lock()
			defer p.mu.Unlock()

			assert.Equal(t, []string{"POST /api/v1/alexa", "GET /api/v1/settings"}, p.paths, "redirects aren't followed")
		})
	}
}

func TestReachabilityNormalisesTheURL(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	p := &proxied{alexa: 400, settings: 401}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	res, _ := reachabilityOf(t, h, "  "+srv.URL+"/encore/?utm=x#top  ")
	assert.Equal(t, srv.URL+"/encore", res.URL, "the query, fragment and trailing slash are dropped")

	p.mu.Lock()
	defer p.mu.Unlock()

	assert.Equal(t, []string{"POST /encore/api/v1/alexa", "GET /encore/api/v1/settings"}, p.paths, "a base path is kept")
}

func TestReachabilityWhenNothingAnswers(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	_, checks := reachabilityOf(t, h, "https://"+addr)
	assert.Equal(t, "fail", checks["alexa_endpoint"].Level)
	assert.Contains(t, checks["alexa_endpoint"].Detail, "could not connect")
	assert.Equal(t, "warn", checks["certificate"].Level, "there was no certificate to read")
	assert.Equal(t, "only the Alexa endpoint is reachable from outside", checks["admin_exposed"].Detail)
}

func TestReachabilityUntrustedCertificate(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true}) // the default client trusts only public authorities

	srv := httptest.NewTLSServer(encoreLike())
	t.Cleanup(srv.Close)

	_, checks := reachabilityOf(t, h, srv.URL)
	assert.Equal(t, "fail", checks["alexa_endpoint"].Level)
	assert.Equal(t, "fail", checks["certificate"].Level)
	assert.Contains(t, checks["certificate"].Detail, "public certificate authority")
}

func TestReachabilityRefusesBadURLs(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.client()

	for _, u := range []string{"ftp://encore.example.net", "https://", "https://user:pw@encore.example.net", "encore.example.net", "https://[::1"} {
		p := c.do(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": u, "setup_token": h.setupToken()}).problem(t)
		require.Equal(t, http.StatusUnprocessableEntity, p.Status, u)
		require.Len(t, p.Errors, 1, u)
		assert.Equal(t, "body.url", p.Errors[0].Location, u)
	}

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": "", "setup_token": h.setupToken()}).status)
	assert.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": "https://encore.example.net"}).status, "without the setup token")
}

func TestTLS12CheckLeavesOtherChecksAlone(t *testing.T) {
	t.Parallel()

	a := &API{opts: Options{HTTPClient: &http.Client{}}}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	require.NoError(t, ln.Close())

	want := ReachabilityCheck{Name: "certificate", Level: "ok", Detail: "issued by R11"}

	for name, base := range map[string]*url.URL{
		"plain http":            {Scheme: "http", Host: "127.0.0.1:" + port},
		"nothing listens there": {Scheme: "https", Host: "encore.example.test:" + port},
	} {
		c := want
		a.tls12Check(t.Context(), &c, base, endpointRoute{host: base.Hostname(), addrs: []string{"127.0.0.1"}})
		assert.Equal(t, want, c, fmt.Sprint(name, ": a check that can't be made isn't a failure"))
	}
}
