package alexa

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamestelfer/billy/pkg/alexaverify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSkillID = "amzn1.ask.skill.00000000-0000-0000-0000-000000000001"
	testCertURL = "https://s3.amazonaws.com/echo.api/echo-api-cert-12.pem"
)

var fixedNow = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// testPKI is a throwaway root and an Alexa-like leaf signed by it.
type testPKI struct {
	roots  *x509.CertPool
	leaf   *rsa.PrivateKey
	bundle []byte
}

// pkiOptions vary the leaf certificate, for the chain checks.
type pkiOptions struct {
	// host is the leaf's DNS name; empty means Alexa's.
	host string
	// notAfter ends the leaf's validity; zero means an hour after fixedNow.
	notAfter time.Time
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()

	return newTestPKIWith(t, pkiOptions{})
}

func newTestPKIWith(t *testing.T, opts pkiOptions) testPKI {
	t.Helper()

	if opts.host == "" {
		opts.host = "echo-api.amazon.com"
	}

	if opts.notAfter.IsZero() {
		opts.notAfter = fixedNow.Add(time.Hour)
	}

	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "encore test root"},
		NotBefore:             fixedNow.Add(-time.Hour),
		NotAfter:              fixedNow.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	require.NoError(t, err)
	root, err := x509.ParseCertificate(rootDER)
	require.NoError(t, err)

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: opts.host},
		DNSNames:     []string{opts.host},
		NotBefore:    fixedNow.Add(-time.Hour),
		NotAfter:     opts.notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, root, &leafKey.PublicKey, rootKey)
	require.NoError(t, err)

	roots := x509.NewCertPool()
	roots.AddCert(root)

	var bundle bytes.Buffer
	require.NoError(t, pem.Encode(&bundle, &pem.Block{Type: "CERTIFICATE", Bytes: leafDER}))

	return testPKI{roots: roots, leaf: leafKey, bundle: bundle.Bytes()}
}

func (p testPKI) sign(t *testing.T, body []byte) string {
	t.Helper()

	sum := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.leaf, crypto.SHA256, sum[:])
	require.NoError(t, err)

	return base64.StdEncoding.EncodeToString(sig)
}

// verifier returns a real alexaverify.Verifier whose certificate fetches are answered with the test bundle, whatever URL is asked for.
func (p testPKI) verifier(t *testing.T) *alexaverify.Verifier {
	t.Helper()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/x-pem-file"}},
			Body:       io.NopCloser(bytes.NewReader(p.bundle)),
		}, nil
	})}

	v, err := alexaverify.New(
		alexaverify.WithRoots(p.roots),
		alexaverify.WithClock(func() time.Time { return fixedNow }),
		alexaverify.WithHTTPClient(client),
		alexaverify.WithLogger(slog.New(slog.DiscardHandler)),
	)
	require.NoError(t, err)

	return v
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func envelope(skillID string, ts time.Time) []byte {
	return fmt.Appendf(nil, `{"version":"1.0","context":{"System":{"application":{"applicationId":%q}}},`+
		`"request":{"type":"LaunchRequest","requestId":"amzn1.echo-api.request.1","timestamp":%q,"locale":"en-AU"}}`,
		skillID, ts.Format(time.RFC3339))
}

func serve(t *testing.T, h http.Handler, body []byte, sig string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/alexa", bytes.NewReader(body))
	req.Header.Set("Signature-256", sig)
	req.Header.Set("SignatureCertChainUrl", testCertURL)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestVerified(t *testing.T) {
	pki := newTestPKI(t)
	v := pki.verifier(t)
	log := slog.New(slog.DiscardHandler)

	var got []byte

	next := func(w http.ResponseWriter, _ *http.Request, body []byte) {
		got = body

		w.WriteHeader(http.StatusOK)
	}

	h := Verified(v, testSkillID, log, next)

	t.Run("authentic request reaches the handler with the original bytes", func(t *testing.T) {
		body := envelope(testSkillID, fixedNow)
		rec := serve(t, h, body, pki.sign(t, body))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, body, got)
	})

	cases := map[string]func() (body []byte, sig string){
		"tampered body": func() ([]byte, string) {
			body := envelope(testSkillID, fixedNow)
			sig := pki.sign(t, body)

			return bytes.Replace(body, []byte("LaunchRequest"), []byte("IntentRequest"), 1), sig
		},
		"stale timestamp": func() ([]byte, string) {
			body := envelope(testSkillID, fixedNow.Add(-151*time.Second))

			return body, pki.sign(t, body)
		},
		"future timestamp": func() ([]byte, string) {
			body := envelope(testSkillID, fixedNow.Add(151*time.Second))

			return body, pki.sign(t, body)
		},
		"another skill": func() ([]byte, string) {
			body := envelope("amzn1.ask.skill.someone-else", fixedNow)

			return body, pki.sign(t, body)
		},
		"missing signature": func() ([]byte, string) {
			return envelope(testSkillID, fixedNow), ""
		},
	}

	for name, build := range cases {
		t.Run(name+" is rejected with a uniform 400", func(t *testing.T) {
			got = nil
			body, sig := build()
			rec := serve(t, h, body, sig)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "bad request", strings.TrimSpace(rec.Body.String()))
			assert.Nil(t, got, "handler must not run")
		})
	}
}

