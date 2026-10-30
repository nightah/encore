package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/core/library"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

// titleSlots maps library kinds to the slot types their titles fill (built-in types are extended), with the most titles of each kind the model takes. Together they keep the largest locale's model well under Amazon's 1.5 MB limit; titles left out are still found by the server-side matcher.
var titleSlots = []struct {
	kind  media.Kind
	slot  string
	limit int
}{
	{media.KindMovie, "AMAZON.Movie", 4000},
	{media.KindTVShow, "AMAZON.TVSeries", 2000},
	{media.KindArtist, "AMAZON.Musician", 2000},
	{media.KindAlbum, "AMAZON.MusicAlbum", 2000},
	{media.KindMusicVideo, "KODI_MUSIC_VIDEO", 1000},
}

// SlotValues builds the deploy-time slot values for the skill model: titles (including music videos) from every distinct library, counted once however many Kodi instances share it, with their aliases as synonyms; rooms with their aliases; and playlists. Each value's ID is the item's "<kind>/<id>", which entity resolution hands back as a matching hint.
func (a *App) SlotValues(ctx context.Context) (map[string][]skillmodel.SlotValue, error) {
	type lib struct {
		catalog *library.Catalog
		members []int64
	}

	a.mu.RLock()
	libs := make([]lib, 0, len(a.groups))

	for _, g := range a.groups {
		libs = append(libs, lib{catalog: g.catalog, members: g.memberIDs()})
	}
	a.mu.RUnlock()

	slices.SortFunc(libs, func(x, y lib) int { return cmp.Compare(x.members[0], y.members[0]) })

	out := map[string][]skillmodel.SlotValue{}

	for _, ts := range titleSlots {
		b := newSlotBuilder()

		for _, l := range libs {
			synonyms, err := a.titleAliases(ctx, l.members, ts.kind)
			if err != nil {
				return nil, err
			}

			for _, it := range l.catalog.Items(ts.kind) {
				b.add(&it, synonyms[it.ID])
			}
		}

		if vs := b.values(ts.limit); len(vs) > 0 {
			out[ts.slot] = vs
		}
	}

	b := newSlotBuilder()

	for _, l := range libs {
		for _, it := range l.catalog.Items(media.KindPlaylist) {
			b.add(&it, nil)
		}
	}

	if vs := b.values(0); len(vs) > 0 {
		out[playlistSlot] = vs
	}

	rooms, err := a.roomValues(ctx)
	if err != nil {
		return nil, err
	}

	if len(rooms) > 0 {
		out[RoomSlot] = rooms
	}

	return out, nil
}

// titleAliases reads every member, since aliases are saved against whichever member was the library's source at the time.
func (a *App) titleAliases(ctx context.Context, members []int64, kind media.Kind) (map[int][]string, error) {
	out := map[int][]string{}

	for _, id := range members {
		rows, err := a.db.Aliases().List(ctx, id)
		if err != nil {
			return nil, err
		}

		for _, r := range rows {
			if r.Media.Kind == kind {
				out[r.Media.ID] = append(out[r.Media.ID], r.Alias)
			}
		}
	}

	return out, nil
}

func (a *App) roomValues(ctx context.Context) ([]skillmodel.SlotValue, error) {
	rooms, err := a.db.Rooms().List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]skillmodel.SlotValue, 0, len(rooms))

	for _, r := range rooms {
		aliases, err := a.db.Rooms().Aliases(ctx, r.ID)
		if err != nil {
			return nil, err
		}

		v := skillmodel.SlotValue{ID: fmt.Sprintf("room/%d", r.ID), Value: r.Name}
		for _, al := range aliases {
			v.Synonyms = append(v.Synonyms, al.Alias)
		}

		out = append(out, v)
	}

	return out, nil
}

func newSlotBuilder() *slotBuilder {
	return &slotBuilder{byTitle: map[string]*slotCandidate{}, ids: map[string]string{}}
}

func (b *slotBuilder) add(it *media.Item, synonyms []string) {
	title := strings.TrimSpace(it.Title)
	if title == "" {
		return
	}

	key := strings.ToLower(title)
	recent := it.DateAdded

	if it.LastPlayed.After(recent) {
		recent = it.LastPlayed
	}

	c, ok := b.byTitle[key]
	if !ok {
		id := docID(it.Ref())

		// Libraries in separate databases can use one ID for different titles; such a title goes in without an ID rather than with another's.
		if other, taken := b.ids[id]; taken && other != key {
			id = ""
		} else {
			b.ids[id] = key
		}

		c = &slotCandidate{value: skillmodel.SlotValue{ID: id, Value: title}}
		b.byTitle[key] = c
	}

	if recent.After(c.recent) {
		c.recent = recent
	}

	for _, s := range synonyms {
		if !slices.ContainsFunc(c.value.Synonyms, func(x string) bool { return strings.EqualFold(x, s) }) && !strings.EqualFold(s, title) {
			c.value.Synonyms = append(c.value.Synonyms, s)
		}
	}
}

// values returns at most limit values (0 means all), keeping the most recently added or played when there are more, sorted by title. The order doesn't follow recency, so playing a title already in the model leaves the model, and the deployed package's hash, unchanged.
func (b *slotBuilder) values(limit int) []skillmodel.SlotValue {
	cands := make([]*slotCandidate, 0, len(b.byTitle))
	for _, c := range b.byTitle {
		cands = append(cands, c)
	}

	slices.SortFunc(cands, func(x, y *slotCandidate) int {
		return cmp.Or(y.recent.Compare(x.recent), cmp.Compare(x.value.Value, y.value.Value))
	})

	if limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}

	slices.SortFunc(cands, func(x, y *slotCandidate) int {
		return cmp.Or(cmp.Compare(strings.ToLower(x.value.Value), strings.ToLower(y.value.Value)), cmp.Compare(x.value.Value, y.value.Value))
	})

	out := make([]skillmodel.SlotValue, len(cands))
	for i, c := range cands {
		out[i] = c.value
	}

	return out
}
