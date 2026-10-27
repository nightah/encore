package library

import (
	"cmp"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// DefaultKinds are the kinds a catalog loads; episodes are cached per show on demand.
var DefaultKinds = []media.Kind{
	media.KindMovie, media.KindTVShow, media.KindMusicVideo,
	media.KindArtist, media.KindAlbum, media.KindSong, media.KindPlaylist,
}

// New returns an empty catalog for the named instance.
func New(instance string, backend media.Source, opts Options) *Catalog {
	if len(opts.Kinds) == 0 {
		opts.Kinds = DefaultKinds
	}

	if opts.RefreshInterval == 0 {
		opts.RefreshInterval = DefaultRefreshInterval
	}

	if opts.Debounce <= 0 {
		opts.Debounce = DefaultDebounce
	}

	if opts.MinResync == 0 {
		opts.MinResync = DefaultMinResync
	}

	if opts.Now == nil {
		opts.Now = time.Now
	}

	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Catalog{
		instance:  instance,
		backend:   backend,
		opts:      opts,
		log:       log,
		kinds:     slices.Clone(opts.Kinds),
		needFull:  map[media.Kind]bool{},
		items:     map[media.Ref]media.Item{},
		byKind:    map[media.Kind][]media.Item{},
		episodes:  map[int][]media.Item{},
		epIndex:   map[int]media.Item{},
		watermark: map[media.Kind]time.Time{},
		hooks:     map[int]ChangeFunc{},
	}
}

// Kinds returns the kinds the catalog loads.
func (c *Catalog) Kinds() []media.Kind {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return slices.Clone(c.kinds)
}

// SetKinds changes the kinds the catalog loads. Titles of a dropped kind leave the catalog straight away, reported to change hooks; an added kind is listed in full by the next Sync or Delta.
func (c *Catalog) SetKinds(kinds []media.Kind) {
	c.write.Lock()
	defer c.write.Unlock()

	var removed []media.Ref

	c.mu.Lock()
	for _, k := range kinds {
		if !slices.Contains(c.kinds, k) {
			c.needFull[k] = true
		}
	}

	for _, k := range c.kinds {
		if slices.Contains(kinds, k) {
			continue
		}

		delete(c.needFull, k)

		for i := range c.byKind[k] {
			removed = append(removed, media.Ref{Kind: k, ID: c.byKind[k][i].ID})
		}
	}

	c.kinds = slices.Clone(kinds)
	c.mu.Unlock()

	if len(removed) > 0 {
		c.apply(nil, removed)
	}
}

// Instance is the backend instance name.
func (c *Catalog) Instance() string { return c.instance }

// OnChange registers fn and returns a func that unregisters it. Hooks run synchronously after each change, in order; they may read the catalog but must not call Sync, Delta, Lookup or Restore.
func (c *Catalog) OnChange(fn ChangeFunc) (remove func()) {
	c.hooksMu.Lock()
	defer c.hooksMu.Unlock()

	c.nextHook++
	id := c.nextHook
	c.hooks[id] = fn

	return func() {
		c.hooksMu.Lock()
		defer c.hooksMu.Unlock()

		delete(c.hooks, id)
	}
}

func (c *Catalog) emit(upserted []media.Item, removed []media.Ref) {
	if len(upserted) == 0 && len(removed) == 0 {
		return
	}

	c.hooksMu.Lock()
	hooks := slices.Collect(maps.Values(c.hooks))
	c.hooksMu.Unlock()

	for _, h := range hooks {
		h(c.instance, upserted, removed)
	}
}

// Items returns every item of kind ordered by ID. The slice is shared and must not be modified.
func (c *Catalog) Items(kind media.Kind) []media.Item {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.byKind[kind]
}

// Get returns one item, including cached episodes.
func (c *Catalog) Get(ref media.Ref) (media.Item, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if ref.Kind == media.KindEpisode {
		it, ok := c.epIndex[ref.ID]

		return it, ok
	}

	it, ok := c.items[ref]

	return it, ok
}

// Stats reports item counts and sync times.
func (c *Catalog) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	s := Stats{
		Counts:    map[media.Kind]int{},
		Episodes:  len(c.epIndex),
		LastSync:  c.lastSync,
		LastDelta: c.lastDelta,
		LastEvent: c.lastEvent,
	}

	for k, items := range c.byKind {
		s.Counts[k] = len(items)
	}

	return s
}