func TestVerifiedRejectsOversizedBody(t *testing.T) {
	called := false
	h := Verified(acceptAll{}, "", slog.New(slog.DiscardHandler), func(http.ResponseWriter, *http.Request, []byte) { called = true })

	rec := serve(t, h, bytes.Repeat([]byte("a"), MaxRequestBytes+1), "sig")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.False(t, called)
}

func TestVerifiedFuncReadsSkillIDPerRequest(t *testing.T) {
	skillID := ""
	ok := func(w http.ResponseWriter, _ *http.Request, _ []byte) { w.WriteHeader(http.StatusOK) }
	h := VerifiedFunc(acceptAll{}, func() string { return skillID }, slog.New(slog.DiscardHandler), ok)

	body := envelope(testSkillID, fixedNow)
	assert.Equal(t, http.StatusOK, serve(t, h, body, "sig").Code, "any skill before one is deployed")

	skillID = "amzn1.ask.skill.deployed"
	assert.Equal(t, http.StatusBadRequest, serve(t, h, body, "sig").Code, "the deployed skill is enforced without a restart")

	skillID = testSkillID
	assert.Equal(t, http.StatusOK, serve(t, h, body, "sig").Code)
}

func TestApplicationIDFallsBackToSession(t *testing.T) {
	id, err := applicationID([]byte(`{"session":{"application":{"applicationId":"amzn1.ask.skill.s"}}}`))
	require.NoError(t, err)
	assert.Equal(t, "amzn1.ask.skill.s", id)
}

type acceptAll struct{}

func (acceptAll) Verify(_ context.Context, _ []byte, _ http.Header) error { return nil }

// recordingVerifier wraps a verifier and keeps what it was asked and why it refused, so a test can tell which check rejected a request.
type recordingVerifier struct {
	next RequestVerifier

	mu   sync.Mutex
	body []byte
	hdr  http.Header
	err  error
}

func (r *recordingVerifier) Verify(ctx context.Context, body []byte, hdr http.Header) error {
	err := r.next.Verify(ctx, body, hdr)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.body, r.hdr, r.err = bytes.Clone(body), hdr.Clone(), err

	return err
}

// verifierServing is a real verifier for pki whose certificate fetches are answered by fetch.
func verifierServing(t *testing.T, pki testPKI, fetch roundTripFunc) *alexaverify.Verifier {
	t.Helper()

	v, err := alexaverify.New(
		alexaverify.WithRoots(pki.roots),
		alexaverify.WithClock(func() time.Time { return fixedNow }),
		alexaverify.WithHTTPClient(&http.Client{Transport: fetch}),
		alexaverify.WithLogger(slog.New(slog.DiscardHandler)),
	)
	require.NoError(t, err)

	return v
}

func post(t *testing.T, h http.Handler, body []byte, hdr http.Header) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/alexa", bytes.NewReader(body))
	maps.Copy(req.Header, hdr)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func signed(sig, certURL string) http.Header {
	hdr := http.Header{}
	if sig != "" {
		hdr.Set("Signature-256", sig)
	}

	if certURL != "" {
		hdr.Set("SignatureCertChainUrl", certURL)
	}

	return hdr
}

