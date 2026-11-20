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

	v, err := alexaverify.New(p.options(client)...)
	require.NoError(t, err)

	return v
}

// options configure a verifier for the test root, whose certificate fetches go through client.
func (p testPKI) options(client *http.Client) []alexaverify.Option {
	return []alexaverify.Option{
		alexaverify.WithRoots(p.roots),
		alexaverify.WithClock(func() time.Time { return fixedNow }),
		alexaverify.WithHTTPClient(client),
		alexaverify.WithLogger(slog.New(slog.DiscardHandler)),
	}
}

// sentVerifier is verifier with the check as sent NewVerifier adds.
func (p testPKI) sentVerifier(t *testing.T) *Verifier {
	t.Helper()

	v, err := newVerifier(p.options(&http.Client{Transport: bundleFrom(p.bundle)})...)
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

	t.Run("ShouldPassAnAuthenticRequestToTheHandlerWithTheOriginalBytes", func(t *testing.T) {
		body := envelope(testSkillID, fixedNow)
		rec := serve(t, h, body, pki.sign(t, body))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, body, got)
	})

	testCases := []struct {
		name  string
		build func() (body []byte, sig string)
	}{
		{
			name: "ShouldRejectATamperedBodyWithAUniform400",
			build: func() ([]byte, string) {
				body := envelope(testSkillID, fixedNow)
				sig := pki.sign(t, body)

				return bytes.Replace(body, []byte("LaunchRequest"), []byte("IntentRequest"), 1), sig
			},
		},
		{
			name: "ShouldRejectAStaleTimestampWithAUniform400",
			build: func() ([]byte, string) {
				body := envelope(testSkillID, fixedNow.Add(-151*time.Second))

				return body, pki.sign(t, body)
			},
		},
		{
			name: "ShouldRejectAFutureTimestampWithAUniform400",
			build: func() ([]byte, string) {
				body := envelope(testSkillID, fixedNow.Add(151*time.Second))

				return body, pki.sign(t, body)
			},
		},
		{
			name: "ShouldRejectAnotherSkillWithAUniform400",
			build: func() ([]byte, string) {
				body := envelope("amzn1.ask.skill.someone-else", fixedNow)

				return body, pki.sign(t, body)
			},
		},
		{
			name: "ShouldRejectAMissingSignatureWithAUniform400",
			build: func() ([]byte, string) {
				return envelope(testSkillID, fixedNow), ""
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got = nil
			body, sig := tc.build()
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
	h := VerifiedFunc(acceptAll{}, func() string { return skillID }, slog.New(slog.DiscardHandler), nil, ok)

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

	testCases := []struct {
		name string
		// chain returns the PKI whose certificate is served and signs, and the PKI whose root is trusted.
		chain func(t *testing.T) (served, trusted testPKI)
		fetch roundTripFunc
		body  []byte
		hdr   http.Header
		err   error
	}{
		{name: "ShouldRejectACertificateURLOverHTTP", hdr: signed(good, "http://s3.amazonaws.com/echo.api/echo-api-cert-12.pem"), err: alexaverify.ErrCertURLInvalid},
		{name: "ShouldRejectACertificateURLOnAnotherHost", hdr: signed(good, "https://attacker.example.com/echo.api/echo-api-cert-12.pem"), err: alexaverify.ErrCertURLInvalid},
		{name: "ShouldRejectACertificateURLOnALookalikeHost", hdr: signed(good, "https://s3.amazonaws.com.attacker.example/echo.api/echo-api-cert-12.pem"), err: alexaverify.ErrCertURLInvalid},
		{name: "ShouldRejectACertificateURLOutsideEchoAPI", hdr: signed(good, "https://s3.amazonaws.com/attacker/echo-api-cert-12.pem"), err: alexaverify.ErrCertURLInvalid},
		{name: "ShouldRejectACertificateURLEscapingEchoAPI", hdr: signed(good, "https://s3.amazonaws.com/echo.api/../attacker/cert.pem"), err: alexaverify.ErrCertURLTraversal},
		{name: "ShouldRejectACertificateURLOnAnotherPort", hdr: signed(good, "https://s3.amazonaws.com:8443/echo.api/echo-api-cert-12.pem"), err: alexaverify.ErrCertURLInvalid},
		{name: "ShouldRejectACertificateURLWithUserinfo", hdr: signed(good, "https://attacker@s3.amazonaws.com/echo.api/echo-api-cert-12.pem"), err: alexaverify.ErrCertURLUserinfo},
		{name: "ShouldRejectAMissingCertificateURL", hdr: signed(good, ""), err: alexaverify.ErrMissingHeader},
		{name: "ShouldRejectAMissingSignature", hdr: signed("", testCertURL), err: alexaverify.ErrMissingHeader},
		{name: "ShouldRejectASignatureThatIsNotBase64", hdr: signed("%%%not-base64%%%", testCertURL), err: alexaverify.ErrBadSignature},
		{name: "ShouldRejectASignatureByAnotherKey", hdr: signed(newTestPKI(t).sign(t, body), testCertURL), err: alexaverify.ErrBadSignature},
		{name: "ShouldRejectASignatureOfAnotherBody", hdr: signed(pki.sign(t, envelope(testSkillID, fixedNow.Add(time.Second))), testCertURL), err: alexaverify.ErrBadSignature},
		{name: "ShouldRejectAMissingBody", body: []byte{}, hdr: signed(good, testCertURL), err: alexaverify.ErrStaleTimestamp},
		{name: "ShouldRejectAChainFromAnUntrustedRoot", chain: func(t *testing.T) (testPKI, testPKI) { return newTestPKI(t), pki }, err: alexaverify.ErrUntrustedChain},
		{name: "ShouldRejectACertificateForAnotherHost", chain: func(t *testing.T) (testPKI, testPKI) {
			wrong := newTestPKIWith(t, pkiOptions{host: "attacker.example.com"})

			return wrong, wrong
		}, err: alexaverify.ErrCertHostname},
		{name: "ShouldRejectAnExpiredCertificate", chain: func(t *testing.T) (testPKI, testPKI) {
			expired := newTestPKIWith(t, pkiOptions{notAfter: fixedNow.Add(-time.Minute)})

			return expired, expired
		}, err: alexaverify.ErrUntrustedChain},
		{
			name: "ShouldRejectWhenTheCertificateIsUnavailable",
			fetch: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("no such key"))}, nil
			},
			hdr: signed(good, testCertURL), err: alexaverify.ErrCertFetch,
		},
		{name: "ShouldRejectACertificateBundleThatIsNotPEM", fetch: bundleFrom([]byte("<html>not a certificate</html>")), hdr: signed(good, testCertURL), err: alexaverify.ErrUntrustedChain},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
			require.ErrorIs(t, rv.err, tc.err)
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

	testCases := []struct {
		name string
		have time.Duration
	}{
		{name: "ShouldAcceptATimestampJustInThePast", have: -149 * time.Second},
		{name: "ShouldAcceptATimestampJustInTheFuture", have: 149 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body := envelope(testSkillID, fixedNow.Add(tc.have))
			assert.Equal(t, http.StatusOK, serve(t, h, body, pki.sign(t, body)).Code)
		})
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
	testCases := []struct {
		name string
		body []byte
		ok   bool
	}{
		{name: "ShouldAcceptWhenTheContextNamesTheSkill", body: []byte(`{"context":{"System":{"application":{"applicationId":"` + testSkillID + `"}}}}`), ok: true},
		{name: "ShouldAcceptWhenOnlyTheSessionNamesTheSkill", body: []byte(`{"session":{"application":{"applicationId":"` + testSkillID + `"}}}`), ok: true},
		{name: "ShouldRejectWhenTheContextNamesAnotherSkillOverTheSession", body: []byte(`{"context":{"System":{"application":{"applicationId":"amzn1.ask.skill.other"}}},"session":{"application":{"applicationId":"` + testSkillID + `"}}}`)},
		{name: "ShouldRejectWithoutAnApplicationID", body: []byte(`{"request":{"type":"LaunchRequest"}}`)},
		{name: "ShouldRejectAnApplicationIDThatDiffersOnlyInCase", body: []byte(`{"context":{"System":{"application":{"applicationId":"` + strings.ToUpper(testSkillID) + `"}}}}`)},
		{name: "ShouldRejectAnApplicationIDWithATrailingSpace", body: []byte(`{"context":{"System":{"application":{"applicationId":"` + testSkillID + ` "}}}}`)},
		{name: "ShouldRejectABodyThatIsNotJSON", body: []byte(`not json`)},
		{name: "ShouldRejectJSONThatIsNotAnObject", body: []byte(`["` + testSkillID + `"]`)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

	testCases := []struct {
		name     string
		verifier RequestVerifier
		body     io.Reader
		stage    string
		counted  RejectStage
	}{
		{name: "ShouldRejectAnUnreadableBody", verifier: acceptAll{}, body: errReader{io.ErrUnexpectedEOF}, stage: "reading body", counted: RejectBody},
		{name: "ShouldRejectAnOversizedBody", verifier: acceptAll{}, body: bytes.NewReader(bytes.Repeat([]byte(" "), MaxRequestBytes+1)), stage: "reading body", counted: RejectBody},
		{name: "ShouldRejectABadSignature", verifier: refuse, body: bytes.NewReader(envelope(testSkillID, fixedNow)), stage: "signature verification", counted: RejectSignature},
		{name: "ShouldRejectAnUndecodableEnvelope", verifier: acceptAll{}, body: strings.NewReader("{"), stage: "decoding envelope", counted: RejectBody},
		{name: "ShouldRejectARequestForAnotherSkill", verifier: acceptAll{}, body: bytes.NewReader(envelope("amzn1.ask.skill.other", fixedNow)), stage: "skill ID", counted: RejectSkillID},
		{name: "ShouldRejectARequestWithNoApplication", verifier: acceptAll{}, body: strings.NewReader("{}"), stage: "skill ID", counted: RejectSkillID},
	}

	var first *httptest.ResponseRecorder

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()

			rejected := NewRejections(func() time.Time { return fixedNow })
			h := VerifiedFunc(tc.verifier, func() string { return testSkillID }, log, rejected, func(http.ResponseWriter, *http.Request, []byte) { t.Error("handler ran") })

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
			assert.Equal(t, []Rejection{{Stage: tc.counted, Count: 1, Recent: 1, Last: fixedNow}}, rejected.Counts(fixedNow), "the rejection is counted by the check that refused it")
		})
	}
}

