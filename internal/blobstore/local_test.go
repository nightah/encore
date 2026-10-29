package blobstore_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/blobstore/blobstoretest"
	"github.com/nightah/encore/internal/config"
)

func TestLocalContract(t *testing.T) {
	blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
		s, err := blobstore.NewLocal(filepath.Join(t.TempDir(), "blobs"))
		require.NoError(t, err)

		return s
	})
}

func TestLocalPermissionsAndAtomicity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	s, err := blobstore.NewLocal(dir)
	require.NoError(t, err)

	require.NoError(t, s.Put(t.Context(), "a/b.json", strings.NewReader("{}"), 2))

	info, err := os.Stat(filepath.Join(dir, "a", "b.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	info, err = os.Stat(filepath.Join(dir, "a"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	// A short body fails and leaves neither the object nor a temporary file.
	require.Error(t, s.Put(t.Context(), "a/short", strings.NewReader("abc"), 10))

	entries, err := os.ReadDir(filepath.Join(dir, "a"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "b.json", entries[0].Name())
}

func TestLocalSymlinkEscape(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	outside := t.TempDir()

	s, err := blobstore.NewLocal(dir)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link")))

	_, err = s.Get(t.Context(), "link/secret")
	require.Error(t, err, "symlinks out of the root are refused")

	require.Error(t, s.Put(t.Context(), "link/planted", strings.NewReader("x"), 1))
	assert.NoFileExists(t, filepath.Join(outside, "planted"))
}

func TestOpen(t *testing.T) {
	dataDir := t.TempDir()

	s, err := blobstore.Open(config.Storage{Backend: config.BackendLocal}, dataDir)
	require.NoError(t, err)

	local, ok := s.(*blobstore.Local)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dataDir, blobstore.LocalDirName), local.Dir())

	_, err = blobstore.Open(config.Storage{Backend: config.BackendS3, S3: config.StorageS3{Endpoint: "http://minio:9000", Bucket: "b", AccessKey: "a", SecretKey: "s", Addressing: config.AddressingPath}}, dataDir)
	require.NoError(t, err)

	_, err = blobstore.Open(config.Storage{Backend: config.BackendWebDAV, WebDAV: config.StorageWebDAV{URL: "https://cloud.example.com/remote.php/dav/files/a"}}, dataDir)
	require.NoError(t, err)

	_, err = blobstore.Open(config.Storage{Backend: "tape"}, dataDir)
	require.Error(t, err)
}

func TestNewLocalErrors(t *testing.T) {
	_, err := blobstore.NewLocal("")
	require.Error(t, err)

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err = blobstore.NewLocal(filepath.Join(file, "blobs"))
	require.ErrorContains(t, err, "creating storage directory")
}

func TestLocalRootRemoved(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	s, err := blobstore.NewLocal(dir)
	require.NoError(t, err)
	require.NoError(t, os.Remove(dir))

	ctx := t.Context()

	require.ErrorContains(t, s.Put(ctx, "k", strings.NewReader("v"), 1), "opening storage directory")

	_, err = s.Get(ctx, "k")
	require.ErrorContains(t, err, "opening storage directory")
	require.NotErrorIs(t, err, blobstore.ErrNotFound, "a missing store is not a missing object")

	require.ErrorContains(t, s.Delete(ctx, "k"), "opening storage directory")

	_, err = s.List(ctx, "")
	require.ErrorContains(t, err, "opening storage directory")
}

func TestLocalPutFailuresLeaveNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	s, err := blobstore.NewLocal(dir)
	require.NoError(t, err)

	ctx := t.Context()

	require.ErrorIs(t, s.Put(ctx, "a/broken", iotest.ErrReader(assert.AnError), -1), assert.AnError)
	require.Error(t, s.Put(ctx, "a/long", strings.NewReader("too long"), 3), "more data than the size is refused")
	require.Error(t, s.Put(ctx, "a/empty", strings.NewReader("x"), 0), "a size of zero is a size too")

	entries, err := os.ReadDir(filepath.Join(dir, "a"))
	require.NoError(t, err)
	assert.Empty(t, entries, "no temporary file is left behind")

	require.NoError(t, s.Put(ctx, "file", strings.NewReader("x"), 1))
	require.ErrorContains(t, s.Put(ctx, "file/child", strings.NewReader("x"), 1), "creating directory for file/child")
}

func TestLocalDeleteLeavesDirectories(t *testing.T) {
	s, err := blobstore.NewLocal(filepath.Join(t.TempDir(), "blobs"))
	require.NoError(t, err)

	ctx := t.Context()
	require.NoError(t, s.Put(ctx, "dir/file", strings.NewReader("x"), 1))

	require.NoError(t, s.Delete(ctx, "dir"), "a directory is not an object, so there is nothing to delete")

	objs, err := s.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, objs, 1, "the directory's contents survive")
	assert.Equal(t, "dir/file", objs[0].Key)
}

func TestLocalListSortsByKeyNotByDirectory(t *testing.T) {
	s, err := blobstore.NewLocal(filepath.Join(t.TempDir(), "blobs"))
	require.NoError(t, err)

	ctx := t.Context()
	for _, k := range []string{"a/b", "a-c", "a.d/e"} {
		require.NoError(t, s.Put(ctx, k, strings.NewReader("x"), 1))
	}

	objs, err := s.List(ctx, "a")
	require.NoError(t, err)

	keys := make([]string, 0, len(objs))
	for _, o := range objs {
		keys = append(keys, o.Key)
	}

	assert.Equal(t, []string{"a-c", "a.d/e", "a/b"}, keys, "'-' and '.' sort before '/', unlike a directory walk")
}

func TestLocalListSkipsInternalFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobs")
	s, err := blobstore.NewLocal(dir)
	require.NoError(t, err)

	ctx := t.Context()
	require.NoError(t, s.Put(ctx, "a/real", strings.NewReader("x"), 1))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", ".tmp-0123"), []byte("in flight"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(dir, "a", "real"), filepath.Join(dir, "a", "link")))

	objs, err := s.List(ctx, "a/")
	require.NoError(t, err)
	require.Len(t, objs, 1, "in-flight writes and symlinks are not objects")
	assert.Equal(t, "a/real", objs[0].Key)
	assert.EqualValues(t, 1, objs[0].Size)
	assert.Equal(t, time.UTC, objs[0].Modified.Location())

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	_, err = s.List(cancelled, "a/")
	require.ErrorIs(t, err, context.Canceled)
}

func TestLocalUnreadableObject(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}

	dir := filepath.Join(t.TempDir(), "blobs")
	s, err := blobstore.NewLocal(dir)
	require.NoError(t, err)
	require.NoError(t, s.Put(t.Context(), "k", strings.NewReader("x"), 1))
	require.NoError(t, os.Chmod(filepath.Join(dir, "k"), 0o000))

	_, err = s.Get(t.Context(), "k")
	require.ErrorContains(t, err, "opening k")
	require.NotErrorIs(t, err, blobstore.ErrNotFound, "an unreadable object is not a missing one")
}

func TestValidateKey(t *testing.T) {
	for _, k := range []string{"a", "a/b", "a/b.c", "backups/encore-20261026T030000.000Z.db.gz", "with space/ü", strings.Repeat("a", 1024)} {
		require.NoError(t, blobstore.ValidateKey(k), k)
	}

	for _, k := range []string{"", "/a", "a/", "a//b", ".", "..", "a/..", "./a", "a\\b", "a\x00b", "\xff", strings.Repeat("a", 1025)} {
		require.ErrorIs(t, blobstore.ValidateKey(k), blobstore.ErrInvalidKey, "%q", k)
	}
}