// TestVerifiedRejectsForgeries signs requests the ways an attacker could, and checks each is refused by the check meant to catch it.
func TestVerifiedRejectsForgeries(t *testing.T) {
	pki := newTestPKI(t)
	body := envelope(testSkillID, fixedNow)
	good := pki.sign(t, body)

	tests := map[string]struct {
		// chain returns the PKI whose certificate is served and signs, and the PKI whose root is trusted.
		chain func(t *testing.T) (served, trusted testPKI)
		fetch roundTripFunc
		body  []byte
		hdr   http.Header
		want  error
	}{
		"certificate URL over http":         {hdr: signed(good, "http://s3.amazonaws.com/echo.api/echo-api-cert-12.pem"), want: alexaverify.ErrCertURLInvalid},
		"certificate URL on another host":   {hdr: signed(good, "https://attacker.example.com/echo.api/echo-api-cert-12.pem"), want: alexaverify.ErrCertURLInvalid},
		"certificate URL on a lookalike":    {hdr: signed(good, "https://s3.amazonaws.com.attacker.example/echo.api/echo-api-cert-12.pem"), want: alexaverify.ErrCertURLInvalid},
		"certificate URL outside echo.api":  {hdr: signed(good, "https://s3.amazonaws.com/attacker/echo-api-cert-12.pem"), want: alexaverify.ErrCertURLInvalid},
		"certificate URL escaping echo.api": {hdr: signed(good, "https://s3.amazonaws.com/echo.api/../attacker/cert.pem"), want: alexaverify.ErrCertURLTraversal},
		"certificate URL on another port":   {hdr: signed(good, "https://s3.amazonaws.com:8443/echo.api/echo-api-cert-12.pem"), want: alexaverify.ErrCertURLInvalid},
		"certificate URL with userinfo":     {hdr: signed(good, "https://attacker@s3.amazonaws.com/echo.api/echo-api-cert-12.pem"), want: alexaverify.ErrCertURLUserinfo},
		"no certificate URL":                {hdr: signed(good, ""), want: alexaverify.ErrMissingHeader},
		"no signature":                      {hdr: signed("", testCertURL), want: alexaverify.ErrMissingHeader},
		"signature not base64":              {hdr: signed("%%%not-base64%%%", testCertURL), want: alexaverify.ErrBadSignature},
		"signature by another key":          {hdr: signed(newTestPKI(t).sign(t, body), testCertURL), want: alexaverify.ErrBadSignature},
		"signature of another body":         {hdr: signed(pki.sign(t, envelope(testSkillID, fixedNow.Add(time.Second))), testCertURL), want: alexaverify.ErrBadSignature},
		"no body":                           {body: []byte{}, hdr: signed(good, testCertURL), want: alexaverify.ErrStaleTimestamp},
		"chain from an untrusted root":      {chain: func(t *testing.T) (testPKI, testPKI) { return newTestPKI(t), pki }, want: alexaverify.ErrUntrustedChain},
		"certificate for another host": {chain: func(t *testing.T) (testPKI, testPKI) {
			wrong := newTestPKIWith(t, pkiOptions{host: "attacker.example.com"})

			return wrong, wrong
		}, want: alexaverify.ErrCertHostname},
		"expired certificate": {chain: func(t *testing.T) (testPKI, testPKI) {
			expired := newTestPKIWith(t, pkiOptions{notAfter: fixedNow.Add(-time.Minute)})

			return expired, expired
		}, want: alexaverify.ErrUntrustedChain},
		"certificate unavailable": {
			fetch: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("no such key"))}, nil
			},
			hdr: signed(good, testCertURL), want: alexaverify.ErrCertFetch,
		},
		"certificate bundle is not PEM": {fetch: bundleFrom([]byte("<html>not a certificate</html>")), hdr: signed(good, testCertURL), want: alexaverify.ErrUntrustedChain},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var inner RequestVerifier = pki.verifier(t)

			reqBody, hdr := body, tc.hdr
			if tc.body != nil {
				reqBody = tc.body
			}

			switch {
			case tc.chain != nil:
				// The served certificate's own key signs, so only the chain checks can refuse it.
				served, trusted := tc.chain(t)
				inner = verifierServing(t, trusted, bundleFrom(served.bundle))
				hdr = signed(served.sign(t, reqBody), testCertURL)
			case tc.fetch != nil:
				inner = verifierServing(t, pki, tc.fetch)
			}

			rv := &recordingVerifier{next: inner}
			called := false
			h := Verified(rv, testSkillID, slog.New(slog.DiscardHandler), func(http.ResponseWriter, *http.Request, []byte) { called = true })

			rec := post(t, h, reqBody, hdr)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "bad request", strings.TrimSpace(rec.Body.String()))
			assert.False(t, called, "a forged request must never reach the skill")
			require.ErrorIs(t, rv.err, tc.want)
		})
	}
}

