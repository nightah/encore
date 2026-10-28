package cli

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
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
	assert.Equal(t, []string{"KEY", "TIME", "SIZE"}, strings.Fields(lines[0]))

	objects, err := f.blobs.List(t.Context(), config.Defaults().Storage.Backups.Prefix)
	require.NoError(t, err)

	sizes := map[string]int64{}
	for _, o := range objects {
		sizes[o.Key] = o.Size
	}

	for i, key := range []string{f.backup, older} {
		taken, ok := blobstore.BackupTime(key)
		require.True(t, ok, key)

		want := strings.Fields(key + " " + taken.Local().Format(time.DateTime) + " " + formatSize(sizes[key]))
		assert.Equal(t, want, strings.Fields(lines[i+1]), "newest first, with when it was taken and its size")
	}
}

func TestRestoreUsesStorageSavedInTheDatabase(t *testing.T) {
	f := newFixture(t, "")

	// Move storage in the web interface: the override lives in the current database.
	elsewhere := t.TempDir()
	db := openStore(t, f.dbPath())
	require.NoError(t, db.Settings().Set(t.Context(), "config.overrides", map[string]any{"storage.local.path": elsewhere, "storage.backups.prefix": "Lounge"}))
	require.NoError(t, db.Close())

	moved, err := blobstore.NewLocal(elsewhere)
	require.NoError(t, err)

	key := putBackup(t, moved, f.dbPath(), restoreNow)
	renamed := "Lounge/" + filepath.Base(key)
	require.NoError(t, moved.Put(t.Context(), renamed, strings.NewReader("x"), 1))

	var out bytes.Buffer
	require.NoError(t, f.restorer(&out).list(t.Context()))
	assert.Contains(t, out.String(), renamed, "the saved prefix is used")
	assert.NotContains(t, out.String(), key, "not the configured prefix")
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

func TestRestoreRefusesATruncatedDownload(t *testing.T) {
	f := newFixture(t, "")

	raw, err := os.ReadFile(f.dbPath())
	require.NoError(t, err)

	var gz bytes.Buffer

	zw := gzip.NewWriter(&gz)
	_, err = zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	half := gz.Bytes()[:gz.Len()/2]
	key := config.Defaults().Storage.Backups.Prefix + "/encore-" + time.Now().Add(time.Hour).UTC().Format("20060102T150405.000Z") + ".db.gz"
	require.NoError(t, f.blobs.Put(t.Context(), key, bytes.NewReader(half), int64(len(half))))

	err = f.restorer(io.Discard).restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decompressing backup "+key)

	f.assertUntouched(t)
}

func TestRestoreStopsWhenTheAnswerCannotBeRead(t *testing.T) {
	f := newFixture(t, "")
	errBroken := errors.New("terminal went away")

	r := f.restorer(io.Discard)
	r.yes = false
	r.interactive = true
	r.in = failingIO{errBroken}

	require.ErrorIs(t, r.restore(t.Context(), ""), errBroken)
	f.assertUntouched(t)
}

func TestRestoreCommandValidatesItsArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"restore"}, "name a backup key, or pass --latest or --list"},
		{[]string{"restore", "--latest", "Encore/encore-1.db.gz"}, "name a backup key, or pass --latest or --list"},
		{[]string{"restore", "--list", "Encore/encore-1.db.gz"}, "--list takes no backup key"},
		{[]string{"restore", "--list", "--latest"}, "--list takes no backup key"},
		{[]string{"restore", "one", "two"}, "accepts at most 1 arg"},
	} {
		_, _, err := runCLI(t, tc.args...)
		require.Error(t, err, tc.args)
		assert.Contains(t, err.Error(), tc.want, tc.args)
	}
}

func TestRestoreCommand(t *testing.T) {
	f := newFixture(t, "")
	t.Setenv("ENCORE_DATA_DIR", f.cfg.DataDir)

	out, _, err := runCLI(t, "restore", "--list")
	require.NoError(t, err)
	assert.Contains(t, out, f.backup)

	_, _, err = runCLI(t, "restore", "--latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input is not a terminal; pass --yes", "piped input can't confirm")
	f.assertUntouched(t)

	out, _, err = runCLI(t, "restore", f.backup, "-y")
	require.NoError(t, err)
	assert.Contains(t, out, "Restored "+f.backup)
	assert.Equal(t, "before", marker(t, f.dbPath()))
}

func TestRestoreCommandReportsAConfigError(t *testing.T) {
	_, _, err := runCLI(t, "restore", "--list", "--config", filepath.Join(t.TempDir(), "missing.yml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing.yml")
}

func emptyRestorer(t *testing.T) *restorer {
	t.Helper()

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()

	return &restorer{cfg: &cfg, in: strings.NewReader(""), out: io.Discard, yes: true, now: func() time.Time { return restoreNow }}
}

func TestRestoreListWithNoBackups(t *testing.T) {
	r := emptyRestorer(t)
	r.cfg.Storage.Backend = ""

	var out bytes.Buffer
	r.out = &out

	require.NoError(t, r.list(t.Context()))
	assert.Equal(t, "No backups under \"Encore\" in local storage.\n", out.String())
}

func TestRestoreLatestWithNoBackups(t *testing.T) {
	r := emptyRestorer(t)

	err := r.restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `there are no backups under "Encore"`)
	assert.NoFileExists(t, store.DefaultPath(r.cfg.DataDir))
}

