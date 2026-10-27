package library

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/core/media"
)

// Sync loads every configured kind in full, replacing what the catalog holds. A kind that fails to load keeps its previous items; the errors are joined.
func (c *Catalog) Sync(ctx context.Context) error {
	c.write.Lock()
	defer c.write.Unlock()

	return c.syncLocked(ctx)
}

func (c *Catalog) syncLocked(ctx context.Context) error {
	start := c.opts.Now()

	var (
		upserted []media.Item
		removed  []media.Ref
		errs     []error
	)

	kinds := c.Kinds()

	for _, kind := range kinds {
		items, err := c.backend.Library(ctx, kind)
		if err != nil {
			// Stopping, not failing: say so once rather than for every kind.
			if ctx.Err() != nil {
				return ctx.Err()
			}

			errs = append(errs, err)

			continue
		}

		upserted = append(upserted, items...)
		removed = append(removed, c.missing(kind, items)...)

		c.mu.Lock()
		delete(c.needFull, kind)
		c.mu.Unlock()
	}

	c.apply(upserted, removed)

	c.mu.Lock()
	// Episodes are refetched on demand; a full sync may have changed any of them.
	c.episodes, c.epIndex = map[int][]media.Item{}, map[int]media.Item{}
	c.epGen++

	if c.watermark[media.KindEpisode].IsZero() {
		c.watermark[media.KindEpisode] = start.Add(-episodeLookback)
	}

	if len(errs) < len(kinds) {
		c.lastSync = start
	}
	c.mu.Unlock()

	c.log.Debug("library synced", "items", len(upserted), "removed", len(removed), "took", c.opts.Now().Sub(start), "errors", len(errs))

	return joinErrors(errs)
}

// missing lists items of kind the catalog holds but fresh does not.
func (c *Catalog) missing(kind media.Kind, fresh []media.Item) []media.Ref {
	keep := make(map[int]bool, len(fresh))
	for i := range fresh {
		keep[fresh[i].ID] = true
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []media.Ref

	for i := range c.byKind[kind] {
		if id := c.byKind[kind][i].ID; !keep[id] {
			out = append(out, media.Ref{Kind: kind, ID: id})
		}
	}

	return out
}

// Delta fetches items added since the newest one the catalog has seen, per kind, and new episodes of cached shows. Playlists are cheap and re-listed in full. Before the first sync it does a full Sync.
func (c *Catalog) Delta(ctx context.Context) error {
	c.write.Lock()
	defer c.write.Unlock()

	c.mu.RLock()
	synced := !c.lastSync.IsZero()
	c.mu.RUnlock()

	if !synced {
		return c.syncLocked(ctx)
	}

	return c.deltaLocked(ctx)
}

func (c *Catalog) deltaLocked(ctx context.Context) error {
	var (
		upserted []media.Item
		removed  []media.Ref
		errs     []error
	)

	for _, kind := range append(c.Kinds(), media.KindEpisode) {
		c.mu.RLock()
		full := c.needFull[kind]
		c.mu.RUnlock()

		// Playlists have no date added, and a kind added by SetKinds has never been listed; both are listed in full.
		if kind == media.KindPlaylist || full {
			items, err := c.backend.Library(ctx, kind)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				errs = append(errs, err)

				continue
			}

			upserted = append(upserted, items...)
			removed = append(removed, c.missing(kind, items)...)

			c.mu.Lock()
			delete(c.needFull, kind)
			c.mu.Unlock()

			continue
		}

		c.mu.RLock()
		since := c.watermark[kind].Add(-deltaSlack)
		c.mu.RUnlock()

		items, err := c.backend.RecentlyAdded(ctx, kind, since)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			errs = append(errs, err)

			continue
		}

		if kind == media.KindEpisode {
			c.mergeEpisodes(items)

			continue
		}

		upserted = append(upserted, items...)
	}

	c.apply(upserted, removed)

	c.mu.Lock()
	c.lastDelta = c.opts.Now()
	c.mu.Unlock()

	return joinErrors(errs)
}

