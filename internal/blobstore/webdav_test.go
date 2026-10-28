package blobstore_test

import (
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// folderHandler answers a request on a collection the way a real server does rather than as x/net/webdav would, reporting whether it did; x/net/webdav answers a GET on a collection with a 405.
type folderHandler func(w http.ResponseWriter, r *http.Request) bool

// nextcloudFolder is what Nextcloud sends for a GET on a folder: a 200 with a short HTML notice.
func nextcloudFolder(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}

	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	_, _ = io.WriteString(w, "This is the WebDAV interface. It can only be accessed by WebDAV clients such as the Nextcloud desktop sync client.")

	return true
}

// modDAVFolder is what Apache mod_dav with Options -Indexes does: mod_dir redirects any request for a folder without the trailing slash to the URL with it, and a GET on that is forbidden.
func modDAVFolder(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case !strings.HasSuffix(r.URL.Path, "/"):
		http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
	case r.Method == http.MethodGet:
		http.Error(w, "Forbidden", http.StatusForbidden)
	default:
		return false
	}

	return true
}

// folderServer serves a temporary directory over WebDAV, letting folder answer requests on collections.
func folderServer(t *testing.T, folder folderHandler) *httptest.Server {
	t.Helper()

	dir := t.TempDir()
	h := &xwebdav.Handler{FileSystem: xwebdav.Dir(dir), LockSystem: xwebdav.NewMemLS()}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(r.URL.Path))); err == nil && fi.IsDir() && r.URL.Path != "/" && folder(w, r) {
			return
		}

		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv
}

var folderServers = map[string]folderHandler{"Nextcloud": nextcloudFolder, "mod_dav": modDAVFolder}

func TestWebDAVContractFolderGET(t *testing.T) {
	for name, folder := range folderServers {
		t.Run(name, func(t *testing.T) {
			blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
				return newDAV(t, config.StorageWebDAV{URL: folderServer(t, folder).URL, Prefix: "encore"})
			})
		})
	}
}

// TestWebDAVGetFolderIsNotFound pins that a key naming a folder is not an object, whatever the server answers to a GET on it: Nextcloud sends a 200 with an HTML notice, which would otherwise be returned as the object's body, and mod_dav a 403, which would otherwise be reported as a permissions problem.
func TestWebDAVGetFolderIsNotFound(t *testing.T) {
	for name, folder := range folderServers {
		t.Run(name, func(t *testing.T) {
			s := newDAV(t, config.StorageWebDAV{URL: folderServer(t, folder).URL})

			require.NoError(t, s.Put(t.Context(), "backups/encore-1.db.gz", strings.NewReader("db"), 2))

			rc, err := s.Get(t.Context(), "backups")
			if err == nil {
				_ = rc.Close()
			}

			require.ErrorIs(t, err, blobstore.ErrNotFound)

			rc, err = s.Get(t.Context(), "backups/encore-1.db.gz")
			require.NoError(t, err)

			b, err := io.ReadAll(rc)
			_ = rc.Close()

			require.NoError(t, err)
			assert.Equal(t, "db", string(b))
		})
	}
}

// TestWebDAVGetWithoutResourceType checks that a PROPFIND answer with no successful propstat leaves the decision to the GET rather than failing the read.
func TestWebDAVGetWithoutResourceType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND":
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>`+r.URL.Path+`</d:href><d:propstat><d:prop><d:resourcetype/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response></d:multistatus>`)
		case r.Method == http.MethodGet && r.URL.Path == "/present":
			_, _ = io.WriteString(w, "body")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	s := newDAV(t, config.StorageWebDAV{URL: srv.URL})

	rc, err := s.Get(t.Context(), "present")
	require.NoError(t, err)

	b, err := io.ReadAll(rc)
	_ = rc.Close()

	require.NoError(t, err)
	assert.Equal(t, "body", string(b))

	_, err = s.Get(t.Context(), "absent")
	require.ErrorIs(t, err, blobstore.ErrNotFound)
}

// TestWebDAVPutSendsContentLength pins that uploads carry a Content-Length rather than chunked transfer encoding: PHP-FPM behind Apache mod_proxy_fcgi, or nginx with fastcgi_request_buffering off, hands Nextcloud an empty body for a chunked PUT and it stores a zero-byte file without an error.
func TestWebDAVPutSendsContentLength(t *testing.T) {
	type upload struct {
		length   int64
		encoding []string
	}

	var (
		mu      sync.Mutex
		uploads []upload
	)

	h := &xwebdav.Handler{FileSystem: xwebdav.Dir(t.TempDir()), LockSystem: xwebdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mu.Lock()
			uploads = append(uploads, upload{r.ContentLength, r.TransferEncoding})
			mu.Unlock()
		}

		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	s := newDAV(t, config.StorageWebDAV{URL: srv.URL})
	body := strings.Repeat("encore ", 1<<15)

	require.NoError(t, s.Put(t.Context(), "known/size.bin", strings.NewReader(body), int64(len(body))))
	// MultiReader hides the length, as a pipe or gzip stream would.
	require.NoError(t, s.Put(t.Context(), "unknown/size.bin", io.MultiReader(strings.NewReader(body)), -1))

	mu.Lock()
	defer mu.Unlock()

	want := upload{length: int64(len(body))}
	assert.Equal(t, []upload{want, want}, uploads)
}

func TestWebDAVSendsEachRequestOnce(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)

	h := &xwebdav.Handler{FileSystem: xwebdav.Dir(t.TempDir()), LockSystem: xwebdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()

		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	s := newDAV(t, config.StorageWebDAV{URL: srv.URL, Username: "alice", Password: "app-password"})

	require.NoError(t, s.Put(t.Context(), "top.bin", strings.NewReader("x"), 1))

	rc, err := s.Get(t.Context(), "top.bin")
	require.NoError(t, err)
	require.NoError(t, rc.Close())

	require.NoError(t, s.Delete(t.Context(), "top.bin"))

	_, err = s.List(t.Context(), "")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, []string{"PUT /top.bin", "PROPFIND /top.bin", "GET /top.bin", "DELETE /top.bin", "PROPFIND /"}, requests)
}

// TestWebDAVPutStreams checks that an upload is not held in memory, since database backups go through Put.
func TestWebDAVPutStreams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	s := newDAV(t, config.StorageWebDAV{URL: srv.URL})

	const size = 32 << 20

	var before, after runtime.MemStats

	runtime.ReadMemStats(&before)
	require.NoError(t, s.Put(t.Context(), "backup.db.gz", io.LimitReader(zeros{}, size), size))
	runtime.ReadMemStats(&after)

	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(size/4), "bytes allocated uploading %d bytes", size)
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
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

// BenchmarkWebDAVGet reads a small object from an in-process server, so it measures the per-request overhead of Get rather than transfer time.
func BenchmarkWebDAVGet(b *testing.B) {
	h := &xwebdav.Handler{FileSystem: xwebdav.Dir(b.TempDir()), LockSystem: xwebdav.NewMemLS()}
	srv := httptest.NewServer(h)
	b.Cleanup(srv.Close)

	s, err := blobstore.NewWebDAV(config.StorageWebDAV{URL: srv.URL})
	require.NoError(b, err)
	require.NoError(b, s.Put(b.Context(), "backups/encore.db.gz", strings.NewReader("db"), 2))

	for b.Loop() {
		rc, err := s.Get(b.Context(), "backups/encore.db.gz")
		if err != nil {
			b.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, rc)
		_ = rc.Close()
	}
}
