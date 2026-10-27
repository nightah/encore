package cli

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/statestore"
	"github.com/nightah/encore/internal/store"
)

var restoreNow = time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)

// fixture is a data directory whose database was backed up with marker "before" and then changed to "after".
type fixture struct {
	cfg    *config.Config
	blobs  blobstore.Store
	backup string
}

// newFixture sets up a data directory with a secret, backs it up to the local backend, then changes it. key is the configured secrets key, or "" for a generated key file.
func newFixture(t *testing.T, key string) *fixture {
	t.Helper()

	ctx := t.Context()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Secrets.Key = key

	db := openStore(t, store.DefaultPath(cfg.DataDir))

	sec, err := secrets.Open(ctx, db.SecretRows(), secrets.Options{Key: key, DataDir: cfg.DataDir})
	require.NoError(t, err)
	require.NoError(t, sec.Put(ctx, "kodi/1/password", []byte("hunter2")))
	require.NoError(t, sec.Close())
	require.NoError(t, db.Settings().Set(ctx, "marker", "before"))

	blobs, err := blobstore.Open(cfg.Storage, cfg.DataDir)
	require.NoError(t, err)

	backup, err := blobstore.Backup(ctx, db, blobs, cfg.Storage.Backups.Prefix, 5)
	require.NoError(t, err)

	require.NoError(t, db.Settings().Set(ctx, "marker", "after"))
	require.NoError(t, db.Close())

	return &fixture{cfg: &cfg, blobs: blobs, backup: backup}
}

func openStore(t *testing.T, path string) *store.DB {
	t.Helper()

	db, err := store.Open(t.Context(), store.Options{Path: path, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)

	return db
}

func marker(t *testing.T, path string) string {
	t.Helper()

	db := openStore(t, path)
	defer db.Close()

	var v string
	require.NoError(t, db.Settings().Get(t.Context(), "marker", &v))

	return v
}

func (f *fixture) restorer(out io.Writer) *restorer {
	return &restorer{cfg: f.cfg, in: strings.NewReader(""), out: out, yes: true, now: func() time.Time { return restoreNow }}
}

func (f *fixture) dbPath() string { return store.DefaultPath(f.cfg.DataDir) }

func putBackup(t *testing.T, s blobstore.Store, path string, at time.Time) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var gz bytes.Buffer

	zw := gzip.NewWriter(&gz)
	_, err = zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	key := config.Defaults().Storage.Backups.Prefix + "/encore-" + at.UTC().Format("20060102T150405.000Z") + ".db.gz"
	require.NoError(t, s.Put(t.Context(), key, &gz, int64(gz.Len())))

	return key
}

func (f *fixture) assertUntouched(t *testing.T) {
	t.Helper()

	assert.Equal(t, "after", marker(t, f.dbPath()), "the current database is unchanged")

	entries, err := os.ReadDir(f.cfg.DataDir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), "before-restore", "nothing was moved aside")
		assert.False(t, strings.HasPrefix(e.Name(), ".restore-"), "the downloaded copy was removed: %s", e.Name())
	}
}

func TestRestoreLatestSwapsDatabase(t *testing.T) {
	f := newFixture(t, "")

	var out bytes.Buffer
	require.NoError(t, f.restorer(&out).restore(t.Context(), ""))

	assert.Equal(t, "before", marker(t, f.dbPath()), "the backup is in place")

	old := f.dbPath() + ".before-restore-20261026T093000Z"
	assert.Equal(t, "after", marker(t, old), "the previous database was kept")
	assert.Contains(t, out.String(), old)
	assert.Contains(t, out.String(), "Restored "+f.backup)
	assert.NotContains(t, out.String(), "hunter2")

	info, err := os.Stat(f.dbPath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	lock, err := lockDataDir(f.cfg.DataDir)
	require.NoError(t, err, "restore released the lock")
	require.NoError(t, lock.Close())
}

func TestRestoreByKeyOrName(t *testing.T) {
	f := newFixture(t, "")

	require.NoError(t, f.restorer(io.Discard).restore(t.Context(), filepath.Base(f.backup)), "a backup can be named by its file name")
	assert.Equal(t, "before", marker(t, f.dbPath()))

	err := f.restorer(io.Discard).restore(t.Context(), "Encore/encore-nope.db.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--list")
}

func TestRestoreList(t *testing.T) {
	f := newFixture(t, "")
	older := putBackup(t, f.blobs, f.dbPath(), restoreNow.Add(-48*time.Hour))

	var out bytes.Buffer
	require.NoError(t, f.restorer(&out).list(t.Context()))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)
	assert.True(t, strings.HasPrefix(lines[0], "KEY"))
	assert.True(t, strings.HasPrefix(lines[1], f.backup), "newest first")
	assert.True(t, strings.HasPrefix(lines[2], older))
}