// mergeEpisodes folds fresh episodes into the cache of shows already cached and advances the episode watermark.
func (c *Catalog) mergeEpisodes(fresh []media.Item) {
	now := c.opts.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range fresh {
		ep := &fresh[i]
		c.raiseWatermark(ep, now)

		cached, ok := c.episodes[ep.ShowID]
		if !ok {
			continue
		}

		next := slices.Clone(cached)
		if j := slices.IndexFunc(next, func(e media.Item) bool { return e.ID == ep.ID }); j >= 0 {
			next[j] = *ep
		} else {
			next = append(next, *ep)
			sortEpisodes(next)
		}

		c.episodes[ep.ShowID] = next
		c.epIndex[ep.ID] = *ep
	}
}

func (c *Catalog) dropEpisode(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ep, ok := c.epIndex[id]
	if !ok {
		return
	}

	delete(c.epIndex, id)
	c.episodes[ep.ShowID] = slices.DeleteFunc(slices.Clone(c.episodes[ep.ShowID]), func(e media.Item) bool { return e.ID == id })
}

func sortEpisodes(eps []media.Item) {
	slices.SortStableFunc(eps, func(a, b media.Item) int {
		if a.Season != b.Season {
			return a.Season - b.Season
		}

		return a.Episode - b.Episode
	})
}

// Episodes returns a show's episodes in season and episode order, fetching them on first use. The slice is shared and must not be modified.
func (c *Catalog) Episodes(ctx context.Context, showID int) ([]media.Item, error) {
	c.mu.RLock()
	eps, ok := c.episodes[showID]
	gen := c.epGen
	c.mu.RUnlock()

	if ok {
		return eps, nil
	}

	eps, err := c.backend.Episodes(ctx, showID)
	if err != nil {
		return nil, err
	}

	now := c.opts.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	// A full sync while fetching invalidated the cache; return the episodes without caching possibly stale data.
	if gen != c.epGen {
		return eps, nil
	}

	c.episodes[showID] = eps

	for i := range eps {
		c.epIndex[eps[i].ID] = eps[i]
		c.raiseWatermark(&eps[i], now)
	}

	return eps, nil
}

// Refresh re-fetches one item, dropping it when the backend no longer has it.
func (c *Catalog) Refresh(ctx context.Context, ref media.Ref) error {
	c.write.Lock()
	defer c.write.Unlock()

	return c.refreshLocked(ctx, ref)
}

func (c *Catalog) refreshLocked(ctx context.Context, ref media.Ref) error {
	it, err := c.backend.Item(ctx, ref)

	switch {
	case errors.Is(err, media.ErrNotFound):
		c.removeLocked(ref)

		return nil
	case err != nil:
		return fmt.Errorf("refreshing %s %d: %w", ref.Kind, ref.ID, err)
	case ref.Kind == media.KindEpisode:
		c.mergeEpisodes([]media.Item{it})
	default:
		c.apply([]media.Item{it}, nil)
	}

	return nil
}

func (c *Catalog) removeLocked(ref media.Ref) {
	if ref.Kind == media.KindEpisode {
		c.dropEpisode(ref.ID)

		return
	}

	c.apply(nil, []media.Ref{ref})
}

// Lookup asks the backend directly for items of kind whose title contains title, adds them to the catalog and returns them. Use it when a title isn't found, before telling the user so.
func (c *Catalog) Lookup(ctx context.Context, kind media.Kind, title string) ([]media.Item, error) {
	if kind == media.KindEpisode {
		return nil, errors.New("library: episodes are looked up through their show")
	}

	var (
		items []media.Item
		err   error
	)

	if s, ok := c.backend.(Searcher); ok {
		items, err = s.Search(ctx, kind, title)
	} else {
		items, err = c.backend.Library(ctx, kind)
		items = containing(items, title)
	}

	if err != nil {
		return nil, fmt.Errorf("looking up %s “%s”: %w", kind, title, err)
	}

	c.write.Lock()
	c.apply(items, nil)
	c.write.Unlock()

	return items, nil
}

func containing(items []media.Item, title string) []media.Item {
	title = strings.ToLower(title)

	return slices.DeleteFunc(items, func(it media.Item) bool { return !strings.Contains(strings.ToLower(it.Title), title) })
}

// joinErrors joins errs on one line, "a; b", which reads better in a log line than errors.Join's newlines; errors.Is and errors.As still see each of them. The backend's errors already say which kind failed.
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}

	return errorList(errs)
}

func (e errorList) Error() string {
	parts := make([]string, len(e))
	for i, err := range e {
		parts[i] = err.Error()
	}

	return strings.Join(parts, "; ")
}

func (e errorList) Unwrap() []error { return e }
