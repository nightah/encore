package app

import (
	"maps"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/store"
)

func slotValue(t *testing.T, values []skillmodel.SlotValue, title string) skillmodel.SlotValue {
	t.Helper()

	var found []skillmodel.SlotValue

	for _, v := range values {
		if v.Value == title {
			found = append(found, v)
		}
	}

	require.Len(t, found, 1, "%q should be in the slot values exactly once", title)

	return found[0]
}

func TestSlotValuesCoverTitlesPlaylistsAndRooms(t *testing.T) {
	h := newHarness(t, true)
	h.waitIndexed(t)
	ctx := t.Context()
	db := h.app.DB()
	id := h.instance(t).id

	require.NoError(t, db.Aliases().Add(ctx, &store.TitleAlias{KodiInstanceID: id, Media: media.Ref{Kind: media.KindMovie, ID: 6}, Alias: "the cornetto one"}))
	require.NoError(t, db.Aliases().Add(ctx, &store.TitleAlias{KodiInstanceID: id, Media: media.Ref{Kind: media.KindTVShow, ID: 1}, Alias: "lumon"}))

	lounge := store.Room{Name: "Lounge", KodiInstanceID: id}
	require.NoError(t, db.Rooms().Create(ctx, &lounge))
	_, err := db.Rooms().AddAlias(ctx, lounge.ID, "living room")
	require.NoError(t, err)

	out, err := h.app.SlotValues(ctx)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"AMAZON.Movie", "AMAZON.TVSeries", "AMAZON.Musician", "AMAZON.MusicAlbum", "KODI_MUSIC_VIDEO", "KODI_PLAYLIST", "KODI_ROOM"}, slices.Collect(maps.Keys(out)))

	movies := out["AMAZON.Movie"]
	assert.Len(t, movies, 6, "the two films called Dune are one spoken value")
	assert.Equal(t, skillmodel.SlotValue{ID: "movie/6", Value: "Hot Fuzz", Synonyms: []string{"the cornetto one"}}, slotValue(t, movies, "Hot Fuzz"))
	assert.Equal(t, []string{"lumon"}, slotValue(t, out["AMAZON.TVSeries"], "Severance").Synonyms, "an alias of another kind stays with its kind")
	assert.Empty(t, slotValue(t, movies, "Dune").Synonyms, "the show's alias doesn't reach the movie with its ID")

	assert.Equal(t, "musicvideo/1", slotValue(t, out["KODI_MUSIC_VIDEO"], "Bohemian Rhapsody").ID)
	slotValue(t, out["AMAZON.Musician"], "Queen")
	slotValue(t, out["AMAZON.MusicAlbum"], "Abbey Road")
	slotValue(t, out["KODI_PLAYLIST"], "Road Trip")

	assert.Equal(t, []skillmodel.SlotValue{{ID: "room/" + strconv.FormatInt(lounge.ID, 10), Value: "Lounge", Synonyms: []string{"living room"}}}, out["KODI_ROOM"])
}

func TestSlotValuesCountASharedLibraryOnce(t *testing.T) {
	lib := fakekodi.DefaultLibrary()
	lounge, bedroom := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})
	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, "upstairs"), kodiConfig("Bedroom", bedroom, "upstairs"))
	catalog := a.instanceNamed(t, "Lounge").Catalog

	require.Eventually(t, func() bool { return catalog.Stats().Counts[media.KindMovie] == 7 }, eventually, 10*time.Millisecond)

	// Aliases are saved against whichever member was the source at the time.
	require.NoError(t, a.DB().Aliases().Add(t.Context(), &store.TitleAlias{KodiInstanceID: a.instanceNamed(t, "Bedroom").id, Media: media.Ref{Kind: media.KindMovie, ID: 4}, Alias: "rocky two"}))

	out, err := a.SlotValues(t.Context())
	require.NoError(t, err)

	assert.Len(t, out["AMAZON.Movie"], 6)
	assert.Equal(t, []string{"rocky two"}, slotValue(t, out["AMAZON.Movie"], "Rocky II").Synonyms)
	assert.NotContains(t, out, "KODI_ROOM", "no rooms, no room values")
}

