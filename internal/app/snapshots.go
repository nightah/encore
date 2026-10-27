package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/core/library"
)

func snapshotKey(groupID int64) string { return fmt.Sprintf("library/%d.json.gz", groupID) }

// restoreSnapshot loads the group's last saved catalog, so voice requests work before the first sync finishes. Snapshots are only a cache: a missing, unreadable or mismatched one is skipped and the live sync that follows corrects whatever a restored one got wrong.
func (a *App) restoreSnapshot(ctx context.Context, g *group) {
	if a.blobs == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	var snap library.Snapshot

	err := blobstore.LoadJSON(ctx, a.blobs, snapshotKey(g.id), &snap)

	switch {
	case errors.Is(err, blobstore.ErrNotFound):
		g.log.Debug("no library snapshot to restore")

		return
	case err != nil:
		g.log.Warn("reading the library snapshot", "error", err)

		return
	case !g.snapshotFits(snap.Instance):
		g.log.Info("skipping a library snapshot taken from another Kodi instance", "snapshot_of", snap.Instance)

		return
	}

	snap.Instance = g.catalog.Instance()

	if err := g.catalog.Restore(snap); err != nil {
		g.log.Warn("restoring the library snapshot", "error", err)

		return
	}

	g.log.Info("restored the library snapshot", "items", len(snap.Items), "synced", snap.LastSync)
}

// keepSnapshots saves the group's catalog after syncs and events, at most once per snapshotEvery, and once more when ctx ends if anything changed since.
func (a *App) keepSnapshots(ctx context.Context, g *group) {
	if a.blobs == nil {
		<-ctx.Done()

		return
	}

	ticker := time.NewTicker(a.tune.snapshotCheck)
	defer ticker.Stop()

	// The catalog as restored or carried over needs no saving.
	saved := g.catalog.Stats()

	var lastSave time.Time

	changed := func() bool {
		s := g.catalog.Stats()

		return !s.LastSync.IsZero() && (!s.LastSync.Equal(saved.LastSync) || !s.LastDelta.Equal(saved.LastDelta) || !s.LastEvent.Equal(saved.LastEvent))
	}

	save := func(ctx context.Context) {
		stats := g.catalog.Stats()

		ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
		defer cancel()

		if err := blobstore.SaveJSON(ctx, a.blobs, snapshotKey(g.id), g.catalog.Snapshot()); err != nil {
			g.log.Warn("saving the library snapshot", "error", err)

			return
		}

		saved, lastSave = stats, time.Now()
		g.log.Debug("saved the library snapshot")
	}

	for {
		select {
		case <-ctx.Done():
			if changed() {
				save(context.WithoutCancel(ctx))
			}

			return
		case <-ticker.C:
			if changed() && time.Since(lastSave) >= a.tune.snapshotEvery {
				save(ctx)
			}
		}
	}
}

func (a *App) deleteSnapshot(ctx context.Context, groupID int64) {
	if a.blobs == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	if err := a.blobs.Delete(ctx, snapshotKey(groupID)); err != nil {
		a.log.Warn("deleting a library snapshot", "library_group", groupID, "error", err)
	}
}