// TestVerifyStage sorts every failure the verifier reports into the check an operator would look at.
func TestVerifyStage(t *testing.T) {
	testCases := []struct {
		name     string
		have     error
		expected RejectStage
	}{
		{name: "ShouldSortAMissingHeaderAsUnsigned", have: alexaverify.ErrMissingHeader, expected: RejectUnsigned},
		{name: "ShouldSortAStaleTimestampAsTimestamp", have: alexaverify.ErrStaleTimestamp, expected: RejectTimestamp},
		{name: "ShouldSortAnInvalidCertURLAsUnsigned", have: alexaverify.ErrCertURLInvalid, expected: RejectUnsigned},
		{name: "ShouldSortACertURLWithUserinfoAsUnsigned", have: alexaverify.ErrCertURLUserinfo, expected: RejectUnsigned},
		{name: "ShouldSortACertURLTraversalAsUnsigned", have: alexaverify.ErrCertURLTraversal, expected: RejectUnsigned},
		{name: "ShouldSortACertFetchAsSignature", have: alexaverify.ErrCertFetch, expected: RejectSignature},
		{name: "ShouldSortAnUntrustedChainAsSignature", have: alexaverify.ErrUntrustedChain, expected: RejectSignature},
		{name: "ShouldSortACertHostnameAsSignature", have: alexaverify.ErrCertHostname, expected: RejectSignature},
		{name: "ShouldSortAnUnsupportedKeyAsSignature", have: alexaverify.ErrUnsupportedKey, expected: RejectSignature},
		{name: "ShouldSortABadSignatureAsSignature", have: alexaverify.ErrBadSignature, expected: RejectSignature},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, verifyStage(fmt.Errorf("wrapped: %w", tc.have)))
		})
	}
}

