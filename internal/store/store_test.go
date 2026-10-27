package store

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	assert.Error(t, db.BackupTo(t.Context(), path))
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
