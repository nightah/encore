package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/core/media"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

func openTest(t *testing.T, opts Options) (*DB, *clock) {
	t.Helper()

	clk := &clock{t: time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)}

	if opts.Path == "" {
		opts.Path = filepath.Join(t.TempDir(), FileName)
	}

	if opts.Now == nil {
		opts.Now = clk.Now
	}

	db, err := Open(t.Context(), opts)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	return db, clk
}

func TestOpenPragmasAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	db, _ := openTest(t, Options{Path: DefaultPath(dir)})

	ctx := t.Context()

	var mode string
	require.NoError(t, db.write.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode))
	assert.Equal(t, "wal", mode)

	var fk, busy int
	require.NoError(t, db.write.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk))
	require.NoError(t, db.write.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy))
	assert.Equal(t, 1, fk)
	assert.Equal(t, 5000, busy)

	_, err := db.read.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ('x', '1', 0)`)
	require.Error(t, err, "the read pool must be read-only")

	info, err := os.Stat(db.Path())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	info, err = os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	require.NoError(t, db.Ping(ctx))
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	db, _ := openTest(t, Options{Path: path})
	require.NoError(t, db.Settings().Set(t.Context(), "k", "v"))
	require.NoError(t, db.Close())

	db2, err := Open(t.Context(), Options{Path: path})
	require.NoError(t, err)
	defer db2.Close()

	v, err := Setting(t.Context(), db2.Settings(), "k", "")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	_, err := Open(t.Context(), Options{})
	assert.Error(t, err)
}

func TestConcurrentWritesDoNotBusy(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()

	var wg sync.WaitGroup

	errs := make(chan error, 50)

	for range 50 {
		wg.Go(func() {
			_, err := db.EchoDevices().Seen(ctx, "amzn1.ask.device.A")
			errs <- err
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	d, err := db.EchoDevices().GetByAlexaID(ctx, "amzn1.ask.device.A")
	require.NoError(t, err)
	assert.EqualValues(t, 50, d.RequestCount)
}

func TestBackup(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()

	require.NoError(t, db.Settings().Set(ctx, "greeting", "hello"))

	var buf bytes.Buffer
	require.NoError(t, db.Backup(ctx, &buf))

	restored := filepath.Join(t.TempDir(), "restored.db")
	require.NoError(t, os.WriteFile(restored, buf.Bytes(), 0o600))

	raw, err := sql.Open("sqlite", "file:"+restored+"?mode=ro")
	require.NoError(t, err)
	defer raw.Close()

	var v string
	require.NoError(t, raw.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'greeting'`).Scan(&v))
	assert.JSONEq(t, `"hello"`, v)

	entries, err := os.ReadDir(filepath.Dir(db.Path()))
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".backup-", "staging directory must be removed")
	}
}

func TestBackupToRefusesExisting(t *testing.T) {
	db, _ := openTest(t, Options{})

	path := filepath.Join(t.TempDir(), "b.db")
	require.NoError(t, db.BackupTo(t.Context(), path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.Error(t, db.BackupTo(t.Context(), path))

	// VACUUM INTO would write into an empty file or through a symlink, so anything already at the path is refused.
	empty := filepath.Join(t.TempDir(), "empty.db")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	require.Error(t, db.BackupTo(t.Context(), empty))

	info, err = os.Stat(empty)
	require.NoError(t, err)
	assert.Zero(t, info.Size(), "an existing file is left alone")

	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.db")
	link := filepath.Join(dir, "link.db")
	require.NoError(t, os.Symlink(target, link))
	require.Error(t, db.BackupTo(t.Context(), link))
	assert.NoFileExists(t, target, "a dangling symlink is not followed")
}

func TestBackupHonoursContext(t *testing.T) {
	db, _ := openTest(t, Options{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.Error(t, db.Backup(ctx, &bytes.Buffer{}))
}

func TestNormalise(t *testing.T) {
	tests := map[string]string{
		"Kid's  Room":      "kids room",
		"KIDS-ROOM":        "kids room",
		"  the Lounge!  ":  "the lounge",
		"Café":             "cafe",
		"Wohnzimmer Ünten": "wohnzimmer unten",
		"Bedroom’s 2":      "bedrooms 2",
		"!!!":              "",
		"ｆｕｌｌ ｗｉｄｔｈ":       "full width",
	}

	for in, want := range tests {
		assert.Equal(t, want, Normalise(in), in)
	}
}

func TestNormaliseProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "s")
		n := Normalise(s)

		assert.Equal(t, n, Normalise(n), "normalising is idempotent")
		assert.Equal(t, strings.TrimSpace(n), n, "no leading or trailing space")
		assert.NotContains(t, n, "  ", "whitespace is collapsed")
	})
}

func TestOpenFailures(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	junk := filepath.Join(dir, "junk.db")
	require.NoError(t, os.WriteFile(junk, bytes.Repeat([]byte("not a database "), 512), 0o600))

	for name, path := range map[string]string{
		"parent is a file":  filepath.Join(file, FileName),
		"path is a dir":     dir,
		"not a sqlite file": junk,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Open(t.Context(), Options{Path: path})
			require.Error(t, err)
		})
	}
}

func TestOpenRefusesConflictingSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	raw, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	_, err = raw.ExecContext(t.Context(), `CREATE TABLE settings (unrelated TEXT)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	_, err = Open(t.Context(), Options{Path: path})
	require.ErrorContains(t, err, "migrating database")
}

func TestOpenReadConns(t *testing.T) {
	db, _ := openTest(t, Options{ReadConns: 2})
	assert.Equal(t, 2, db.read.Stats().MaxOpenConnections)
	assert.Equal(t, 1, db.write.Stats().MaxOpenConnections, "a single writer serialises writes")

	db, _ = openTest(t, Options{})
	assert.GreaterOrEqual(t, db.read.Stats().MaxOpenConnections, 4)
}

func TestPingReportsWhichPoolFailed(t *testing.T) {
	db, err := Open(t.Context(), Options{Path: filepath.Join(t.TempDir(), FileName)})
	require.NoError(t, err)

	require.NoError(t, db.read.Close())
	require.ErrorContains(t, db.Ping(t.Context()), "read pool")

	require.NoError(t, db.write.Close())
	require.ErrorContains(t, db.Ping(t.Context()), "writer")
}

func TestMillisRoundTrip(t *testing.T) {
	assert.Zero(t, toMillis(time.Time{}), "the zero time is stored as 0")
	assert.True(t, fromMillis(0).IsZero(), "0 reads back as the zero time")

	rapid.Check(t, func(t *rapid.T) {
		ms := rapid.Int64Range(1, 1<<45).Draw(t, "ms")
		ns := rapid.Int64Range(0, int64(time.Millisecond)-1).Draw(t, "ns")
		in := time.UnixMilli(ms).Add(time.Duration(ns)).In(time.FixedZone("AEST", 10*60*60))

		out := fromMillis(toMillis(in))
		assert.Equal(t, time.UTC, out.Location())
		assert.True(t, out.Equal(in.Truncate(time.Millisecond)), "%v != %v", out, in)
	})
}

func TestMapErr(t *testing.T) {
	require.NoError(t, mapErr(nil))
	require.ErrorIs(t, mapErr(sql.ErrNoRows), ErrNotFound)
	require.ErrorIs(t, mapErr(sql.ErrNoRows), sql.ErrNoRows, "the cause is kept")

	plain := errors.New("disk full")
	assert.Equal(t, plain, mapErr(plain), "unknown errors pass through unchanged")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }

func TestBackupReportsWriterErrors(t *testing.T) {
	db, _ := openTest(t, Options{})

	require.ErrorIs(t, db.Backup(t.Context(), failingWriter{}), io.ErrShortWrite)

	entries, err := os.ReadDir(filepath.Dir(db.Path()))
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".backup-", "the staging directory is removed after a failure too")
	}
}

func TestBackupNeedsAWritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	db, _ := openTest(t, Options{})
	dir := filepath.Dir(db.Path())

	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	require.ErrorContains(t, db.Backup(t.Context(), &bytes.Buffer{}), "staging directory")
}

// TestClosedDatabaseErrors checks every repository reports a driver failure instead of an empty or missing result.
func TestClosedDatabaseErrors(t *testing.T) {
	db, err := Open(t.Context(), Options{Path: filepath.Join(t.TempDir(), FileName)})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	ctx := t.Context()
	k := newKodi("lounge")
	k.ID = 1

	ignore := func(_ any, err error) error { return err }

	for name, call := range map[string]func() error{
		"BackupTo":                  func() error { return db.BackupTo(ctx, filepath.Join(t.TempDir(), "b.db")) },
		"Settings.Set":              func() error { return db.Settings().Set(ctx, "k", "v") },
		"Settings.Delete":           func() error { return db.Settings().Delete(ctx, "k") },
		"Settings.Get":              func() error { return db.Settings().Get(ctx, "k", new(string)) },
		"Settings.All":              func() error { return ignore(db.Settings().All(ctx)) },
		"Setting":                   func() error { return ignore(Setting(ctx, db.Settings(), "k", 0)) },
		"Sessions.Create":           func() error { return db.Sessions().Create(ctx, &Session{IDHash: []byte{1}}) },
		"Sessions.Get":              func() error { return ignore(db.Sessions().Get(ctx, []byte{1})) },
		"Sessions.Touch":            func() error { return db.Sessions().Touch(ctx, []byte{1}, time.Now()) },
		"Sessions.Delete":           func() error { return db.Sessions().Delete(ctx, []byte{1}) },
		"Sessions.DeleteUser":       func() error { return ignore(db.Sessions().DeleteUser(ctx, "admin")) },
		"Sessions.DeleteAllExcept":  func() error { return ignore(db.Sessions().DeleteAllExcept(ctx, nil)) },
		"Sessions.DeleteExpired":    func() error { return ignore(db.Sessions().DeleteExpired(ctx)) },
		"SecretRows.Put":            func() error { return db.SecretRows().Put(ctx, SecretRow{ID: "a"}) },
		"SecretRows.Get":            func() error { return ignore(db.SecretRows().Get(ctx, "a")) },
		"SecretRows.Delete":         func() error { return db.SecretRows().Delete(ctx, "a") },
		"SecretRows.KeyIDs":         func() error { return ignore(db.SecretRows().KeyIDs(ctx)) },
		"SecretRows.Rewrite":        func() error { return db.SecretRows().Rewrite(ctx, func(*SecretRow) error { return nil }) },
		"KodiInstances.Create":      func() error { return db.KodiInstances().Create(ctx, newKodi("x")) },
		"KodiInstances.Update":      func() error { return db.KodiInstances().Update(ctx, k) },
		"KodiInstances.SetDefault":  func() error { return db.KodiInstances().SetDefault(ctx, 1) },
		"KodiInstances.Get":         func() error { return ignore(db.KodiInstances().Get(ctx, 1)) },
		"KodiInstances.Default":     func() error { return ignore(db.KodiInstances().Default(ctx)) },
		"KodiInstances.List":        func() error { return ignore(db.KodiInstances().List(ctx)) },
		"KodiInstances.Delete":      func() error { return db.KodiInstances().Delete(ctx, 1) },
		"Rooms.Create":              func() error { return db.Rooms().Create(ctx, &Room{Name: "Lounge"}) },
		"Rooms.Update":              func() error { return db.Rooms().Update(ctx, &Room{ID: 1, Name: "Lounge"}) },
		"Rooms.Get":                 func() error { return ignore(db.Rooms().Get(ctx, 1)) },
		"Rooms.List":                func() error { return ignore(db.Rooms().List(ctx)) },
		"Rooms.Resolve":             func() error { _, _, err := db.Rooms().Resolve(ctx, "lounge"); return err },
		"Rooms.Delete":              func() error { return db.Rooms().Delete(ctx, 1) },
		"Rooms.AddAlias":            func() error { return ignore(db.Rooms().AddAlias(ctx, 1, "front room", SpokenRoom)) },
		"Rooms.RemoveAlias":         func() error { return db.Rooms().RemoveAlias(ctx, 1) },
		"Rooms.Aliases":             func() error { return ignore(db.Rooms().Aliases(ctx, 1)) },
		"Aliases.Add":               func() error { return db.Aliases().Add(ctx, &TitleAlias{Alias: "x"}) },
		"Aliases.Delete":            func() error { return db.Aliases().Delete(ctx, 1) },
		"Aliases.List":              func() error { return ignore(db.Aliases().List(ctx, 1)) },
		"Aliases.ForMedia":          func() error { return ignore(db.Aliases().ForMedia(ctx, 1, media.Ref{Kind: media.KindMovie, ID: 1})) },
		"Aliases.Lookup":            func() error { return ignore(db.Aliases().Lookup(ctx, 1, "x")) },
		"EchoDevices.Seen":          func() error { return ignore(db.EchoDevices().Seen(ctx, "dev")) },
		"EchoDevices.Declare":       func() error { return ignore(db.EchoDevices().Declare(ctx, "dev")) },
		"EchoDevices.Update":        func() error { return db.EchoDevices().Update(ctx, &EchoDevice{ID: 1}) },
		"EchoDevices.Get":           func() error { return ignore(db.EchoDevices().Get(ctx, 1)) },
		"EchoDevices.GetByAlexaID":  func() error { return ignore(db.EchoDevices().GetByAlexaID(ctx, "dev")) },
		"EchoDevices.List":          func() error { return ignore(db.EchoDevices().List(ctx)) },
		"EchoDevices.Delete":        func() error { return db.EchoDevices().Delete(ctx, 1) },
		"LibraryGroups.Save":        func() error { return db.LibraryGroups().Save(ctx, &LibraryGroup{}, nil) },
		"LibraryGroups.Assign":      func() error { return db.LibraryGroups().Assign(ctx, 1, 1) },
		"LibraryGroups.Get":         func() error { return ignore(db.LibraryGroups().Get(ctx, 1)) },
		"LibraryGroups.List":        func() error { return ignore(db.LibraryGroups().List(ctx)) },
		"LibraryGroups.Members":     func() error { return ignore(db.LibraryGroups().Members(ctx, 1)) },
		"LibraryGroups.Delete":      func() error { return db.LibraryGroups().Delete(ctx, 1) },
		"LibraryGroups.DeleteEmpty": func() error { return ignore(db.LibraryGroups().DeleteEmpty(ctx)) },
		"RequestLog.Insert":         func() error { return db.RequestLog().Insert(ctx, &RequestLogEntry{}) },
		"RequestLog.Prune":          func() error { return db.RequestLog().Prune(ctx) },
		"RequestLog.List":           func() error { return ignore(db.RequestLog().List(ctx, RequestLogQuery{})) },
		"RequestLog.Get":            func() error { return ignore(db.RequestLog().Get(ctx, 1)) },
	} {
		err := call()
		require.Error(t, err, name)
		assert.NotErrorIs(t, err, ErrNotFound, "%s: a broken database is not an empty one", name)
	}
}