func TestRestoreUsesStorageSavedInTheDatabase(t *testing.T) {
	f := newFixture(t, "")

	// Move storage in the web interface: the override lives in the current database.
	elsewhere := t.TempDir()
	db := openStore(t, f.dbPath())
	require.NoError(t, db.Settings().Set(t.Context(), "config.overrides", map[string]any{"storage.local.path": elsewhere}))
	require.NoError(t, db.Close())

	moved, err := blobstore.NewLocal(elsewhere)
	require.NoError(t, err)

	key := putBackup(t, moved, f.dbPath(), restoreNow)

	var out bytes.Buffer
	require.NoError(t, f.restorer(&out).list(t.Context()))
	assert.Contains(t, out.String(), key)
	assert.NotContains(t, out.String(), f.backup, "the configured location is overridden")
}

func TestRestoreRefusesWhileServing(t *testing.T) {
	f := newFixture(t, "")

	lock, err := lockDataDir(f.cfg.DataDir)
	require.NoError(t, err)

	defer lock.Close()

	err = f.restorer(io.Discard).restore(t.Context(), "")
	require.ErrorIs(t, err, errLocked)
	assert.Contains(t, err.Error(), "stop it before restoring")
	assert.Contains(t, err.Error(), "pid")

	require.ErrorIs(t, f.restorer(io.Discard).list(t.Context()), errLocked)

	f.assertUntouched(t)
}

func TestRestoreRefusesNewerSchema(t *testing.T) {
	f := newFixture(t, "")

	latest, err := store.SchemaVersion()
	require.NoError(t, err)

	// A backup from a future Encore: same database, one more migration recorded.
	future := filepath.Join(t.TempDir(), "future.db")
	db := openStore(t, f.dbPath())
	require.NoError(t, db.BackupTo(t.Context(), future))
	require.NoError(t, db.Close())

	raw, err := sql.Open("sqlite", future)
	require.NoError(t, err)
	_, err = raw.ExecContext(t.Context(), `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, latest+1)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	// Newer than the fixture's own backup, which carries the real time.
	putBackup(t, f.blobs, future, time.Now().Add(time.Hour))

	err = f.restorer(io.Discard).restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer than this Encore's")
	assert.Contains(t, err.Error(), "current database is unchanged")

	f.assertUntouched(t)
}

func TestRestoreAcceptsOlderSchema(t *testing.T) {
	latest, err := store.SchemaVersion()
	require.NoError(t, err)

	// Migrations are squashed into one until release, so there is no older schema to restore yet.
	if latest < 2 {
		t.Skip("one migration: no older schema yet")
	}

	f := newFixture(t, "")

	past := filepath.Join(t.TempDir(), "past.db")
	db := openStore(t, f.dbPath())
	require.NoError(t, db.BackupTo(t.Context(), past))
	require.NoError(t, db.Close())

	raw, err := sql.Open("sqlite", past)
	require.NoError(t, err)
	_, err = raw.ExecContext(t.Context(), `DELETE FROM goose_db_version WHERE version_id = (SELECT MAX(version_id) FROM goose_db_version)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	key, err := secrets.ReadKey(secrets.Options{DataDir: f.cfg.DataDir})
	require.NoError(t, err)

	info, err := store.InspectFile(t.Context(), past)
	require.NoError(t, err)

	require.Less(t, info.SchemaVersion, latest)

	require.NoError(t, verifyBackup(t.Context(), past, key), "an older schema is fine; serve migrates it")
}

func TestRestoreRefusesWrongKey(t *testing.T) {
	right, err := secrets.GenerateKey()
	require.NoError(t, err)

	f := newFixture(t, secrets.EncodeKey(right))

	wrong, err := secrets.GenerateKey()
	require.NoError(t, err)

	f.cfg.Secrets.Key = secrets.EncodeKey(wrong)

	err = f.restorer(io.Discard).restore(t.Context(), "")
	require.ErrorIs(t, err, secrets.ErrUnknownKey)
	assert.Contains(t, err.Error(), "cannot decrypt")
	assert.NotContains(t, err.Error(), "hunter2")

	f.assertUntouched(t)

	// With no key at all the backup's secrets can't be read either.
	f.cfg.Secrets.Key = ""

	err = f.restorer(io.Discard).restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no secrets key is configured")

	f.assertUntouched(t)
}

func TestRestoreRefusesCorruptBackup(t *testing.T) {
	f := newFixture(t, "")

	junk := filepath.Join(t.TempDir(), "junk.db")
	require.NoError(t, os.WriteFile(junk, bytes.Repeat([]byte("not a database "), 512), 0o600))
	// Newer than the fixture's own backup, which carries the real time.
	putBackup(t, f.blobs, junk, time.Now().Add(time.Hour))

	err := f.restorer(io.Discard).restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a readable SQLite database")

	f.assertUntouched(t)
}

func TestRestoreConfirmation(t *testing.T) {
	f := newFixture(t, "")

	r := f.restorer(io.Discard)
	r.yes = false

	err := r.restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--yes", "no terminal, no --yes: refuse")
	f.assertUntouched(t)

	r.interactive = true
	r.in = strings.NewReader("n\n")

	var out bytes.Buffer
	r.out = &out

	err = r.restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
	assert.Contains(t, out.String(), "[y/N]")
	f.assertUntouched(t)

	r.in = strings.NewReader("y\n")
	require.NoError(t, r.restore(t.Context(), ""))
	assert.Equal(t, "before", marker(t, f.dbPath()))
}

func TestRestoreIntoEmptyDataDir(t *testing.T) {
	f := newFixture(t, "")

	// A new host: storage is configured, the data directory is empty apart from the key.
	fresh := t.TempDir()
	key, err := os.ReadFile(filepath.Join(f.cfg.DataDir, secrets.KeyFileName))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(fresh, secrets.KeyFileName), key, 0o600))

	f.cfg.Storage.Local.Path = filepath.Join(f.cfg.DataDir, blobstore.LocalDirName)
	f.cfg.DataDir = fresh

	var out bytes.Buffer
	require.NoError(t, f.restorer(&out).restore(t.Context(), ""))
	assert.Equal(t, "before", marker(t, store.DefaultPath(fresh)))
	assert.NotContains(t, out.String(), "Moved the previous database")
}

