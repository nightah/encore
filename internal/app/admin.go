package app

import (
	"context"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/library"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/secrets"
)

// Secrets exposes the secret store to the admin API.
func (a *App) Secrets() *secrets.Store { return a.secrets }

// Config returns the configuration the app was started with.
func (a *App) Config() *config.Config { return a.cfg }

// InstanceIDs returns the IDs of the loaded Kodi instances, sorted.
func (a *App) InstanceIDs() []int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	ids := make([]int64, 0, len(a.instances))
	for id := range a.instances {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	return ids
}

func (a *App) runtime(id int64) (*Instance, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	inst, ok := a.instances[id]
	if !ok {
		return nil, ErrNoRuntime
	}

	return inst, nil
}

// runtimeLibrary reads under the lock because the group can change while the app runs.
func (a *App) runtimeLibrary(id int64) (*Instance, *library.Catalog, *match.Index, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	inst, ok := a.instances[id]
	if !ok {
		return nil, nil, nil, ErrNoRuntime
	}

	return inst, inst.Catalog, inst.Index, nil
}

// InstanceStatus reports an instance's connection, library and playback. It asks Kodi, each question bounded by a short timeout, whether it answers and what is playing; with info it also asks for the version and the episode total, which change rarely and cost more calls.
func (a *App) InstanceStatus(ctx context.Context, id int64, info bool) (InstanceStatus, error) {
	inst, cat, _, err := a.runtimeLibrary(id)
	if err != nil {
		return InstanceStatus{}, err
	}

	st := InstanceStatus{
		ID:        id,
		Name:      inst.name,
		Connected: inst.Client.Transport().Connected(),
		CanWake:   inst.Waker.MAC() != nil,
	}

	// A runtime may have no catalog yet while its library group starts.
	if cat != nil {
		st.fillLibrary(cat)
	}

	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	_, err = inst.Client.JSONRPCPing(pctx)

	cancel()

	st.Reachable = err == nil
	st.Unauthorized = errors.Is(err, kodi.ErrUnauthorized)

	if !st.Reachable {
		return st, nil
	}

	if info {
		ictx, cancel := context.WithTimeout(ctx, probeTimeout)
		if mi, err := inst.Backend.Info(ictx); err == nil {
			st.KodiVersion, st.APIVersion = mi.Version, mi.APIVersion
		}

		end := kodirpc.ListAmount(1)
		if r, err := inst.Client.VideoLibraryGetEpisodes(ictx, kodirpc.VideoLibraryGetEpisodesParams{Limits: &kodirpc.ListLimits{End: end}}); err == nil {
			st.Counts.Episodes = r.Limits.Total
		}

		cancel()
	}

	nctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	if np, err := inst.Backend.NowPlaying(nctx); err == nil {
		st.NowPlaying = np
	}

	return st, nil
}

// RefreshLibrary brings st's library counts, sync times and newest title up to date. They are read from memory, so a status cached to spare Kodi the questions doesn't have to hold them back.
func (a *App) RefreshLibrary(id int64, st *InstanceStatus) {
	_, cat, _, err := a.runtimeLibrary(id)
	if err != nil || cat == nil {
		return
	}

	// The episode total comes from Kodi when it was asked; the catalog only knows the episodes fetched so far.
	episodes := st.Counts.Episodes
	st.Newest = nil
	st.fillLibrary(cat)
	st.Counts.Episodes = max(st.Counts.Episodes, episodes)
}

func (st *InstanceStatus) fillLibrary(cat *library.Catalog) {
	stats := cat.Stats()
	st.LastEventAt = stats.LastEvent
	st.LastSyncAt = latest(stats.LastSync, stats.LastDelta)

	if !st.LastSyncAt.IsZero() {
		st.NextSyncAt = st.LastSyncAt.Add(library.DefaultRefreshInterval)
	}

	st.Counts = LibraryCounts{
		Movies:   stats.Counts[media.KindMovie],
		Shows:    stats.Counts[media.KindTVShow],
		Episodes: stats.Episodes,
		Songs:    stats.Counts[media.KindSong],
	}

	if newest := cat.RecentlyAdded(1, media.KindMovie, media.KindEpisode); len(newest) > 0 {
		st.Newest = &newest[0]
	}
}

// InstanceImage fetches artwork, an image reference from the instance's items, from that Kodi. The caller closes the body.
func (a *App) InstanceImage(ctx context.Context, id int64, ref string) (io.ReadCloser, string, error) {
	inst, err := a.runtime(id)
	if err != nil {
		return nil, "", err
	}

	return inst.Client.Image(ctx, ref)
}

// RecentlyAdded returns the n newest movies, shows, episodes and songs in an instance's catalog, newest first.
func (a *App) RecentlyAdded(id int64, n int) ([]media.Item, error) {
	_, cat, _, err := a.runtimeLibrary(id)
	if err != nil {
		return nil, err
	}

	if cat == nil {
		return nil, nil
	}

	return cat.RecentlyAdded(n, media.KindMovie, media.KindTVShow, media.KindEpisode, media.KindSong), nil
}

// Findable reports whether an item is in an instance's title index, so a voice request can find it.
func (a *App) Findable(id int64, ref media.Ref) bool {
	_, cat, _, err := a.runtimeLibrary(id)
	if err != nil || cat == nil {
		return false
	}

	if ref.Kind == media.KindEpisode {
		// Episodes are found through their show.
		it, ok := cat.Get(ref)
		if !ok {
			return false
		}

		ref = media.Ref{Kind: media.KindTVShow, ID: it.ShowID}
	}

	_, ok := cat.Get(ref)

	return ok
}

// Item returns one item from an instance's catalog.
func (a *App) Item(id int64, ref media.Ref) (media.Item, bool) {
	_, cat, _, err := a.runtimeLibrary(id)
	if err != nil || cat == nil {
		return media.Item{}, false
	}

	return cat.Get(ref)
}

// Search runs a query against an instance's title index, as the match tester does.
func (a *App) Search(ctx context.Context, id int64, q match.Query) (match.Result, error) {
	_, _, index, err := a.runtimeLibrary(id)
	if err != nil {
		return match.Result{}, err
	}

	if index == nil {
		return match.Result{}, ErrNoRuntime
	}

	return index.Search(ctx, q)
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}