func TestRestoreOpenFailures(t *testing.T) {
	t.Run("invalid secrets key", func(t *testing.T) {
		r := emptyRestorer(t)
		r.cfg.Secrets.Key = "not a key"

		require.Error(t, r.list(t.Context()))
	})

	t.Run("data directory can't be created", func(t *testing.T) {
		r := emptyRestorer(t)
		file := filepath.Join(r.cfg.DataDir, "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		r.cfg.DataDir = filepath.Join(file, "data")

		key, err := secrets.GenerateKey()
		require.NoError(t, err)
		r.cfg.Secrets.Key = secrets.EncodeKey(key)

		err = r.list(t.Context())
		require.Error(t, err)
		require.NotErrorIs(t, err, errLocked)
		assert.Contains(t, err.Error(), "creating data directory")
	})

	t.Run("unreadable current database", func(t *testing.T) {
		r := emptyRestorer(t)
		require.NoError(t, os.WriteFile(store.DefaultPath(r.cfg.DataDir), bytes.Repeat([]byte("junk "), 1024), 0o600))

		err := r.list(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "opening the current database for its storage settings")
	})

	t.Run("unknown storage backend", func(t *testing.T) {
		r := emptyRestorer(t)
		r.cfg.Storage.Backend = "floppy"

		err := r.list(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "opening storage")

		lock, err := lockDataDir(r.cfg.DataDir)
		require.NoError(t, err, "the lock is released after a failed open")
		require.NoError(t, lock.Close())
	})

	t.Run("unknown state storage backend", func(t *testing.T) {
		r := emptyRestorer(t)
		r.cfg.Stateless = true
		r.cfg.Storage.Backend = "floppy"

		err := r.list(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "opening state storage")
	})
}

func TestRestoreReportsAStorageFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}

	r := emptyRestorer(t)

	backups := filepath.Join(t.TempDir(), "blobs", "Encore")
	require.NoError(t, os.MkdirAll(backups, 0o700))
	require.NoError(t, os.Chmod(backups, 0))
	t.Cleanup(func() { _ = os.Chmod(backups, 0o700) })

	r.cfg.Storage.Local.Path = filepath.Dir(backups)

	err := r.list(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing backups")

	err = r.restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing backups")
}

// statelessFixture is a fixture in stateless mode whose saved state is the "after" database, with no local database.
func statelessFixture(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t, "")
	f.cfg.Stateless = true
	f.cfg.Storage.Local.Path = filepath.Join(f.cfg.DataDir, blobstore.LocalDirName)

	db := openStore(t, f.dbPath())
	_, err := (&statestore.Saver{DB: db, Store: f.blobs, Log: slog.New(slog.DiscardHandler), Owner: "pod-1"}).Save(t.Context())
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, os.Remove(f.dbPath()))

	return f
}

func (f *fixture) setOwner(t *testing.T, at time.Time) {
	t.Helper()

	b, err := json.Marshal(map[string]any{"owner": "pod-2", "heartbeat": at})
	require.NoError(t, err)
	require.NoError(t, f.blobs.Put(t.Context(), statestore.KeyOwner, bytes.NewReader(b), int64(len(b))))
}

func (f *fixture) savedMarker(t *testing.T) string {
	t.Helper()

	path := store.DefaultPath(t.TempDir())
	ok, err := statestore.Restore(t.Context(), f.blobs, path)
	require.NoError(t, err)
	require.True(t, ok)

	return marker(t, path)
}

func TestRestoreStatelessRefusesAnUnreadableOwnerMarker(t *testing.T) {
	f := statelessFixture(t)
	require.NoError(t, f.blobs.Put(t.Context(), statestore.KeyOwner, strings.NewReader("{"), 1))

	err := f.restorer(io.Discard).list(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner marker")
}

func TestRestoreStatelessRefusesAnUnreadableSavedState(t *testing.T) {
	f := statelessFixture(t)
	junk := []byte("not gzip")
	require.NoError(t, f.blobs.Put(t.Context(), statestore.KeyDatabase, bytes.NewReader(junk), int64(len(junk))))

	require.Error(t, f.restorer(io.Discard).list(t.Context()))
}

// ownerStartsOnConfirm answers the confirmation prompt, but only after another Encore claimed the storage.
type ownerStartsOnConfirm struct {
	t      *testing.T
	f      *fixture
	answer io.Reader
}

func (r *ownerStartsOnConfirm) Read(p []byte) (int, error) {
	if r.answer == nil {
		r.f.setOwner(r.t, restoreNow)
		r.answer = strings.NewReader("y\n")
	}

	return r.answer.Read(p)
}

func TestRestoreStatelessChecksTheOwnerAgainBeforeUploading(t *testing.T) {
	f := statelessFixture(t)

	r := f.restorer(io.Discard)
	r.yes = false
	r.interactive = true
	r.in = &ownerStartsOnConfirm{t: t, f: f}

	err := r.restore(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `the Encore on "pod-2"`)
	assert.Equal(t, "after", f.savedMarker(t), "the saved state was not replaced")
}

func TestRestoreConfirm(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"  YES  \r\n", true},
		{"y", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"", false},
		{"yeah\n", false},
		{"ja\n", false},
		{"n\ny\n", false},
	} {
		var out bytes.Buffer

		r := &restorer{in: strings.NewReader(tc.input), out: &out}
		ok, err := r.confirm("Replace? [y/N] ")
		require.NoError(t, err, "%q", tc.input)
		assert.Equal(t, tc.want, ok, "%q", tc.input)
		assert.Equal(t, "Replace? [y/N] ", out.String())
	}
}

