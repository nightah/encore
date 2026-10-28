package blobstore

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTLSConfig(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	bundle := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))

	get := func(t *testing.T, cfg *tls.Config) error {
		t.Helper()

		client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
		require.NoError(t, err)

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}

		return err
	}

	t.Run("system pool only", func(t *testing.T) {
		cfg, err := tlsConfig("", false)
		require.NoError(t, err)
		assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
		assert.Nil(t, cfg.RootCAs)
		require.Error(t, get(t, cfg), "a private CA is not trusted by default")
	})

	t.Run("bundle", func(t *testing.T) {
		cfg, err := tlsConfig(bundle, false)
		require.NoError(t, err)
		require.NoError(t, get(t, cfg))
	})

	t.Run("insecure", func(t *testing.T) {
		cfg, err := tlsConfig("", true)
		require.NoError(t, err)
		require.NoError(t, get(t, cfg))
	})

	t.Run("bundle without certificates", func(t *testing.T) {
		empty := filepath.Join(dir, "empty.pem")
		require.NoError(t, os.WriteFile(empty, []byte("not a certificate"), 0o600))

		_, err := tlsConfig(empty, false)
		require.ErrorContains(t, err, "no PEM certificates")
	})

	t.Run("missing bundle", func(t *testing.T) {
		_, err := tlsConfig(filepath.Join(dir, "missing.pem"), false)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}