// RecentlyAdded returns the n newest items of the given kinds (all, including cached episodes, when none are given), newest first.
func (c *Catalog) RecentlyAdded(n int, kinds ...media.Kind) []media.Item {
	c.mu.RLock()

	var out []media.Item

	for ref := range c.items {
		if len(kinds) == 0 || slices.Contains(kinds, ref.Kind) {
			out = append(out, c.items[ref])
		}
	}

	if len(kinds) == 0 || slices.Contains(kinds, media.KindEpisode) {
		out = slices.AppendSeq(out, maps.Values(c.epIndex))
	}

	c.mu.RUnlock()

	slices.SortFunc(out, func(a, b media.Item) int {
		return cmp.Or(b.DateAdded.Compare(a.DateAdded), strings.Compare(string(a.Kind), string(b.Kind)), cmp.Compare(a.ID, b.ID))
	})

	return out[:min(n, len(out))]
}

// apply upserts and removes items, then tells the hooks what actually changed. Callers hold c.write.
func (c *Catalog) apply(upserted []media.Item, removed []media.Ref) {
	var (
		changed []media.Item
		gone    []media.Ref
		dirty   = map[media.Kind]bool{}
		now     = c.opts.Now()
	)

	c.mu.Lock()

	for i := range upserted {
		it := &upserted[i]
		if it.Kind == media.KindEpisode {
			continue
		}

		if old, ok := c.items[it.Ref()]; ok && equal(&old, it) {
			continue
		}

		c.items[it.Ref()] = *it
		changed = append(changed, *it)
		dirty[it.Kind] = true
		c.raiseWatermark(it, now)
	}

	for _, ref := range removed {
		if _, ok := c.items[ref]; ok {
			delete(c.items, ref)
			gone = append(gone, ref)
			dirty[ref.Kind] = true
		}
	}

	for kind := range dirty {
		c.rebuild(kind)
	}

	c.mu.Unlock()

	c.emit(changed, gone)
}

// Callers hold c.mu.
func (c *Catalog) rebuild(kind media.Kind) {
	var items []media.Item

	for ref := range c.items {
		if ref.Kind == kind {
			items = append(items, c.items[ref])
		}
	}

	slices.SortFunc(items, func(a, b media.Item) int { return cmp.Compare(a.ID, b.ID) })
	c.byKind[kind] = items
}

// raiseWatermark tracks the newest date added per kind, capped at now so one bogus future date can't hide later additions. Callers hold c.mu.
func (c *Catalog) raiseWatermark(it *media.Item, now time.Time) {
	added := it.DateAdded
	if added.After(now) {
		added = now
	}

	if added.After(c.watermark[it.Kind]) {
		c.watermark[it.Kind] = added
	}
}

func equal(a, b *media.Item) bool {
	return a.Kind == b.Kind && a.ID == b.ID && a.Title == b.Title && a.SortTitle == b.SortTitle &&
		a.OriginalTitle == b.OriginalTitle && a.Year == b.Year && slices.Equal(a.Genres, b.Genres) &&
		a.ShowID == b.ShowID && a.ShowTitle == b.ShowTitle && a.Season == b.Season && a.Episode == b.Episode &&
		a.Artist == b.Artist && a.Album == b.Album && a.PlayCount == b.PlayCount && a.Resume == b.Resume &&
		a.Runtime == b.Runtime && a.DateAdded.Equal(b.DateAdded) && a.LastPlayed.Equal(b.LastPlayed)
}
