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

// countingStore counts the snapshots written to the store it wraps.
type countingStore struct {
	blobstore.Store

	puts atomic.Int32
}

func (s *countingStore) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	s.puts.Add(1)

	return s.Store.Put(ctx, key, r, size)
}

func TestSnapshotsAreSavedAtMostOncePerInterval(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		srv := newKodi(t, fakekodi.Options{})

		cfg := config.Defaults()
		cfg.DataDir = t.TempDir()
		cfg.Kodi = []config.KodiInstance{kodiConfig("Lounge", srv, "")}

		// The production timings: the need is checked every 15 seconds, and met at most once a minute.
		tune := onFakes(defaultTuning())
		// Periodic deltas would count as changes too.
		tune.catalog.RefreshInterval = -1
		a := openApp(t, &cfg, tune)
		counted := &countingStore{Store: a.blobs}
		a.blobs = counted
		startApp(t, a)

		inst := a.instanceNamed(t, "Lounge")
		settled(t, func() bool { return !inst.Catalog.Stats().LastSync.IsZero() })
		assert.Zero(t, counted.puts.Load(), "nothing is saved before the first check")

		after(t, tune.snapshotCheck, func() bool { return counted.puts.Load() == 1 }, "the first sync is saved at the first check")

		// A library change right after the save.
		connected(t, srv)
		srv.Update(func(l *fakekodi.Library) {
			l.Movies = append(l.Movies, fakekodi.Movie{MovieID: 50, Title: "Paddington", Label: "Paddington", DateAdded: time.Now().Format(fakekodi.TimeLayout)})
		})
		srv.Notify("VideoLibrary.OnUpdate", map[string]any{"id": 50, "type": "movie", "added": true})
		after(t, time.Second, func() bool { return hasTitle(inst.Catalog, "Paddington") })

		// Checks at 30, 45 and 60 seconds find the change, but the last save was under a minute before.
		after(t, tune.snapshotEvery-tune.snapshotCheck, func() bool { return counted.puts.Load() == 1 }, "a change isn't saved within a minute of the last save")
		after(t, tune.snapshotCheck, func() bool { return counted.puts.Load() == 2 }, "it's saved at the first check a minute after the last save")

		// Nothing changes after that, so nothing more is written.
		after(t, 10*tune.snapshotEvery, func() bool { return counted.puts.Load() == 2 }, "an unchanged library isn't saved again")
	})
}

func TestTheLibraryIsSavedOnShutdown(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		srv := newKodi(t, fakekodi.Options{})
		dir := t.TempDir()

		// The periodic check never comes round, so only the save on the way out can write the snapshot.
		tune := fastTuning()
		tune.snapshotCheck = time.Hour

		a, stop := runApp(t, dir, tune, kodiConfig("Lounge", srv, ""))
		inst := a.instanceNamed(t, "Lounge")
		g, _ := a.groupOf("Lounge")
		file := filepath.Join(dir, "blobs", filepath.FromSlash(snapshotKey(g.ID)))

		settled(t, func() bool { return !inst.Catalog.Stats().LastSync.IsZero() })
		assert.NoFileExists(t, file)

		stop()
		assert.FileExists(t, file, "what changed since the last save isn't lost")
	})
}

func TestSnapshotsAreDisabledWhenTheStoreCantOpen(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		srv := newKodi(t, fakekodi.Options{})
		dir := t.TempDir()

		cfg := config.Defaults()
		cfg.DataDir = dir
		cfg.Kodi = []config.KodiInstance{kodiConfig("Lounge", srv, "")}
		cfg.Storage.Backend = "carrier-pigeon"

		a := openApp(t, &cfg, fastTuning())
		require.Nil(t, a.blobs)
		startApp(t, a)

		inst := a.instanceNamed(t, "Lounge")
		settled(t, func() bool { return inst.Index.Len() > 0 }, "the library works without snapshots")

		require.NoError(t, a.DB().KodiInstances().Delete(t.Context(), inst.id))
		require.NoError(t, a.RemoveInstance(t.Context(), inst.id))
		assert.NoDirExists(t, filepath.Join(dir, blobstore.LocalDirName))
	})
}

func TestAFailingSnapshotStoreIsNotFatal(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		srv := newKodi(t, fakekodi.Options{})

		cfg := config.Defaults()
		cfg.DataDir = t.TempDir()
		cfg.Kodi = []config.KodiInstance{kodiConfig("Lounge", srv, "")}

		a := openApp(t, &cfg, fastTuning())
		broken := &brokenStore{}
		a.blobs = broken
		startApp(t, a)

		inst := a.instanceNamed(t, "Lounge")
		settled(t, func() bool { return inst.Index.Len() > 0 }, "a snapshot that can't be read means a cold start")
		assert.Zero(t, broken.puts.Load(), "nothing is saved before the first check")
		after(t, fastTuning().snapshotCheck, func() bool { return broken.puts.Load() == 1 }, "saving is attempted at the next check")
		settled(t, func() bool { return broken.puts.Load() > 0 }, "saving is attempted")

		require.NoError(t, a.DB().KodiInstances().Delete(t.Context(), inst.id))
		require.NoError(t, a.RemoveInstance(t.Context(), inst.id), "a snapshot that can't be deleted doesn't fail the removal")
		assert.Positive(t, broken.deletes.Load())
	})
}
