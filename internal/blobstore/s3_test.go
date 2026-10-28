package blobstore_test

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/blobstore/blobstoretest"
	"github.com/nightah/encore/internal/config"
)

// memS3 is an in-memory S3 server speaking just enough of the API for minio-go with path-style addressing: buckets that exist, single and multipart uploads, GET, DELETE and ListObjectsV2. fail makes every request with a method fail with an S3 error until heal.
type memS3 struct {
	mu       sync.Mutex
	buckets  map[string]map[string]memObject
	uploads  map[string]map[int][]byte
	nextID   int
	failures map[string]s3Failure
}

type memObject struct {
	data     []byte
	modified time.Time
}

type s3Failure struct {
	code   string
	status int
}

func newMemS3(t *testing.T, buckets ...string) (*memS3, *httptest.Server) {
	t.Helper()

	m := &memS3{buckets: map[string]map[string]memObject{}, uploads: map[string]map[int][]byte{}, failures: map[string]s3Failure{}}
	for _, b := range buckets {
		m.buckets[b] = map[string]memObject{}
	}

	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)

	return m, srv
}

func (m *memS3) fail(method, code string, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.failures[method] = s3Failure{code: code, status: status}
}

func (m *memS3) heal() {
	m.mu.Lock()
	defer m.mu.Unlock()

	clear(m.failures)
}

func s3Error(w http.ResponseWriter, code string, status int) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
}

func (m *memS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if f, ok := m.failures[r.Method]; ok {
		s3Error(w, f.code, f.status)

		return
	}

	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

	objects, ok := m.buckets[bucket]
	if !ok {
		s3Error(w, "NoSuchBucket", http.StatusNotFound)

		return
	}

	q := r.URL.Query()

	switch {
	case key == "" && r.Method == http.MethodHead:
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodGet && q.Get("list-type") == "2":
		m.list(w, objects, q.Get("prefix"))
	case r.Method == http.MethodPost && q.Has("uploads"):
		m.nextID++
		id := strconv.Itoa(m.nextID)
		m.uploads[id] = map[int][]byte{}
		writeXML(w, struct {
			XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
			Bucket   string
			Key      string
			UploadID string `xml:"UploadId"`
		}{Bucket: bucket, Key: key, UploadID: id})
	case r.Method == http.MethodPut && q.Has("uploadId"):
		data, err := readBody(r)
		if err != nil {
			s3Error(w, "IncompleteBody", http.StatusBadRequest)

			return
		}

		n, _ := strconv.Atoi(q.Get("partNumber"))
		m.uploads[q.Get("uploadId")][n] = data
		w.Header().Set("ETag", etag(data))
	case r.Method == http.MethodPost && q.Has("uploadId"):
		parts := m.uploads[q.Get("uploadId")]
		numbers := slices.Sorted(func(yield func(int) bool) {
			for n := range parts {
				if !yield(n) {
					return
				}
			}
		})

		var all []byte
		for _, n := range numbers {
			all = append(all, parts[n]...)
		}

		delete(m.uploads, q.Get("uploadId"))
		objects[key] = memObject{data: all, modified: time.Now()}
		writeXML(w, struct {
			XMLName xml.Name `xml:"CompleteMultipartUploadResult"`
			Bucket  string
			Key     string
			ETag    string
		}{Bucket: bucket, Key: key, ETag: etag(all)})
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		delete(m.uploads, q.Get("uploadId"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		data, err := readBody(r)
		if err != nil {
			s3Error(w, "IncompleteBody", http.StatusBadRequest)

			return
		}

		objects[key] = memObject{data: data, modified: time.Now()}
		w.Header().Set("ETag", etag(data))
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		o, ok := objects[key]
		if !ok {
			s3Error(w, "NoSuchKey", http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(o.data)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", etag(o.data))
		w.Header().Set("Last-Modified", o.modified.UTC().Format(http.TimeFormat))

		if r.Method == http.MethodGet {
			_, _ = w.Write(o.data)
		}
	case r.Method == http.MethodDelete:
		delete(objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		s3Error(w, "NotImplemented", http.StatusNotImplemented)
	}
}

func (m *memS3) list(w http.ResponseWriter, objects map[string]memObject, prefix string) {
	type content struct {
		Key          string
		Size         int
		LastModified string
		ETag         string
	}

	var contents []content

	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range objects {
			if !yield(k) {
				return
			}
		}
	}) {
		if strings.HasPrefix(k, prefix) {
			o := objects[k]
			contents = append(contents, content{Key: k, Size: len(o.data), LastModified: o.modified.UTC().Format(time.RFC3339), ETag: etag(o.data)})
		}
	}

	writeXML(w, struct {
		XMLName     xml.Name `xml:"ListBucketResult"`
		Prefix      string
		KeyCount    int
		IsTruncated bool
		Contents    []content
	}{Prefix: prefix, KeyCount: len(contents), Contents: contents})
}

// readBody decodes the aws-chunked framing minio-go uses for signed streaming uploads over plain HTTP.
func readBody(r *http.Request) ([]byte, error) {
	if !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
		return io.ReadAll(r.Body)
	}

	var out []byte

	br := bufio.NewReader(r.Body)

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}

		sizeHex, _, _ := strings.Cut(strings.TrimSpace(line), ";")

		size, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return nil, err
		}

		if size == 0 {
			return out, nil
		}

		chunk := make([]byte, size+2)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return nil, err
		}

		out = append(out, chunk[:size]...)
	}
}