func bundleFrom(b []byte) roundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(b))}, nil
	}
}

func TestVerifiedAcceptsTimestampWithinTolerance(t *testing.T) {
	pki := newTestPKI(t)
	h := Verified(pki.verifier(t), testSkillID, slog.New(slog.DiscardHandler), func(w http.ResponseWriter, _ *http.Request, _ []byte) { w.WriteHeader(http.StatusOK) })

	for _, offset := range []time.Duration{-149 * time.Second, 149 * time.Second} {
		body := envelope(testSkillID, fixedNow.Add(offset))
		assert.Equal(t, http.StatusOK, serve(t, h, body, pki.sign(t, body)).Code, offset)
	}
}

func TestVerifiedPassesExactBytesAndHeaders(t *testing.T) {
	pki := newTestPKI(t)
	rv := &recordingVerifier{next: pki.verifier(t)}

	var (
		got     []byte
		elapsed time.Duration
	)

	h := Verified(rv, testSkillID, slog.New(slog.DiscardHandler), func(w http.ResponseWriter, r *http.Request, body []byte) {
		got, elapsed = body, verifyTime(r.Context())

		w.WriteHeader(http.StatusOK)
	})

	// Insignificant whitespace is part of what Amazon signed, so it must reach the verifier untouched.
	body := append([]byte("  \n"), envelope(testSkillID, fixedNow)...)
	sig := pki.sign(t, body)

	require.Equal(t, http.StatusOK, serve(t, h, body, sig).Code)
	require.NoError(t, rv.err)
	assert.Equal(t, body, rv.body)
	assert.Equal(t, body, got)
	assert.Equal(t, sig, rv.hdr.Get("Signature-256"))
	assert.Equal(t, testCertURL, rv.hdr.Get("SignatureCertChainUrl"))
	assert.Positive(t, elapsed, "the handler learns how long verification took")
}

func TestVerifiedSkillIDChecks(t *testing.T) {
	tests := map[string]struct {
		body []byte
		ok   bool
	}{
		"context names the skill":              {body: []byte(`{"context":{"System":{"application":{"applicationId":"` + testSkillID + `"}}}}`), ok: true},
		"only the session names the skill":     {body: []byte(`{"session":{"application":{"applicationId":"` + testSkillID + `"}}}`), ok: true},
		"context wins over the session":        {body: []byte(`{"context":{"System":{"application":{"applicationId":"amzn1.ask.skill.other"}}},"session":{"application":{"applicationId":"` + testSkillID + `"}}}`)},
		"no application ID":                    {body: []byte(`{"request":{"type":"LaunchRequest"}}`)},
		"application ID differs only in case":  {body: []byte(`{"context":{"System":{"application":{"applicationId":"` + strings.ToUpper(testSkillID) + `"}}}}`)},
		"application ID with a trailing space": {body: []byte(`{"context":{"System":{"application":{"applicationId":"` + testSkillID + ` "}}}}`)},
		"not JSON":                             {body: []byte(`not json`)},
		"JSON but not an object":               {body: []byte(`["` + testSkillID + `"]`)},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			called := false
			h := Verified(acceptAll{}, testSkillID, slog.New(slog.DiscardHandler), func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				called = true

				w.WriteHeader(http.StatusOK)
			})

			rec := serve(t, h, tc.body, "sig")
			assert.Equal(t, tc.ok, called)

			if !tc.ok {
				assert.Equal(t, http.StatusBadRequest, rec.Code)
			}
		})
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// TestVerifiedRejectionsLookAlike checks every rejection gives the caller the same answer, while the log says which check failed.
func TestVerifiedRejectionsLookAlike(t *testing.T) {
	var logs bytes.Buffer

	log := slog.New(slog.NewJSONHandler(&logs, nil))
	refuse := verifierFunc(func(context.Context, []byte, http.Header) error { return alexaverify.ErrBadSignature })

	tests := map[string]struct {
		verifier RequestVerifier
		body     io.Reader
		stage    string
	}{
		"unreadable body":     {verifier: acceptAll{}, body: errReader{io.ErrUnexpectedEOF}, stage: "reading body"},
		"oversized body":      {verifier: acceptAll{}, body: bytes.NewReader(bytes.Repeat([]byte(" "), MaxRequestBytes+1)), stage: "reading body"},
		"bad signature":       {verifier: refuse, body: bytes.NewReader(envelope(testSkillID, fixedNow)), stage: "signature verification"},
		"undecodable":         {verifier: acceptAll{}, body: strings.NewReader("{"), stage: "decoding envelope"},
		"for another skill":   {verifier: acceptAll{}, body: bytes.NewReader(envelope("amzn1.ask.skill.other", fixedNow)), stage: "skill ID"},
		"with no application": {verifier: acceptAll{}, body: strings.NewReader("{}"), stage: "skill ID"},
	}

	var first *httptest.ResponseRecorder

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			logs.Reset()

			h := Verified(tc.verifier, testSkillID, log, func(http.ResponseWriter, *http.Request, []byte) { t.Error("handler ran") })

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/alexa", tc.body)
			req.RemoteAddr = "203.0.113.7:4242"

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if first == nil {
				first = rec
			}

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, first.Body.String(), rec.Body.String(), "the body never says which check failed")
			assert.Equal(t, first.Header(), rec.Header())

			var entry map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
			assert.Equal(t, "rejected Alexa request", entry["msg"])
			assert.Equal(t, "WARN", entry["level"])
			assert.Equal(t, tc.stage, entry["stage"])
			assert.Equal(t, "203.0.113.7:4242", entry["remote"])
			assert.NotEmpty(t, entry["error"])
		})
	}
}

