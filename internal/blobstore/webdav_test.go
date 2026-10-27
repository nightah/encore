package blobstore_test

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	xwebdav "golang.org/x/net/webdav"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/blobstore/blobstoretest"
	"github.com/nightah/encore/internal/config"
)

// davServer requires basic auth and serves a temporary directory.
func davServer(t *testing.T, tls bool) (srv *httptest.Server, dir string) {
	t.Helper()

	dir = t.TempDir()
	h := &xwebdav.Handler{FileSystem: xwebdav.Dir(dir), LockSystem: xwebdav.NewMemLS()}

	auth := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "alice" || p != "app-password" {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			http.Error(w, "unauthorised", http.StatusUnauthorized)

			return
		}

		h.ServeHTTP(w, r)
	})

	if tls {
		srv = httptest.NewTLSServer(auth)
	} else {
		srv = httptest.NewServer(auth)
	}

	t.Cleanup(srv.Close)

	return srv, dir
}

func newDAV(t *testing.T, cfg config.StorageWebDAV) *blobstore.WebDAV {
	t.Helper()

	s, err := blobstore.NewWebDAV(cfg)
	require.NoError(t, err)

	return s
}

func TestWebDAVContract(t *testing.T) {
	blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
		srv, _ := davServer(t, false)

		return newDAV(t, config.StorageWebDAV{URL: srv.URL, Username: "alice", Password: "app-password"})
	})
}

func TestWebDAVContractWithPrefix(t *testing.T) {
	blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
		srv, _ := davServer(t, false)

		return newDAV(t, config.StorageWebDAV{URL: srv.URL + "/", Username: "alice", Password: "app-password", Prefix: "/encore/prod/"})
	})
}

func TestWebDAVPrefixAndCollections(t *testing.T) {
	srv, dir := davServer(t, false)
	s := newDAV(t, config.StorageWebDAV{URL: srv.URL, Username: "alice", Password: "app-password", Prefix: "encore"})

	require.NoError(t, s.Put(t.Context(), "deep/nested/file.txt", strings.NewReader("hi"), 2))

	b, err := os.ReadFile(filepath.Join(dir, "encore", "deep", "nested", "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hi", string(b))
}

func TestWebDAVBadCredentials(t *testing.T) {
	srv, _ := davServer(t, false)
	s := newDAV(t, config.StorageWebDAV{URL: srv.URL, Username: "alice", Password: "wrong"})

	err := s.Probe(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "probe: cannot write")
	assert.Contains(t, err.Error(), "check the username and app password")
	assert.NotContains(t, err.Error(), "wrong")
}

func TestWebDAVCustomCA(t *testing.T) {
	srv, _ := davServer(t, true)

	cfg := config.StorageWebDAV{URL: srv.URL, Username: "alice", Password: "app-password"}

	err := newDAV(t, cfg).Probe(t.Context())
	require.Error(t, err, "an untrusted certificate is refused")
	assert.Contains(t, err.Error(), "certificate")

	cfg.CABundle = writeCA(t, srv)
	require.NoError(t, newDAV(t, cfg).Probe(t.Context()))

	cfg.CABundle = ""
	cfg.InsecureSkipVerify = true
	require.NoError(t, newDAV(t, cfg).Probe(t.Context()))
}

func TestWebDAVConfigErrors(t *testing.T) {
	for _, cfg := range []config.StorageWebDAV{
		{URL: "ftp://host/dav"},
		{URL: "https://user:pass@host/dav"}, // trufflehog:ignore
		{URL: "https://host/dav", Prefix: "../up"},
		{URL: "https://host/dav", CABundle: "/nonexistent/ca.pem"},
	} {
		_, err := blobstore.NewWebDAV(cfg)
		require.Error(t, err, cfg.URL)
		assert.NotContains(t, err.Error(), "pass@")
	}
}

func writeCA(t *testing.T, srv *httptest.Server) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))

	return path
}
