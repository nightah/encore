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
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
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

func newTestPKI(t *testing.T) testPKI {
	t.Helper()

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
		Subject:      pkix.Name{CommonName: "echo-api.amazon.com"},
		DNSNames:     []string{"echo-api.amazon.com"},
		NotBefore:    fixedNow.Add(-time.Hour),
		NotAfter:     fixedNow.Add(time.Hour),
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
