package blobstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestValidateKey(t *testing.T) {
	for _, k := range []string{"a", "a/b", "a/b.c", "backups/encore-20261026T030000.000Z.db.gz", "with space/ü"} {
		require.NoError(t, blobstore.ValidateKey(k), k)
	}

	for _, k := range []string{"", "/a", "a/", "a//b", ".", "..", "a/..", "./a", "a\\b", "a\x00b", "\xff", strings.Repeat("a", 1025)} {
		require.ErrorIs(t, blobstore.ValidateKey(k), blobstore.ErrInvalidKey, "%q", k)
	}
}