func TestRejectionsCount(t *testing.T) {
	now := fixedNow
	r := NewRejections(func() time.Time { return now })
	assert.Empty(t, r.Counts(now))

	r.Record(RejectSkillID)
	r.Record(RejectTimestamp)
	now = now.Add(time.Minute)
	r.Record(RejectTimestamp)

	assert.Equal(t, []Rejection{
		{Stage: RejectTimestamp, Count: 2, Recent: 2, Last: fixedNow.Add(time.Minute)},
		{Stage: RejectSkillID, Count: 1, Recent: 1, Last: fixedNow},
	}, r.Counts(now), "stages come in the order the checks run, each with its latest rejection")

	var none *Rejections
	none.Record(RejectBody)

	assert.NotNil(t, NewRejections(nil).now, "the clock defaults to time.Now")
}

// Recent counts the rejections of the last day, while Count keeps every one since Encore started.
func TestRejectionsRecent(t *testing.T) {
	now := fixedNow
	r := NewRejections(func() time.Time { return now })

	r.Record(RejectSignature)

	now = now.Add(20 * 24 * time.Hour)
	r.Record(RejectSignature)

	assert.Equal(t, []Rejection{{Stage: RejectSignature, Count: 2, Recent: 1, Last: now}}, r.Counts(now), "a rejection weeks ago isn't a recent one")

	now = now.Add(RecentRejections - time.Minute)
	assert.Equal(t, 1, r.Counts(now)[0].Recent, "a day isn't over yet")

	later := now.Add(time.Hour + time.Minute)
	assert.Zero(t, r.Counts(later)[0].Recent, "a day after the last rejection, none is recent")
	assert.Equal(t, 2, r.Counts(later)[0].Count)

	for range 3 * 24 {
		now = now.Add(time.Hour)
		r.Record(RejectSignature)
	}

	assert.Equal(t, 74, r.Counts(now)[0].Count)
	assert.Equal(t, 25, r.Counts(now)[0].Recent, "the last day's hours, counted to the hour")
	assert.LessOrEqual(t, len(r.hours[RejectSignature]), 25, "hours older than a day are let go")
}