type verifierFunc func(ctx context.Context, body []byte, hdr http.Header) error

func (f verifierFunc) Verify(ctx context.Context, body []byte, hdr http.Header) error {
	return f(ctx, body, hdr)
}

func TestVerifiedVerifiesBeforeCheckingSkill(t *testing.T) {
	var order []string

	v := verifierFunc(func(context.Context, []byte, http.Header) error {
		order = append(order, "verify")

		return alexaverify.ErrBadSignature
	})

	h := VerifiedFunc(v, func() string {
		order = append(order, "skill")

		return testSkillID
	}, slog.New(slog.DiscardHandler), func(http.ResponseWriter, *http.Request, []byte) {})

	serve(t, h, envelope(testSkillID, fixedNow), "sig")
	assert.Equal(t, []string{"verify"}, order, "nothing about the body is trusted before its signature is")
}

func TestVerifyTimeWithoutVerification(t *testing.T) {
	assert.Zero(t, verifyTime(t.Context()))
}

func TestNewVerifierRefusesWithoutFetching(t *testing.T) {
	var logs bytes.Buffer

	v, err := NewVerifier(slog.New(slog.NewTextHandler(&logs, nil)))
	require.NoError(t, err)

	body := envelope(testSkillID, time.Now())

	require.ErrorIs(t, v.Verify(t.Context(), body, http.Header{}), alexaverify.ErrMissingHeader)
	require.ErrorIs(t, v.Verify(t.Context(), body, signed("c2ln", "https://attacker.example.com/echo.api/cert.pem")), alexaverify.ErrCertURLInvalid)
	require.ErrorIs(t, v.Verify(t.Context(), body, signed("c2ln", "file:///etc/passwd")), alexaverify.ErrCertURLInvalid)
}

// TestVerifiedEnforcesSkillOnRealEnvelopes checks the skill ID on full-size envelopes of every request type, not just the minimal test envelope.
func TestVerifiedEnforcesSkillOnRealEnvelopes(t *testing.T) {
	const (
		own     = "amzn1.ask.skill.test"
		foreign = "amzn1.ask.skill.attacker"
	)

	requests, err := filepath.Glob("testdata/golden/*/request.json")
	require.NoError(t, err)
	require.NotEmpty(t, requests)

	for _, path := range requests {
		body := mustRead(t, path)

		// with re-encodes the envelope naming the given skills in its context and session.
		with := func(contextID, sessionID string) []byte {
			var env map[string]any
			require.NoError(t, json.Unmarshal(body, &env), path)

			app := func(parent map[string]any) map[string]any {
				a, ok := parent["application"].(map[string]any)
				require.True(t, ok, "%s has an application", path)

				return a
			}

			app(env["context"].(map[string]any)["System"].(map[string]any))["applicationId"] = contextID
			app(env["session"].(map[string]any))["applicationId"] = sessionID

			b, err := json.Marshal(env)
			require.NoError(t, err)

			return b
		}

		for name, tc := range map[string]struct {
			body []byte
			ok   bool
		}{
			"own skill":                     {body: body, ok: true},
			"another skill":                 {body: with(foreign, foreign)},
			"another skill in the context":  {body: with(foreign, own)},
			"another skill only in session": {body: with(own, foreign), ok: true},
		} {
			called := false
			h := Verified(acceptAll{}, own, slog.New(slog.DiscardHandler), func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				called = true

				w.WriteHeader(http.StatusOK)
			})

			rec := serve(t, h, tc.body, "sig")
			assert.Equal(t, tc.ok, called, "%s: %s", path, name)

			if !tc.ok {
				assert.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", path, name)
			}
		}
	}
}