type failingIO struct{ err error }

func (f failingIO) Read([]byte) (int, error)  { return 0, f.err }
func (f failingIO) Write([]byte) (int, error) { return 0, f.err }

func TestRestoreConfirmFailures(t *testing.T) {
	errBroken := errors.New("broken pipe")

	_, err := (&restorer{in: failingIO{errBroken}, out: io.Discard}).confirm("?")
	require.ErrorIs(t, err, errBroken, "a failed read is not a no")

	_, err = (&restorer{in: strings.NewReader("y\n"), out: failingIO{errBroken}}).confirm("?")
	require.ErrorIs(t, err, errBroken, "the prompt must be shown before reading an answer")
}

func TestVerifyBackupRejectsAForeignDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.ExecContext(t.Context(), `CREATE TABLE notes (body TEXT)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	err = verifyBackup(t.Context(), path, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an Encore database")
}

func TestVerifyBackupWithoutSecretsNeedsNoKey(t *testing.T) {
	path := store.DefaultPath(t.TempDir())
	require.NoError(t, openStore(t, path).Close())

	require.NoError(t, verifyBackup(t.Context(), path, nil))
}

func TestVerifyBackupMissingFile(t *testing.T) {
	require.Error(t, verifyBackup(t.Context(), filepath.Join(t.TempDir(), "missing.db"), nil))
}

func TestSwapDatabaseRefusesToOverwriteAnEarlierCopy(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "encore.db")
	restored := filepath.Join(dir, "restored")

	for _, path := range []string{db, restored, db + ".before-restore-20261026T093000Z-shm"} {
		require.NoError(t, os.WriteFile(path, []byte(filepath.Base(path)), 0o600))
	}

	_, err := swapDatabase(db, restored, restoreNow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	b, err := os.ReadFile(db)
	require.NoError(t, err)
	assert.Equal(t, "encore.db", string(b), "nothing was moved")
	assert.FileExists(t, restored)
}

func TestSwapDatabaseWithoutACurrentDatabase(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "encore.db")
	restored := filepath.Join(dir, "restored")
	require.NoError(t, os.WriteFile(restored, []byte("restored"), 0o600))

	old, err := swapDatabase(db, restored, restoreNow)
	require.NoError(t, err)
	assert.Empty(t, old, "there was nothing to move aside")

	b, err := os.ReadFile(db)
	require.NoError(t, err)
	assert.Equal(t, "restored", string(b))
}

func TestRemoveDBRemovesItsCompanionFiles(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "x.db")

	for _, sfx := range []string{"", "-wal", "-shm"} {
		require.NoError(t, os.WriteFile(db+sfx, nil, 0o600))
	}

	other := filepath.Join(dir, "x.db.before-restore-1")
	require.NoError(t, os.WriteFile(other, nil, 0o600))

	removeDB(db)

	for _, sfx := range []string{"", "-wal", "-shm"} {
		assert.NoFileExists(t, db+sfx)
	}

	assert.FileExists(t, other, "only the database's own files are removed")
	assert.NotPanics(t, func() { removeDB(db) }, "removing again is harmless")
}

func TestIsTerminal(t *testing.T) {
	assert.False(t, isTerminal(strings.NewReader("y\n")), "a reader that isn't a file")

	f, err := os.CreateTemp(t.TempDir(), "in")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	assert.False(t, isTerminal(f), "a regular file")

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	assert.False(t, isTerminal(r), "a pipe")
}

func TestFormatSize(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1 << 20, "1.0 MiB"},
		{3*1<<20 + 1<<19, "3.5 MiB"},
		{5 << 30, "5.0 GiB"},
		{1 << 40, "1.0 TiB"},
		{1 << 50, "1.0 PiB"},
		{1 << 60, "1.0 EiB"},
		{1<<63 - 1, "8.0 EiB"},
	} {
		assert.Equal(t, tc.want, formatSize(tc.n), "%d", tc.n)
	}
}
