package blobstore_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
)

// fakeS3 answers HeadBucket with 200 and everything else with an S3 AccessDenied error, recording request paths and hosts.
type fakeS3 struct {
	mu       sync.Mutex
	requests []string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.Host+r.URL.Path)
	f.mu.Unlock()

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)

		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied.</Message></Error>`))
}

func (f *fakeS3) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.requests...)
}

func TestS3PathStyleWithCustomCA(t *testing.T) {
	fake := &fakeS3{}
	srv := httptest.NewTLSServer(fake)
	t.Cleanup(srv.Close)

	cfg := config.StorageS3{
		Endpoint:   srv.URL,
		Region:     "us-east-1",
		Bucket:     "encore",
		Prefix:     "prod",
		Addressing: config.AddressingPath,
		AccessKey:  "AKIA",
		SecretKey:  "s3cr3t-value",
	}

	s, err := blobstore.NewS3(cfg)
	require.NoError(t, err)

	err = s.Probe(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate", "the test CA is not trusted by default")

	cfg.CABundle = writeCA(t, srv)
	s, err = blobstore.NewS3(cfg)
	require.NoError(t, err)

	err = s.Probe(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "probe: cannot write")
	assert.Contains(t, err.Error(), "check the access key")
	assert.NotContains(t, err.Error(), "s3cr3t-value")

	host := strings.TrimPrefix(srv.URL, "https://")
	reqs := fake.seen()
	require.Len(t, reqs, 2)
	assert.Equal(t, "HEAD "+host+"/encore/", reqs[0])
	assert.True(t, strings.HasPrefix(reqs[1], "PUT "+host+"/encore/prod/.encore-probe-"), "path-style request with prefix: %s", reqs[1])
}

func TestS3InsecureSkipVerify(t *testing.T) {
	srv := httptest.NewTLSServer(&fakeS3{})
	t.Cleanup(srv.Close)

	s, err := blobstore.NewS3(config.StorageS3{
		Endpoint: srv.URL, Bucket: "encore", Addressing: config.AddressingPath, Region: "us-east-1",
		AccessKey: "a", SecretKey: "b", InsecureSkipVerify: true,
	})
	require.NoError(t, err)

	err = s.Probe(t.Context())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "certificate")
	assert.Contains(t, err.Error(), "AccessDenied")
}

func TestS3ConfigErrors(t *testing.T) {
	base := config.StorageS3{Endpoint: "http://minio:9000", Bucket: "b", AccessKey: "a", SecretKey: "s"}

	for name, mutate := range map[string]func(*config.StorageS3){
		"scheme":    func(c *config.StorageS3) { c.Endpoint = "minio:9000" },
		"path":      func(c *config.StorageS3) { c.Endpoint = "http://minio:9000/bucket" },
		"bucket":    func(c *config.StorageS3) { c.Bucket = "" },
		"prefix":    func(c *config.StorageS3) { c.Prefix = "a/../b" },
		"ca bundle": func(c *config.StorageS3) { c.Endpoint = "https://minio:9000"; c.CABundle = "/nonexistent.pem" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)

			_, err := blobstore.NewS3(cfg)
			require.Error(t, err)
		})
	}

	for _, sse := range []string{config.SSES3, config.SSEKMS} {
		cfg := base
		cfg.SSE, cfg.SSEKMSKeyID = sse, "key-1"

		_, err := blobstore.NewS3(cfg)
		require.NoError(t, err, sse)
	}
}