func etag(data []byte) string {
	sum := md5.Sum(data)

	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func writeXML(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(v)
}

func memS3Store(t *testing.T, srv *httptest.Server, bucket, prefix string) *blobstore.S3 {
	t.Helper()

	s, err := blobstore.NewS3(config.StorageS3{
		Endpoint: srv.URL, Region: "us-east-1", Bucket: bucket, Prefix: prefix,
		Addressing: config.AddressingPath, AccessKey: "AKIA", SecretKey: "s3cr3t-value",
	})
	require.NoError(t, err)

	return s
}

func TestS3ContractInMemory(t *testing.T) {
	for _, prefix := range []string{"", "encore/prod"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
				_, srv := newMemS3(t, "encore")

				return memS3Store(t, srv, "encore", prefix)
			})
		})
	}
}

func TestS3PrefixIsolatesObjects(t *testing.T) {
	m, srv := newMemS3(t, "encore")
	prod, staging := memS3Store(t, srv, "encore", "prod"), memS3Store(t, srv, "encore", "/staging/")

	require.NoError(t, prod.Put(t.Context(), "state/db", strings.NewReader("prod"), 4))
	require.NoError(t, staging.Put(t.Context(), "state/db", strings.NewReader("staging"), 7))

	m.mu.Lock()
	stored := slices.Sorted(func(yield func(string) bool) {
		for k := range m.buckets["encore"] {
			if !yield(k) {
				return
			}
		}
	})
	m.mu.Unlock()
	assert.Equal(t, []string{"prod/state/db", "staging/state/db"}, stored, "keys live under the prefix, which is trimmed of slashes")

	objs, err := prod.List(t.Context(), "")
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, "state/db", objs[0].Key, "listed keys are relative to the prefix")
}

func TestS3ErrorsCarryHints(t *testing.T) {
	m, srv := newMemS3(t, "encore")
	s := memS3Store(t, srv, "encore", "")
	ctx := t.Context()

	require.NoError(t, s.Put(ctx, "k", strings.NewReader("v"), 1))

	m.fail(http.MethodHead, "AccessDenied", http.StatusForbidden)
	_, err := s.Get(ctx, "k")
	require.ErrorContains(t, err, "check the access key")
	require.NotErrorIs(t, err, blobstore.ErrNotFound, "a refusal is not a missing object")
	m.heal()

	m.fail(http.MethodDelete, "InvalidRegion", http.StatusBadRequest)
	require.ErrorContains(t, s.Delete(ctx, "k"), "set storage.s3.region")
	m.heal()

	m.fail(http.MethodDelete, "NoSuchKey", http.StatusNotFound)
	require.NoError(t, s.Delete(ctx, "k"), "deleting a missing object is not an error")
	m.heal()

	m.fail(http.MethodGet, "InvalidArgument", http.StatusBadRequest)
	_, err = s.List(ctx, "")
	require.ErrorContains(t, err, "(InvalidArgument)")
	m.heal()

	m.fail(http.MethodPut, "InvalidArgument", http.StatusBadRequest)
	require.ErrorContains(t, s.Put(ctx, "k", strings.NewReader("v"), 1), "uploading k")
	m.heal()

	_, err = s.List(ctx, "../x")
	require.ErrorIs(t, err, blobstore.ErrInvalidKey)

	rc, err := s.Get(ctx, "k")
	require.NoError(t, err, "the object survived the failed calls")
	require.NoError(t, rc.Close())
}

func TestS3MissingBucket(t *testing.T) {
	_, srv := newMemS3(t, "encore")
	s := memS3Store(t, srv, "typo", "")

	require.ErrorContains(t, s.Probe(t.Context()), `bucket "typo" does not exist`)
	require.ErrorContains(t, s.Delete(t.Context(), "k"), "create it first", "a missing bucket is not a missing object")

	_, err := s.List(t.Context(), "")
	require.ErrorContains(t, err, "create it first")
}

func TestS3ProbeBucketCheckFails(t *testing.T) {
	m, srv := newMemS3(t, "encore")
	s := memS3Store(t, srv, "encore", "")

	m.fail(http.MethodHead, "AccessDenied", http.StatusForbidden)

	err := s.Probe(t.Context())
	require.ErrorContains(t, err, `checking bucket "encore"`)
	assert.NotContains(t, err.Error(), "s3cr3t-value")
}

func TestS3PutStreamsLargeUnknownSizes(t *testing.T) {
	_, srv := newMemS3(t, "encore")
	s := memS3Store(t, srv, "encore", "")

	data := bytes.Repeat([]byte("0123456789abcdef"), 1<<16)
	require.NoError(t, s.Put(t.Context(), "big", bytes.NewReader(data), -1))

	rc, err := s.Get(t.Context(), "big")
	require.NoError(t, err)

	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, got))
}

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