func TestSlotValuesOfSeparateLibrariesAreMerged(t *testing.T) {
	other := &fakekodi.Library{Movies: []fakekodi.Movie{
		{MovieID: 6, Title: "Arrival", Label: "Arrival", DateAdded: "2026-01-01 10:00:00"},
		{MovieID: 70, Title: "hot fuzz", Label: "hot fuzz", DateAdded: "2026-01-01 10:00:00"},
	}}
	lounge, bedroom := fakekodi.New(t, fakekodi.Options{}), fakekodi.New(t, fakekodi.Options{Library: other})
	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, "none"), kodiConfig("Bedroom", bedroom, "none"))

	require.Eventually(t, func() bool {
		return a.instanceNamed(t, "Lounge").Catalog.Stats().Counts[media.KindMovie] == 7 && a.instanceNamed(t, "Bedroom").Catalog.Stats().Counts[media.KindMovie] == 2
	}, eventually, 10*time.Millisecond)

	out, err := a.SlotValues(t.Context())
	require.NoError(t, err)

	movies := out["AMAZON.Movie"]
	assert.Len(t, movies, 7, "Hot Fuzz is in both libraries but spoken once")
	assert.Equal(t, "movie/6", slotValue(t, movies, "Hot Fuzz").ID, "the lowest instance's library is read first")
	assert.Empty(t, slotValue(t, movies, "Arrival").ID, "another library's movie 6 isn't Hot Fuzz, so it gets no ID")
}

func TestSlotValuesWithoutLibraries(t *testing.T) {
	a := newBareApp(t)

	out, err := a.SlotValues(t.Context())
	require.NoError(t, err)
	assert.Empty(t, out)

	require.NoError(t, a.DB().Close())

	_, err = a.SlotValues(t.Context())
	require.Error(t, err, "rooms that can't be read fail the build rather than leave the room slot out")
}

func TestSlotBuilder(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	b := newSlotBuilder()
	b.add(&media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune", DateAdded: old}, []string{"the spice one"})
	// The same title from a second library merges, keeping the more recent date and both aliases.
	b.add(&media.Item{Kind: media.KindMovie, ID: 9, Title: "dune", DateAdded: old, LastPlayed: recent}, []string{"Dune", "arrakis"})
	// A second library reusing ID 1 for another title gets no ID rather than a wrong one.
	b.add(&media.Item{Kind: media.KindMovie, ID: 1, Title: "Hot Fuzz", DateAdded: old}, nil)
	b.add(&media.Item{Kind: media.KindMovie, ID: 2, Title: "  "}, nil)

	assert.Equal(t, []skillmodel.SlotValue{
		{ID: "movie/1", Value: "Dune", Synonyms: []string{"the spice one", "arrakis"}},
		{Value: "Hot Fuzz"},
	}, b.values(0))

	assert.Equal(t, []skillmodel.SlotValue{{ID: "movie/1", Value: "Dune", Synonyms: []string{"the spice one", "arrakis"}}}, b.values(1), "the cap keeps the most recently played")
}

func TestSlotValuesIgnorePlayback(t *testing.T) {
	build := func(dunePlayed time.Time) []skillmodel.SlotValue {
		b := newSlotBuilder()
		b.add(&media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune", DateAdded: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), LastPlayed: dunePlayed}, nil)
		b.add(&media.Item{Kind: media.KindMovie, ID: 2, Title: "Arrival", DateAdded: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}, nil)

		return b.values(0)
	}

	unplayed := build(time.Time{})
	assert.Equal(t, []string{"Arrival", "Dune"}, []string{unplayed[0].Value, unplayed[1].Value}, "sorted by title")
	assert.Equal(t, unplayed, build(time.Date(2026, 10, 27, 10, 53, 0, 0, time.UTC)), "playing a title doesn't change the model")
}