func TestSwapDatabaseMovesWALFiles(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "encore.db")

	for _, name := range []string{"encore.db", "encore.db-wal", "encore.db-shm", "restored"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600))
	}

	old, err := swapDatabase(db, filepath.Join(dir, "restored"), restoreNow)
	require.NoError(t, err)
	assert.Equal(t, db+".before-restore-20261026T093000Z", old)

	for _, sfx := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(old + sfx)
		require.NoError(t, err)
		assert.Equal(t, "encore.db"+sfx, string(b))

		if sfx != "" {
			assert.NoFileExists(t, db+sfx, "the old WAL is not left beside the restored database")
		}
	}

	b, err := os.ReadFile(db)
	require.NoError(t, err)
	assert.Equal(t, "restored", string(b))

	// A failed rename puts everything back.
	require.NoError(t, os.WriteFile(db+"-wal", []byte("wal"), 0o600))

	_, err = swapDatabase(db, filepath.Join(dir, "missing"), restoreNow.Add(time.Second))
	require.Error(t, err)

	b, err = os.ReadFile(db)
	require.NoError(t, err)
	assert.Equal(t, "restored", string(b))
	assert.FileExists(t, db+"-wal")
}

func TestServeLockExcludesASecondHolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")

	first, err := lockDataDir(dir)
	require.NoError(t, err)

	_, err = lockDataDir(dir)
	require.ErrorIs(t, err, errLocked)

	require.NoError(t, first.Close())

	again, err := lockDataDir(dir)
	require.NoError(t, err)
	require.NoError(t, again.Close())
}

func TestRestoreStateless(t *testing.T) {
	ctx := t.Context()

	key, err := secrets.GenerateKey()
	require.NoError(t, err)

	f := newFixture(t, secrets.EncodeKey(key))
	f.cfg.Stateless = true
	f.cfg.Storage.Local.Path = filepath.Join(f.cfg.DataDir, blobstore.LocalDirName)

	// The saved state is the current database ("after").
	db := openStore(t, f.dbPath())
	saver := &statestore.Saver{DB: db, Store: f.blobs, Log: slog.New(slog.DiscardHandler), Owner: "pod-1"}
	_, err = saver.Save(ctx)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// Restore works from the state store alone; the local database plays no part.
	require.NoError(t, os.Remove(f.dbPath()))

	setOwner := func(at time.Time) {
		b, err := json.Marshal(map[string]any{"owner": "pod-1", "heartbeat": at})
		require.NoError(t, err)
		require.NoError(t, f.blobs.Put(ctx, statestore.KeyOwner, bytes.NewReader(b), int64(len(b))))
	}

	setOwner(restoreNow.Add(-time.Minute))

	err = f.restorer(io.Discard).restore(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `the Encore on "pod-1"`)

	setOwner(restoreNow.Add(-time.Hour))

	var out bytes.Buffer
	require.NoError(t, f.restorer(&out).restore(ctx, ""))
	assert.Contains(t, out.String(), statestore.KeyPrevious)

	dir := t.TempDir()
	ok, err := statestore.Restore(ctx, f.blobs, store.DefaultPath(dir))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "before", marker(t, store.DefaultPath(dir)), "the backup is the saved state")

	prev, err := f.blobs.Get(ctx, statestore.KeyPrevious)
	require.NoError(t, err)

	zr, err := gzip.NewReader(prev)
	require.NoError(t, err)

	prevPath := filepath.Join(t.TempDir(), "prev.db")
	raw, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.NoError(t, prev.Close())
	require.NoError(t, os.WriteFile(prevPath, raw, 0o600))
	assert.Equal(t, "after", marker(t, prevPath), "the replaced state is kept as the previous copy")

	assert.NoFileExists(t, f.dbPath(), "stateless restore doesn't write a local database")

	// A wrong key is refused before anything is uploaded.
	wrong, err := secrets.GenerateKey()
	require.NoError(t, err)

	f.cfg.Secrets.Key = secrets.EncodeKey(wrong)
	err = f.restorer(io.Discard).restore(ctx, "")
	require.ErrorIs(t, err, secrets.ErrUnknownKey)
}