// A request refused for its timestamp is only put down to the clock when Alexa signed it; anything else that fails that check is somebody else's request.
func TestVerifiedBlamesTheClockOnlyForAlexaRequests(t *testing.T) {
	pki := newTestPKI(t)
	stale := envelope(testSkillID, fixedNow.Add(-30*time.Minute))
	fresh := envelope(testSkillID, fixedNow)
	other := envelope(testSkillID, fixedNow.Add(-31*time.Minute))
	undated := []byte(`{"request":{"type":"LaunchRequest","timestamp":"yesterday"}}`)

	sent := pki.sentVerifier(t)

	remembered := RememberCertURL(sent, testCertURL, func(context.Context, string) { t.Error("a refused request taught a certificate URL") })

	testCases := []struct {
		name     string
		verifier RequestVerifier
		body     []byte
		hdr      http.Header
		expected RejectStage
	}{
		{name: "ShouldBlameTheClockForAStaleRequestAlexaSigned", verifier: sent, body: stale, hdr: signed(pki.sign(t, stale), testCertURL), expected: RejectTimestamp},
		{name: "ShouldBlameTheClockThroughRememberCertURL", verifier: remembered, body: stale, hdr: signed(pki.sign(t, stale), testCertURL), expected: RejectTimestamp},
		{name: "ShouldBlameTheSignatureForAStaleRequestSignedByAnybodyElse", verifier: sent, body: stale, hdr: signed(pki.sign(t, other), testCertURL), expected: RejectSignature},
		{name: "ShouldBlameTheSignatureForAStaleRequestFromAnotherChain", verifier: sent, body: stale, hdr: signed(newTestPKI(t).sign(t, stale), testCertURL), expected: RejectSignature},
		{name: "ShouldBlameTheBodyForAnEmptyEnvelope", verifier: sent, body: []byte(`{}`), hdr: signed("x", "x"), expected: RejectBody},
		{name: "ShouldBlameTheBodyForABodyThatIsNotJSON", verifier: sent, body: []byte(`garbage`), hdr: signed("x", "x"), expected: RejectBody},
		{name: "ShouldBlameTheBodyForATimestampThatDoesNotParse", verifier: sent, body: undated, hdr: signed("x", testCertURL), expected: RejectBody},
		{name: "ShouldCountAStaleRequestWithAnotherURLAsUnsigned", verifier: sent, body: stale, hdr: signed(pki.sign(t, stale), "https://attacker.example.com/echo.api/cert.pem"), expected: RejectUnsigned},
		{name: "ShouldCountAFreshRequestWithAnotherURLAsUnsigned", verifier: sent, body: fresh, hdr: signed(pki.sign(t, fresh), "x"), expected: RejectUnsigned},
		{name: "ShouldBlameTheSignatureForAFreshRequestWithABadSignature", verifier: sent, body: fresh, hdr: signed(pki.sign(t, other), testCertURL), expected: RejectSignature},
		{name: "ShouldBlameTheSignatureWhenTheVerifierCannotCheckAsSent", verifier: pki.verifier(t), body: stale, hdr: signed(pki.sign(t, stale), testCertURL), expected: RejectSignature},
		{name: "ShouldBlameTheSignatureWhenRememberCertURLWrapsAVerifierThatCannotCheckAsSent", verifier: RememberCertURL(pki.verifier(t), testCertURL, nil), body: stale, hdr: signed(pki.sign(t, stale), testCertURL), expected: RejectSignature},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rejected := NewRejections(func() time.Time { return fixedNow })
			h := VerifiedFunc(tc.verifier, func() string { return testSkillID }, slog.New(slog.DiscardHandler), rejected, func(http.ResponseWriter, *http.Request, []byte) { t.Error("handler ran") })

			assert.Equal(t, http.StatusBadRequest, post(t, h, tc.body, tc.hdr).Code)
			assert.Equal(t, []Rejection{{Stage: tc.expected, Count: 1, Recent: 1, Last: fixedNow}}, rejected.Counts(fixedNow))
		})
	}
}

