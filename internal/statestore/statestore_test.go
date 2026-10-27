package statestore

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
)

func newBlobStore(t *testing.T) blobstore.Store {
	t.Helper()

	s, err := blobstore.Open(config.Storage{Backend: config.BackendLocal, Local: config.StorageLocal{Path: t.TempDir()}}, t.TempDir())
	require.NoError(t, err)

	return s
}

func openDB(t *testing.T, dir string) *store.DB {
	t.Helper()

	db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(dir), Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestRestoreWithNothingSaved(t *testing.T) {
	ok, err := Restore(t.Context(), newBlobStore(t), filepath.Join(t.TempDir(), "encore.db"))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSaveAndRestoreRoundTrip(t *testing.T) {
	bs := newBlobStore(t)

	// First container: create state and save it.
	first := openDB(t, t.TempDir())
	require.NoError(t, first.Settings().Set(t.Context(), "skill.id", "amzn1.ask.skill.test"))

	saver := &Saver{DB: first, Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "first"}

	uploaded, err := saver.Save(t.Context())
	require.NoError(t, err)
	assert.True(t, uploaded)

	uploaded, err = saver.Save(t.Context())
	require.NoError(t, err)
	assert.False(t, uploaded, "an unchanged database isn't uploaded again")

	// A change is saved, and the earlier copy is kept as a fallback.
	require.NoError(t, first.Settings().Set(t.Context(), "skill.invocation", "encore"))

	uploaded, err = saver.Save(t.Context())
	require.NoError(t, err)
	assert.True(t, uploaded)

	_, err = bs.Get(t.Context(), KeyPrevious)
	require.NoError(t, err)

	// Second container, empty disk: restore and read the state back.
	dir := t.TempDir()
	ok, err := Restore(t.Context(), bs, store.DefaultPath(dir))
	require.NoError(t, err)
	require.True(t, ok)

	info, err := os.Stat(store.DefaultPath(dir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	second := openDB(t, dir)

	var invocation string
	require.NoError(t, second.Settings().Get(t.Context(), "skill.invocation", &invocation))
	assert.Equal(t, "encore", invocation)
}

func TestRunSavesOnShutdown(t *testing.T) {
	bs := newBlobStore(t)
	db := openDB(t, t.TempDir())
	saver := &Saver{DB: db, Store: bs, Log: slog.New(slog.DiscardHandler), Interval: time.Hour, Owner: "me"}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		saver.Run(ctx)
		close(done)
	}()

	require.NoError(t, db.Settings().Set(t.Context(), "k", "v"))
	cancel()
	<-done

	r, err := bs.Get(t.Context(), KeyDatabase)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	_, err = bs.Get(t.Context(), KeyOwner)
	require.ErrorIs(t, err, blobstore.ErrNotFound, "a clean shutdown releases the owner marker")
}

func TestReleaseKeepsAnotherOwnersMarker(t *testing.T) {
	bs := newBlobStore(t)

	other := &Saver{Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "other"}
	other.heartbeat(t.Context())

	me := &Saver{Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "me"}
	me.release(t.Context())

	_, _, active, err := ActiveOwner(t.Context(), bs, time.Now())
	require.NoError(t, err)
	assert.True(t, active)
}

func TestActiveOwner(t *testing.T) {
	bs := newBlobStore(t)
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

	_, _, active, err := ActiveOwner(t.Context(), bs, now)
	require.NoError(t, err)
	assert.False(t, active, "no marker")

	s := &Saver{Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "pod-1", Now: func() time.Time { return now }}
	s.heartbeat(t.Context())

	name, seen, active, err := ActiveOwner(t.Context(), bs, now.Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, "pod-1", name)
	assert.True(t, seen.Equal(now))

	_, _, active, err = ActiveOwner(t.Context(), bs, now.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, active, "a stale heartbeat")
}

func TestUploadKeepsPrevious(t *testing.T) {
	bs := newBlobStore(t)

	first := openDB(t, t.TempDir())
	require.NoError(t, first.Settings().Set(t.Context(), "marker", "saved"))

	_, err := (&Saver{DB: first, Store: bs, Log: slog.New(slog.DiscardHandler)}).Save(t.Context())
	require.NoError(t, err)

	replacement := filepath.Join(t.TempDir(), "restored.db")
	second := openDB(t, t.TempDir())
	require.NoError(t, second.Settings().Set(t.Context(), "marker", "uploaded"))
	require.NoError(t, second.BackupTo(t.Context(), replacement))

	require.NoError(t, Upload(t.Context(), bs, replacement))

	read := func(key string) string {
		r, err := bs.Get(t.Context(), key)
		require.NoError(t, err)

		zr, err := gzip.NewReader(r)
		require.NoError(t, err)

		raw, err := io.ReadAll(zr)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(store.DefaultPath(dir), raw, 0o600))

		var v string
		require.NoError(t, openDB(t, dir).Settings().Get(t.Context(), "marker", &v))

		return v
	}

	assert.Equal(t, "uploaded", read(KeyDatabase))
	assert.Equal(t, "saved", read(KeyPrevious))
}

func TestOwnerWarning(t *testing.T) {
	bs := newBlobStore(t)
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

	other := &Saver{Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "other", Now: func() time.Time { return now }}
	other.heartbeat(t.Context())

	var logs bytes.Buffer

	me := &Saver{Store: bs, Log: slog.New(slog.NewTextHandler(&logs, nil)), Owner: "me", Now: func() time.Time { return now.Add(time.Minute) }}
	me.checkOwner(t.Context())
	assert.Contains(t, logs.String(), "another Encore seems to be using this storage")

	logs.Reset()

	later := &Saver{Store: bs, Log: slog.New(slog.NewTextHandler(&logs, nil)), Owner: "later", Now: func() time.Time { return now.Add(time.Hour) }}
	later.checkOwner(t.Context())
	assert.NotContains(t, logs.String(), "another encore", "a stale owner is ignored")
}

type failingDB struct{}

func (failingDB) Backup(context.Context, io.Writer) error { return io.ErrUnexpectedEOF }

func TestSaveReportsBackupErrors(t *testing.T) {
	_, err := (&Saver{DB: failingDB{}, Store: newBlobStore(t), Log: slog.New(slog.DiscardHandler)}).Save(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copying database")
}
