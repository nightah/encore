package blobstore

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/encrypt"
)

// Store is an object store.
type Store interface {
	// Put writes r under key, replacing any existing object. size is the exact length, or -1 if unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	// Get opens an object for reading, or returns ErrNotFound. The caller must close it.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes an object. Deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// List returns the objects whose keys start with prefix, sorted by key.
	List(ctx context.Context, prefix string) ([]Object, error)
	// Probe writes, reads back and deletes a small object to check the store is usable, returning a descriptive error if not.
	Probe(ctx context.Context) error
}

// Object describes a stored object.
type Object struct {
	Key      string
	Size     int64
	Modified time.Time
}

// Database is what Backup needs from the database; *store.DB implements it.
type Database interface {
	// BackupTo writes a consistent copy of the database to a path that does not exist yet.
	BackupTo(ctx context.Context, path string) error
	// Path is the database file; the backup is staged in its directory, which is known to be writable even in a read-only container.
	Path() string
}

// Local stores objects as files under a directory. Writes are atomic (temporary file, fsync, rename) and every operation goes through os.Root, so keys cannot escape the directory even via symlinks.
type Local struct {
	dir string
}

// S3 stores objects in an S3-compatible bucket.
type S3 struct {
	client *minio.Client
	bucket string
	prefix string
	sse    encrypt.ServerSide
}

// WebDAV stores objects on a WebDAV server such as Nextcloud or ownCloud, using basic authentication (an app password for Nextcloud). Intermediate collections are created on Put. List walks collections one level at a time because Nextcloud and many others refuse Depth: infinity.
type WebDAV struct {
	url       string
	prefix    string
	auth      string
	transport http.RoundTripper
}

type ctxTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}
