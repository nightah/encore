package library

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// Snapshot captures the catalog.
func (c *Catalog) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	s := Snapshot{
		Instance:   c.instance,
		Items:      make([]media.Item, 0, len(c.items)),
		Episodes:   maps.Clone(c.episodes),
		Watermarks: maps.Clone(c.watermark),
		LastSync:   c.lastSync,
	}

	for _, kind := range c.kinds {
		s.Items = append(s.Items, c.byKind[kind]...)
	}

	return s
}

// Restore replaces the catalog's contents with s, skipping kinds it doesn't load, and reports the difference to change hooks. Follow it with Delta, or let Run do so, to catch up with what changed since.
func (c *Catalog) Restore(s Snapshot) error {
	if s.Instance != c.instance {
		return fmt.Errorf("library: snapshot is for instance %q, not %q", s.Instance, c.instance)
	}

	c.write.Lock()
	defer c.write.Unlock()

	// The kinds loaded may have changed since the snapshot, and no sync would remove titles of a kind it doesn't load.
	kinds := c.Kinds()
	items := slices.DeleteFunc(slices.Clone(s.Items), func(it media.Item) bool { return !slices.Contains(kinds, it.Kind) })

	keep := make(map[media.Ref]bool, len(items))
	for i := range items {
		keep[items[i].Ref()] = true
	}

	var removed []media.Ref

	c.mu.RLock()
	for ref := range c.items {
		if !keep[ref] {
			removed = append(removed, ref)
		}
	}
	c.mu.RUnlock()

	c.apply(items, removed)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.episodes, c.epIndex = map[int][]media.Item{}, map[int]media.Item{}
	c.epGen++

	if !slices.Contains(kinds, media.KindTVShow) {
		s.Episodes = nil
	}

	for show, eps := range s.Episodes {
		c.episodes[show] = eps
		for i := range eps {
			c.epIndex[eps[i].ID] = eps[i]
		}
	}

	c.watermark = maps.Clone(s.Watermarks)
	if c.watermark == nil {
		c.watermark = map[media.Kind]time.Time{}
	}

	c.lastSync = s.LastSync

	return nil
}
