package app

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
)

// brokenStore fails every call, as an unreachable remote store does.
type brokenStore struct{ puts, gets, deletes atomic.Int32 }

var errBroken = errors.New("store unreachable")

func (s *brokenStore) Put(context.Context, string, io.Reader, int64) error {
	s.puts.Add(1)

	return errBroken
}

func (s *brokenStore) Get(context.Context, string) (io.ReadCloser, error) {
	s.gets.Add(1)

	return nil, errBroken
}

func (s *brokenStore) Delete(context.Context, string) error {
	s.deletes.Add(1)

	return errBroken
}

func (*brokenStore) List(context.Context, string) ([]blobstore.Object, error) { return nil, errBroken }

func (*brokenStore) Probe(context.Context) error { return errBroken }

func TestTheLibraryIsSavedOnShutdown(t *testing.T) {
	srv := fakekodi.New(t, fakekodi.Options{})
	dir := t.TempDir()

	// The periodic check never comes round, so only the save on the way out can write the snapshot.
	tune := fastTuning()
	tune.snapshotCheck = time.Hour

	a, stop := runApp(t, dir, tune, kodiConfig("Lounge", srv, ""))
	inst := a.instanceNamed(t, "Lounge")
	g, _ := a.groupOf("Lounge")
	file := filepath.Join(dir, "blobs", filepath.FromSlash(snapshotKey(g.ID)))

	require.Eventually(t, func() bool { return !inst.Catalog.Stats().LastSync.IsZero() }, eventually, 10*time.Millisecond)
	assert.NoFileExists(t, file)

	stop()
	assert.FileExists(t, file, "what changed since the last save isn't lost")
}

func TestSnapshotsAreDisabledWhenTheStoreCantOpen(t *testing.T) {
	srv := fakekodi.New(t, fakekodi.Options{})
	dir := t.TempDir()

	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Kodi = []config.KodiInstance{kodiConfig("Lounge", srv, "")}
	cfg.Storage.Backend = "carrier-pigeon"

	a := openApp(t, &cfg, fastTuning())
	require.Nil(t, a.blobs)
	startApp(t, a)

	inst := a.instanceNamed(t, "Lounge")
	require.Eventually(t, func() bool { return inst.Index.Len() > 0 }, eventually, 10*time.Millisecond, "the library works without snapshots")

	require.NoError(t, a.DB().KodiInstances().Delete(t.Context(), inst.id))
	require.NoError(t, a.RemoveInstance(t.Context(), inst.id))
	assert.NoDirExists(t, filepath.Join(dir, blobstore.LocalDirName))
}

func TestAFailingSnapshotStoreIsNotFatal(t *testing.T) {
	srv := fakekodi.New(t, fakekodi.Options{})

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Kodi = []config.KodiInstance{kodiConfig("Lounge", srv, "")}

	a := openApp(t, &cfg, fastTuning())
	broken := &brokenStore{}
	a.blobs = broken
	startApp(t, a)

	inst := a.instanceNamed(t, "Lounge")
	require.Eventually(t, func() bool { return inst.Index.Len() > 0 }, eventually, 10*time.Millisecond, "a snapshot that can't be read means a cold start")
	assert.Positive(t, broken.gets.Load())
	require.Eventually(t, func() bool { return broken.puts.Load() > 0 }, eventually, 10*time.Millisecond, "saving is attempted")

	require.NoError(t, a.DB().KodiInstances().Delete(t.Context(), inst.id))
	require.NoError(t, a.RemoveInstance(t.Context(), inst.id), "a snapshot that can't be deleted doesn't fail the removal")
	assert.Positive(t, broken.deletes.Load())
}
