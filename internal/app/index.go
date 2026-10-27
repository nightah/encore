package app

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// indexChanges runs on the catalog's goroutine.
func (g *group) indexChanges(_ string, upserted []media.Item, removed []media.Ref) {
	now := time.Now()

	g.aliasMu.RLock()
	docs := make([]match.Doc, 0, len(upserted))

	for idx := range upserted {
		if doc, ok := docFor(&upserted[idx], g.aliases[upserted[idx].Ref()], now); ok {
			docs = append(docs, doc)
		}
	}
	g.aliasMu.RUnlock()

	if err := g.index.Upsert(docs...); err != nil {
		g.log.Error("updating the title index", "error", err)
	}

	ids := make([]string, 0, len(removed))
	for _, ref := range removed {
		ids = append(ids, docID(ref))
	}

	if err := g.index.Delete(ids...); err != nil {
		g.log.Error("removing titles from the index", "error", err)
	}
}

// docID is the index ID for a library item. Indexes are per library group, whose members share IDs, so the instance isn't part of it. Alexa slot value IDs use the same form, so entity resolution results can be passed straight through as hints.
func docID(ref media.Ref) string { return fmt.Sprintf("%s/%d", ref.Kind, ref.ID) }

func docFor(item *media.Item, userAliases []string, now time.Time) (match.Doc, bool) {
	if item.Title == "" || item.Kind == media.KindEpisode {
		// Episodes are reached through their show.
		return match.Doc{}, false
	}

	doc := match.Doc{ID: docID(item.Ref()), Kind: item.Kind, Ref: item.Ref(), Title: item.Title, Year: item.Year}

	for _, alias := range append([]string{item.SortTitle, item.OriginalTitle}, userAliases...) {
		if alias != "" && alias != item.Title && !slices.Contains(doc.Aliases, alias) {
			doc.Aliases = append(doc.Aliases, alias)
		}
	}

	switch {
	case item.InProgress():
		doc.Boost = inProgressBoost
	case !item.DateAdded.IsZero() && now.Sub(item.DateAdded) < recentlyAddedFor:
		doc.Boost = recentlyAddedBoost
	}

	return doc, true
}

// loadAliases reads aliases saved against any member and returns the refs whose aliases changed.
func (a *App) loadAliases(ctx context.Context, g *group) ([]media.Ref, error) {
	next := map[media.Ref][]string{}

	for _, id := range g.memberIDs() {
		rows, err := a.db.Aliases().List(ctx, id)
		if err != nil {
			return nil, err
		}

		for _, r := range rows {
			next[r.Media] = append(next[r.Media], r.Alias)
		}
	}

	g.aliasMu.Lock()
	prev := g.aliases
	g.aliases = next
	g.aliasMu.Unlock()

	var changed []media.Ref

	for ref := range maps.Keys(next) {
		if !slices.Equal(prev[ref], next[ref]) {
			changed = append(changed, ref)
		}
	}

	for ref := range maps.Keys(prev) {
		if _, ok := next[ref]; !ok {
			changed = append(changed, ref)
		}
	}

	return changed, nil
}

// ReloadAliases makes the title indexes match the aliases saved in the database, after one was added or removed.
func (a *App) ReloadAliases(ctx context.Context) error {
	a.mu.RLock()
	groups := slices.Collect(maps.Values(a.groups))
	a.mu.RUnlock()

	for _, g := range groups {
		if err := a.refreshAliases(ctx, g); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) refreshAliases(ctx context.Context, g *group) error {
	changed, err := a.loadAliases(ctx, g)
	if err != nil {
		return err
	}

	var items []media.Item

	for _, ref := range changed {
		if it, ok := g.catalog.Get(ref); ok {
			items = append(items, it)
		}
	}

	if len(items) > 0 {
		g.indexChanges("", items, nil)
	}

	return nil
}
