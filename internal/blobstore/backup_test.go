package blobstore_test

import (
	"compress/gzip"
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