func TestVerifiedBodySizeLimitIsInclusive(t *testing.T) {
	var got int

	h := Verified(acceptAll{}, testSkillID, slog.New(slog.DiscardHandler), func(w http.ResponseWriter, _ *http.Request, body []byte) {
		got = len(body)

		w.WriteHeader(http.StatusOK)
	})

	env := envelope(testSkillID, fixedNow)
	padded := slices.Concat(env, bytes.Repeat([]byte(" "), MaxRequestBytes-len(env)))

	require.Equal(t, http.StatusOK, serve(t, h, padded, "sig").Code, "a body of exactly MaxRequestBytes is read whole")
	assert.Equal(t, MaxRequestBytes, got)

	got = 0
	assert.Equal(t, http.StatusBadRequest, serve(t, h, append(padded, ' '), "sig").Code)
	assert.Zero(t, got)
}

func TestWarmVerifierFillsTheCertificateCache(t *testing.T) {
	pki := newTestPKI(t)

	var fetches atomic.Int32

	v := verifierServing(t, pki, func(r *http.Request) (*http.Response, error) {
		fetches.Add(1)

		return bundleFrom(pki.bundle)(r)
	})

	WarmVerifier(t.Context(), v, testCertURL, slog.New(slog.DiscardHandler))
	require.Equal(t, int32(1), fetches.Load(), "warming downloads the chain")

	h := Verified(v, testSkillID, slog.New(slog.DiscardHandler), func(w http.ResponseWriter, _ *http.Request, _ []byte) { w.WriteHeader(http.StatusOK) })
	body := envelope(testSkillID, fixedNow)
	require.Equal(t, http.StatusOK, serve(t, h, body, pki.sign(t, body)).Code)
	assert.Equal(t, int32(1), fetches.Load(), "the first request finds the chain already cached")
}

func TestWarmVerifierIsBestEffort(t *testing.T) {
	pki := newTestPKI(t)

	var fetches atomic.Int32

	v := verifierServing(t, pki, func(*http.Request) (*http.Response, error) {
		fetches.Add(1)

		return nil, errors.New("no route to host")
	})

	var logs bytes.Buffer

	log := slog.New(slog.NewTextHandler(&logs, nil))

	WarmVerifier(t.Context(), v, "", log)
	assert.Zero(t, fetches.Load(), "with no stored URL there is nothing to warm")
	assert.Empty(t, logs.String())

	WarmVerifier(t.Context(), v, testCertURL, log)
	assert.Contains(t, logs.String(), "couldn't warm the Alexa certificate cache", "a failed warm is logged, not fatal")
}

func TestRememberCertURL(t *testing.T) {
	const rotated = "https://s3.amazonaws.com/echo.api/echo-api-cert-13.pem"

	var (
		reject bool
		seen   []string
	)

	v := RememberCertURL(verifierFunc(func(context.Context, []byte, http.Header) error {
		if reject {
			return errors.New("bad signature")
		}

		return nil
	}), testCertURL, func(_ context.Context, url string) { seen = append(seen, url) })

	verify := func(url string) error { return v.Verify(t.Context(), nil, signed("sig", url)) }

	require.NoError(t, verify(testCertURL))
	assert.Empty(t, seen, "the stored URL isn't stored again")

	reject = true
	require.Error(t, verify(rotated))
	assert.Empty(t, seen, "a request that fails verification can't choose the URL")

	reject = false
	require.NoError(t, verify(""))
	assert.Empty(t, seen, "a request without the header leaves the stored URL alone")

	require.NoError(t, verify(rotated))
	require.NoError(t, verify(rotated))
	assert.Equal(t, []string{rotated}, seen, "a rotated certificate is stored once")
}
