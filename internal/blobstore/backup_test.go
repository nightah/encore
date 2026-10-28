package blobstore_test

import (
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/store"
)

func TestBackupWritesAndPrunes(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()

	db, err := store.Open(ctx, store.Options{Path: store.DefaultPath(dataDir)})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	require.NoError(t, db.Settings().Set(ctx, "marker", "backed-up"))

	s, err := blobstore.NewLocal(filepath.Join(t.TempDir(), "blobs"))
	require.NoError(t, err)

	// Older backups, plus objects the pruner must leave alone.
	for _, k := range []string{
		"backups/encore-20260101T030000.000Z.db.gz",
		"backups/encore-20260102T030000.000Z.db.gz",
		"backups/encore-20260103T030000.000Z.db.gz",
		"backups/encore-notatime.db.gz",
		"backups/notes.txt",
	} {
		require.NoError(t, s.Put(ctx, k, strings.NewReader("old"), 3))
	}

	key, err := blobstore.Backup(ctx, db, s, "backups", 2)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(key, "backups/encore-"))

	backups, err := blobstore.ListBackups(ctx, s, "backups")
	require.NoError(t, err)
	require.Len(t, backups, 2)
	assert.Equal(t, key, backups[0].Key, "newest first")
	assert.Equal(t, "backups/encore-20260103T030000.000Z.db.gz", backups[1].Key)

	all, err := s.List(ctx, "backups/")
	require.NoError(t, err)
	assert.Len(t, all, 4, "unrelated objects survive pruning")

	rc, err := s.Get(ctx, key)
	require.NoError(t, err)

	defer rc.Close()

	gz, err := gzip.NewReader(rc)
	require.NoError(t, err)

	restored := filepath.Join(t.TempDir(), "restored.db")
	f, err := os.Create(restored)
	require.NoError(t, err)
	_, err = io.Copy(f, gz)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	raw, err := sql.Open("sqlite", "file:"+restored+"?mode=ro")
	require.NoError(t, err)
	defer raw.Close()

	var v string
	require.NoError(t, raw.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'marker'`).Scan(&v))
	assert.JSONEq(t, `"backed-up"`, v)

	entries, err := os.ReadDir(dataDir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".backup-"), "staging directory removed: %s", e.Name())
	}
}

func TestBackupRejectsBadArguments(t *testing.T) {
	ctx := t.Context()

	db, err := store.Open(ctx, store.Options{Path: store.DefaultPath(t.TempDir())})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	s, err := blobstore.NewLocal(t.TempDir())
	require.NoError(t, err)

	_, err = blobstore.Backup(ctx, db, s, "backups", 0)
	require.Error(t, err)

	_, err = blobstore.Backup(ctx, db, s, "../escape", 3)
	require.ErrorIs(t, err, blobstore.ErrInvalidKey)

	_, err = blobstore.Backup(ctx, fileDB{dir: t.TempDir(), err: assert.AnError}, s, "../escape", 3)
	require.ErrorIs(t, err, blobstore.ErrInvalidKey)
	require.NotErrorIs(t, err, assert.AnError, "a bad prefix is refused before the database is copied")

	key, err := blobstore.Backup(ctx, db, s, "", 3)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(key, "encore-"), "an empty prefix writes at the root")
}

func TestFetchBackup(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()

	db, err := store.Open(ctx, store.Options{Path: store.DefaultPath(dataDir)})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	require.NoError(t, db.Settings().Set(ctx, "marker", "fetched"))

	s, err := blobstore.NewLocal(t.TempDir())
	require.NoError(t, err)

	key, err := blobstore.Backup(ctx, db, s, "backups", 3)
	require.NoError(t, err)

	taken, ok := blobstore.BackupTime(key)
	require.True(t, ok)
	assert.WithinDuration(t, time.Now(), taken, time.Minute)

	_, ok = blobstore.BackupTime("backups/notes.txt")
	assert.False(t, ok)

	dir := t.TempDir()
	path, err := blobstore.FetchBackup(ctx, s, key, dir)
	require.NoError(t, err)
	assert.Equal(t, dir, filepath.Dir(path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	fi, err := store.InspectFile(ctx, path)
	require.NoError(t, err)
	assert.Positive(t, fi.SchemaVersion)

	require.NoError(t, s.Put(ctx, "backups/encore-20260101T030000.000Z.db.gz", strings.NewReader("not gzip"), 8))
	_, err = blobstore.FetchBackup(ctx, s, "backups/encore-20260101T030000.000Z.db.gz", dir)
	require.Error(t, err)

	_, err = blobstore.FetchBackup(ctx, s, "backups/missing.db.gz", dir)
	require.ErrorIs(t, err, blobstore.ErrNotFound)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "failed fetches leave nothing behind")
}

// fileDB is a Database whose backup is a fixed file body.
type fileDB struct {
	dir string
	err error
}

func (d fileDB) Path() string { return filepath.Join(d.dir, "encore.db") }

func (d fileDB) BackupTo(_ context.Context, path string) error {
	if d.err != nil {
		return d.err
	}

	return os.WriteFile(path, []byte("SQLite format 3\x00 and more"), 0o600)
}

// failingStore wraps a Local store and fails the operations whose error is set.
type failingStore struct {
	*blobstore.Local

	putErr, listErr, deleteErr error
}

func (f *failingStore) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if f.putErr != nil {
		return f.putErr
	}

	return f.Local.Put(ctx, key, r, size)
}

func (f *failingStore) List(ctx context.Context, prefix string) ([]blobstore.Object, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return f.Local.List(ctx, prefix)
}

func (f *failingStore) Delete(ctx context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}

	return f.Local.Delete(ctx, key)
}

func newFailingStore(t *testing.T) *failingStore {
	t.Helper()

	l, err := blobstore.NewLocal(t.TempDir())
	require.NoError(t, err)

	return &failingStore{Local: l}
}

func TestBackupFailures(t *testing.T) {
	ctx := t.Context()

	t.Run("staging directory", func(t *testing.T) {
		_, err := blobstore.Backup(ctx, fileDB{dir: filepath.Join(t.TempDir(), "missing")}, newFailingStore(t), "backups", 3)
		require.ErrorContains(t, err, "staging directory")
	})

	t.Run("database copy", func(t *testing.T) {
		dir := t.TempDir()

		_, err := blobstore.Backup(ctx, fileDB{dir: dir, err: assert.AnError}, newFailingStore(t), "backups", 3)
		require.ErrorIs(t, err, assert.AnError)

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries, "the staging directory is removed")
	})

	t.Run("upload", func(t *testing.T) {
		s := newFailingStore(t)
		s.putErr = assert.AnError

		key, err := blobstore.Backup(ctx, fileDB{dir: t.TempDir()}, s, "backups", 3)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "uploading backup")
		assert.Empty(t, key)
	})

	t.Run("listing for pruning", func(t *testing.T) {
		s := newFailingStore(t)
		s.listErr = assert.AnError

		key, err := blobstore.Backup(ctx, fileDB{dir: t.TempDir()}, s, "backups", 3)
		require.ErrorContains(t, err, "listing backups to prune")
		assert.NotEmpty(t, key, "the backup was taken, so its key is returned with the error")

		_, err = s.Get(ctx, key)
		require.NoError(t, err)
	})

	t.Run("pruning", func(t *testing.T) {
		s := newFailingStore(t)

		for _, k := range []string{"backups/encore-20260101T030000.000Z.db.gz", "backups/encore-20260102T030000.000Z.db.gz"} {
			require.NoError(t, s.Put(ctx, k, strings.NewReader("old"), 3))
		}

		s.deleteErr = assert.AnError

		key, err := blobstore.Backup(ctx, fileDB{dir: t.TempDir()}, s, "backups/", 1)
		require.ErrorContains(t, err, "pruning old backups")
		require.ErrorIs(t, err, assert.AnError)
		assert.True(t, strings.HasPrefix(key, "backups/encore-"), "a trailing slash on the prefix is not doubled: %s", key)

		backups, err := blobstore.ListBackups(ctx, s, "backups")
		require.NoError(t, err)
		assert.Len(t, backups, 3, "nothing was pruned")
	})
}

func TestListBackupsReportsStorageErrors(t *testing.T) {
	s := newFailingStore(t)
	s.listErr = assert.AnError

	_, err := blobstore.ListBackups(t.Context(), s, "backups")
	require.ErrorIs(t, err, assert.AnError)
}

func TestBackupTime(t *testing.T) {
	want := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)

	for key, ok := range map[string]bool{
		"encore-20261026T030000.000Z.db.gz":           true,
		"backups/encore-20261026T030000.000Z.db.gz":   true,
		"a/b/c/encore-20261026T030000.000Z.db.gz":     true,
		"backups/encore-20261026T030000.000Z.db":      false,
		"backups/encore-20261026T030000.000Z":         false,
		"backups/other-20261026T030000.000Z.db.gz":    false,
		"backups/encore-2026-10-26.db.gz":             false,
		"encore-20261026T030000.000Z.db.gz/notes.txt": false,
		"backups/encore-20261326T030000.000Z.db.gz":   false,
		"backups/xencore-20261026T030000.000Z.db.gz":  false,
		"": false,
		"backups/encore-20261026T030000.000Z.db.gz.backup": false,
	} {
		got, gotOK := blobstore.BackupTime(key)
		assert.Equal(t, ok, gotOK, key)

		if ok {
			assert.True(t, got.Equal(want), key)
		}
	}
}

func TestFetchBackupFailures(t *testing.T) {
	ctx := t.Context()

	s, err := blobstore.NewLocal(t.TempDir())
	require.NoError(t, err)

	var gz strings.Builder

	zw := gzip.NewWriter(&gz)
	_, err = zw.Write([]byte(strings.Repeat("page ", 10000)))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	whole := gz.String()
	require.NoError(t, s.Put(ctx, "whole.db.gz", strings.NewReader(whole), int64(len(whole))))
	require.NoError(t, s.Put(ctx, "truncated.db.gz", strings.NewReader(whole[:len(whole)/2]), int64(len(whole)/2)))

	_, err = blobstore.FetchBackup(ctx, s, "whole.db.gz", filepath.Join(t.TempDir(), "missing"))
	require.ErrorContains(t, err, "creating restore file")

	dir := t.TempDir()

	_, err = blobstore.FetchBackup(ctx, s, "truncated.db.gz", dir)
	require.ErrorContains(t, err, "decompressing backup truncated.db.gz")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	_, err = blobstore.FetchBackup(cancelled, s, "whole.db.gz", dir)
	require.ErrorIs(t, err, context.Canceled)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "failed fetches leave nothing behind")
}
