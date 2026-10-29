package actions

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/core/media"
)

func TestNavigate(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.Navigate(t.Context(), target, media.InputDown, 3)
	assert.Empty(t, o.Say.ID)
	assert.False(t, o.EndSession, "navigation keeps the session open like a remote")

	svc.Navigate(t.Context(), target, media.InputSelect, 0)
	svc.Navigate(t.Context(), target, media.InputUp, 500)

	calls := backend.Calls()
	assert.Equal(t, []string{"Input down", "Input down", "Input down", "Input select"}, calls[:4])
	assert.Len(t, calls, 4+maxPresses, "presses are capped")
}

func TestSendText(t *testing.T) {
	target, backend, _ := newTarget()

	assert.Empty(t, newService().SendText(t.Context(), target, "the expanse").Say.ID)
	assert.Equal(t, "remote.no_text", newService().SendText(t.Context(), target, " ").Say.ID)
	assert.Equal(t, []string{"SendText the expanse"}, backend.Calls())
}

func TestShowSection(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.ShowSection(t.Context(), target, media.SectionMovies, "")
	assert.False(t, o.EndSession)

	svc.ShowSection(t.Context(), target, media.SectionMovies, "sci fi")
	svc.ShowSection(t.Context(), target, media.SectionMusic, "rock")
	svc.ShowSection(t.Context(), target, media.SectionAlbums, "rock")

	o = svc.ShowSection(t.Context(), target, media.SectionTVShows, "westerns")
	assert.Equal(t, "library.no_genre", o.Say.ID)
	assert.Equal(t, "westerns", o.Say.Params["genre"])

	assert.Equal(t, []string{
		"ShowView movies",
		"Library movie", "ShowView movies genre Science Fiction",
		"Library song", "ShowView music genre Rock",
		"ShowView albums",
		"Library tvshow",
	}, backend.Calls())
}

func TestShowPlaylist(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().ShowPlaylist(t.Context(), target, "road trip")
	assert.False(t, o.EndSession)
	assert.Equal(t, []string{"ShowView playlist playlist/70"}, backend.Calls())

	assert.Equal(t, "playlist.not_found", newService().ShowPlaylist(t.Context(), target, "gym").Say.ID)
}

func TestOpenFavourite(t *testing.T) {
	target, backend, _ := newTarget()
	backend.SetFavourites(
		media.Favourite{Title: "Kids Movies", Type: "window"},
		media.Favourite{Title: "Radio National", Type: "media"},
	)

	svc := newService()

	o := svc.OpenFavourite(t.Context(), target, "radio national")
	assert.Equal(t, "favourite.opening", o.Say.ID)
	assert.Equal(t, "Radio National", o.Say.Params["favourite"])

	o = svc.OpenFavourite(t.Context(), target, "kids")
	assert.Equal(t, "Kids Movies", o.Say.Params["favourite"], "a prefix is enough")

	assert.Equal(t, "favourite.not_found", svc.OpenFavourite(t.Context(), target, "sport").Say.ID)

	o = svc.OpenFavourite(t.Context(), target, "")
	assert.False(t, o.EndSession, "no name opens the favourites view")

	assert.Contains(t, backend.Calls(), "OpenFavourite Kids Movies")
	assert.Contains(t, backend.Calls(), "ShowView favourites")
}

func TestBestName(t *testing.T) {
	names := []string{"YouTube", "Global Search", "Netflix", "The Movie Database"}

	for spoken, want := range map[string]int{"youtube": 0, "you tube": 0, "global": 1, "movie database": 3, "the netflix app": 2} {
		i, ok := bestName("en", names, spoken)
		assert.True(t, ok, spoken)
		assert.Equal(t, want, i, spoken)
	}

	_, ok := bestName("en", names, "spotify")
	assert.False(t, ok)
}

func TestBestNameRanks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		names  []string
		spoken string
		want   int
		ok     bool
	}{
		{"exact beats an earlier prefix", []string{"Radio National News", "Radio National"}, "radio national", 1, true},
		{"spacing beats a prefix", []string{"YouTube Music", "You Tube"}, "youtube", 1, true},
		{"prefix beats contains", []string{"Classic Radio", "Radio Classics"}, "radio", 1, true},
		{"contains beats contained", []string{"ABC", "Kids ABC Shows"}, "abc shows", 1, true},
		{"the first of equals wins", []string{"Radio One", "Radio Two"}, "radio", 0, true},
		{"unnamed entries are skipped", []string{"", "???", "Radio"}, "radio", 2, true},
		{"blank spoken name", []string{"Radio"}, "  ", 0, false},
		{"no names", nil, "radio", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, ok := bestName("en", tc.names, tc.spoken)
			assert.Equal(t, tc.ok, ok)

			if tc.ok {
				assert.Equal(t, tc.want, i)
			}
		})
	}
}

func TestShowSectionWithoutTheGenreView(t *testing.T) {
	target, backend, _ := newTarget()
	target, _ = failing(target, backend, media.ErrNotFound, "ShowView")

	o := newService().ShowSection(t.Context(), target, media.SectionAlbums, "rock")

	assert.Equal(t, "library.no_genre", o.Say.ID, "Kodi has no view for the genre")
	assert.Equal(t, "rock", o.Say.Params["genre"])
	assert.True(t, o.EndSession)
}

func TestOpenFavouriteThatFails(t *testing.T) {
	target, backend, _ := newTarget()
	backend.SetFavourites(media.Favourite{Title: "Radio National", Type: "media"})
	target, _ = failing(target, backend, errBoom, "OpenFavourite")

	o := newService().OpenFavourite(t.Context(), target, "radio national")

	assert.Equal(t, "favourite.cant_open", o.Say.ID)
	assert.Equal(t, map[string]string{"favourite": "Radio National"}, o.Say.Params)
	assert.ErrorIs(t, o.Err, errBoom, "the cause is kept for the log")
}