// A stale timestamp and a missing signature are counted apart from a bad signature, so the overview can point at the host clock.
func TestVerifiedCountsVerifierStages(t *testing.T) {
	pki := newTestPKI(t)
	v := pki.sentVerifier(t)

	rejected := NewRejections(func() time.Time { return fixedNow })
	h := VerifiedFunc(v, func() string { return testSkillID }, slog.New(slog.DiscardHandler), rejected, func(http.ResponseWriter, *http.Request, []byte) { t.Error("handler ran") })

	stale := envelope(testSkillID, fixedNow.Add(-30*time.Minute))
	post(t, h, stale, signed(pki.sign(t, stale), testCertURL))
	post(t, h, stale, signed("", testCertURL))

	assert.Equal(t, []Rejection{{Stage: RejectUnsigned, Count: 1, Recent: 1, Last: fixedNow}, {Stage: RejectTimestamp, Count: 1, Recent: 1, Last: fixedNow}}, rejected.Counts(fixedNow))
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
	}, slog.New(slog.DiscardHandler), nil, func(http.ResponseWriter, *http.Request, []byte) {})

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

	testCases := []struct {
		name string
		hdr  http.Header
		err  error
	}{
		{name: "ShouldRefuseAMissingHeader", hdr: http.Header{}, err: alexaverify.ErrMissingHeader},
		{name: "ShouldRefuseACertificateURLOnAnotherHost", hdr: signed("c2ln", "https://attacker.example.com/echo.api/cert.pem"), err: alexaverify.ErrCertURLInvalid},
		{name: "ShouldRefuseAFileCertificateURL", hdr: signed("c2ln", "file:///etc/passwd"), err: alexaverify.ErrCertURLInvalid},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, v.Verify(t.Context(), body, tc.hdr), tc.err)
		})
	}
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
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			body := mustRead(t, path)

			// with re-encodes the envelope naming the given skills in its context and session.
			with := func(contextID, sessionID string) []byte {
				var env map[string]any
				require.NoError(t, json.Unmarshal(body, &env))

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

			testCases := []struct {
				name string
				body []byte
				ok   bool
			}{
				{name: "ShouldAcceptTheOwnSkill", body: body, ok: true},
				{name: "ShouldRejectAnotherSkill", body: with(foreign, foreign)},
				{name: "ShouldRejectAnotherSkillInTheContext", body: with(foreign, own)},
				{name: "ShouldAcceptAnotherSkillOnlyInTheSession", body: with(own, foreign), ok: true},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					called := false
					h := Verified(acceptAll{}, own, slog.New(slog.DiscardHandler), func(w http.ResponseWriter, _ *http.Request, _ []byte) {
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
		})
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
