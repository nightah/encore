package actions

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

func lastCall(calls []string) string {
	if len(calls) == 0 {
		return ""
	}

	return calls[len(calls)-1]
}

func TestArtist(t *testing.T) {
	target, backend, _ := newTarget()
	svc := seeded()

	o := svc.Artist(t.Context(), target, MusicRequest{Artist: "queen"}, nil)
	assert.Equal(t, "music.playing_artist", o.Say.ID)
	assert.Equal(t, "Play artist/30 shuffle limit 100", lastCall(backend.Calls()), "artists always shuffle, capped at the default limit")

	assert.Equal(t, "playback.shuffling", svc.Artist(t.Context(), target, MusicRequest{Artist: "queen", Shuffle: true}, nil).Say.ID)

	o = svc.Artist(t.Context(), target, MusicRequest{Artist: "the beatles", Genre: "pop"}, nil)
	assert.Equal(t, "music.playing_genre_by", o.Say.ID)
	assert.Equal(t, map[string]string{"genre": "Pop", "artist": "The Beatles"}, o.Say.Params)
	assert.Equal(t, "Queue song/52", lastCall(backend.Calls()))

	assert.Equal(t, "music.no_genre_by", svc.Artist(t.Context(), target, MusicRequest{Artist: "queen", Genre: "jazz"}, nil).Say.ID)
	assert.Equal(t, "match.not_found", svc.Artist(t.Context(), target, MusicRequest{Artist: "abba"}, nil).Say.ID)
}

func TestAlbum(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.Album(t.Context(), target, MusicRequest{Album: "abbey road"}, nil)
	assert.Equal(t, "music.playing_album", o.Say.ID)
	assert.Equal(t, map[string]string{"title": "Abbey Road", "artist": "The Beatles"}, o.Say.Params)
	assert.Equal(t, "Play album/42", lastCall(backend.Calls()))

	o = svc.Album(t.Context(), target, MusicRequest{Album: "innuendo", Artist: "queen", Shuffle: true}, nil)
	assert.Equal(t, "music.shuffling_album", o.Say.ID)
	assert.Equal(t, "Play album/41 shuffle limit 100", lastCall(backend.Calls()))

	o = svc.Album(t.Context(), target, MusicRequest{Album: "abbey road", Artist: "queen"}, nil)
	assert.Equal(t, "music.not_found_by", o.Say.ID)
}

func TestLatestAlbum(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().LatestAlbum(t.Context(), target, MusicRequest{Artist: "queen"}, nil)
	assert.Equal(t, "Innuendo", o.Say.Params["title"])
	assert.Equal(t, "Play album/41", lastCall(backend.Calls()))
}

func TestSong(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	o := svc.Song(t.Context(), target, MusicRequest{Title: "bohemian rhapsody"}, nil)
	assert.Equal(t, "music.playing_song", o.Say.ID)
	assert.Equal(t, "Play song/50", lastCall(backend.Calls()), "the song, not the music video of the same name")

	assert.Equal(t, "music.not_found_by", svc.Song(t.Context(), target, MusicRequest{Title: "come together", Artist: "queen"}, nil).Say.ID)
	assert.Equal(t, "music.not_found_on", svc.Song(t.Context(), target, MusicRequest{Title: "come together", Album: "innuendo"}, nil).Say.ID)

	o = svc.Song(t.Context(), target, MusicRequest{Title: "love of my life", Album: "a night at the opera"}, nil)
	assert.Equal(t, "music.playing_song", o.Say.ID)
}

func TestSongOrAlbum(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	svc.SongOrAlbum(t.Context(), target, MusicRequest{Title: "come together", Artist: "the beatles"}, nil)
	assert.Equal(t, "Play song/52", lastCall(backend.Calls()))

	o := svc.SongOrAlbum(t.Context(), target, MusicRequest{Title: "innuendo", Artist: "queen"}, nil)
	assert.Equal(t, "music.playing_album", o.Say.ID)
	assert.Equal(t, "Play album/41", lastCall(backend.Calls()))
}

func TestGenreRecentAndParty(t *testing.T) {
	target, backend, _ := newTarget()
	target.QueueLimit = 2
	svc := seeded()

	o := svc.Genre(t.Context(), target, "rock", nil)
	assert.Equal(t, "music.playing_genre", o.Say.ID)
	assert.Equal(t, "Rock", o.Say.Params["genre"])

	queued := strings.Split(strings.TrimPrefix(lastCall(backend.Calls()), "Queue "), ",")
	assert.Len(t, queued, 2, "capped at the queue limit")

	assert.Equal(t, "music.playing_recent", svc.RecentSongs(t.Context(), target, nil).Say.ID)
	assert.Contains(t, lastCall(backend.Calls()), "song/51", "the newest song is queued")

	assert.Equal(t, "music.party_mode", svc.PartyMode(t.Context(), target, nil).Say.ID)
	assert.Equal(t, "PartyMode", lastCall(backend.Calls()))
}

func TestMusicVideos(t *testing.T) {
	target, backend, _ := newTarget()
	svc := seeded()

	o := svc.MusicVideo(t.Context(), target, MusicRequest{Title: "bohemian rhapsody", Artist: "queen"}, nil)
	assert.Equal(t, "music.playing_video", o.Say.ID)
	assert.Equal(t, "Play musicvideo/60 resume", lastCall(backend.Calls()))

	o = svc.RandomMusicVideo(t.Context(), target, MusicRequest{Genre: "rock"}, nil)
	require.NotNil(t, o.Played)
	assert.Equal(t, 60, o.Played.ID)

	assert.Equal(t, "music.shuffling_videos", svc.ShuffleMusicVideos(t.Context(), target, MusicRequest{Artist: "queen"}, nil).Say.ID)
	assert.Equal(t, "Queue musicvideo/60", lastCall(backend.Calls()))

	assert.Equal(t, "music.no_videos_by", svc.ShuffleMusicVideos(t.Context(), target, MusicRequest{Artist: "the beatles"}, nil).Say.ID)
	assert.Equal(t, "music.no_videos_genre_by", svc.RandomMusicVideo(t.Context(), target, MusicRequest{Genre: "rock", Artist: "the beatles"}, nil).Say.ID)
}

func TestFindByAsksKodiForTitlesNotCachedYet(t *testing.T) {
	var cached []media.Item

	for _, it := range testkit.SampleLibrary() {
		if it.Kind == media.KindAlbum && it.ID != 41 {
			cached = append(cached, it)
		}
	}

	req := MusicRequest{Album: "innuendo", Artist: "queen"}

	target, backend, _ := newTarget()
	target.Library = &testkit.Library{Cached: cached}

	o := newService().Album(t.Context(), target, req, nil)
	assert.Equal(t, "music.playing_album", o.Say.ID)
	assert.Equal(t, []string{"Item album/41", "Play album/41"}, backend.Calls())

	target, _ = failing(target, backend, errBoom, "Item")

	o = newService().Album(t.Context(), target, req, nil)
	assert.Equal(t, "music.not_found_by", o.Say.ID, "a title Kodi can't describe is skipped")
	assert.NoError(t, o.Err)
}

func TestMusicNotFound(t *testing.T) {
	target, backend, _ := newTarget(append(testkit.SampleLibrary(), media.Item{Kind: media.KindArtist, ID: 39, Title: "Kate Bush"})...)
	svc := newService()

	for name, tc := range map[string]struct {
		o      Outcome
		id     string
		params map[string]string
	}{
		"album":                 {svc.Album(t.Context(), target, MusicRequest{Album: "the wall"}, nil), "match.not_found", map[string]string{"query": "the wall"}},
		"song":                  {svc.Song(t.Context(), target, MusicRequest{Title: "yesterday"}, nil), "match.not_found", map[string]string{"query": "yesterday"}},
		"music video":           {svc.MusicVideo(t.Context(), target, MusicRequest{Title: "thriller"}, nil), "match.not_found", map[string]string{"query": "thriller"}},
		"music video by":        {svc.MusicVideo(t.Context(), target, MusicRequest{Title: "bohemian rhapsody", Artist: "the beatles"}, nil), "music.not_found_by", map[string]string{"title": "bohemian rhapsody", "artist": "the beatles"}},
		"latest album unknown":  {svc.LatestAlbum(t.Context(), target, MusicRequest{Artist: "abba"}, nil), "match.not_found", map[string]string{"query": "abba"}},
		"latest album none":     {svc.LatestAlbum(t.Context(), target, MusicRequest{Artist: "kate bush"}, nil), "music.no_albums_by", map[string]string{"artist": "Kate Bush"}},
		"artist genre none":     {svc.Artist(t.Context(), target, MusicRequest{Artist: "kate bush", Genre: "rock"}, nil), "music.no_genre_by", map[string]string{"genre": "rock", "artist": "Kate Bush"}},
		"genre":                 {svc.Genre(t.Context(), target, "polka", nil), "library.no_genre", map[string]string{"genre": "polka"}},
		"song or album neither": {svc.SongOrAlbum(t.Context(), target, MusicRequest{Title: "yesterday", Artist: "the beatles"}, nil), "music.not_found_by", map[string]string{"title": "yesterday", "artist": "the beatles"}},
	} {
		assert.Equal(t, tc.id, tc.o.Say.ID, name)
		assert.Equal(t, tc.params, tc.o.Say.Params, name)
		assert.True(t, tc.o.EndSession, name)
		assert.Nil(t, tc.o.Played, name)
	}

	for _, c := range backend.Calls() {
		assert.NotContains(t, c, "Play", "nothing plays")
		assert.NotContains(t, c, "Queue", "nothing is queued")
	}
}

func TestMusicWithoutAnyMusic(t *testing.T) {
	target, backend, _ := newTarget(media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune"})
	svc := newService()

	assert.Equal(t, "music.no_recent_songs", svc.RecentSongs(t.Context(), target, nil).Say.ID)
	assert.Equal(t, "library.empty", svc.Genre(t.Context(), target, "", nil).Say.ID)
	assert.Equal(t, "library.empty", svc.RandomMusicVideo(t.Context(), target, MusicRequest{}, nil).Say.ID)
	assert.Equal(t, "library.empty", svc.ShuffleMusicVideos(t.Context(), target, MusicRequest{}, nil).Say.ID)
	assert.Equal(t, []string{"Library song", "Library song", "Library musicvideo", "Library musicvideo"}, backend.Calls())
}

func TestLatestAlbumBreaksTies(t *testing.T) {
	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []media.Item{
		{Kind: media.KindArtist, ID: 1, Title: "Kate Bush"},
		{Kind: media.KindAlbum, ID: 10, Title: "Hounds of Love", Artist: "Kate Bush", Year: 1985, DateAdded: jan},
		{Kind: media.KindAlbum, ID: 11, Title: "Hounds of Love (Remaster)", Artist: "Kate Bush", Year: 1985, DateAdded: jan.AddDate(0, 1, 0)},
		{Kind: media.KindAlbum, ID: 12, Title: "The Kick Inside", Artist: "Kate Bush", Year: 1978, DateAdded: jan.AddDate(1, 0, 0)},
		{Kind: media.KindAlbum, ID: 13, Title: "Aerial", Artist: "Someone Else", Year: 2005},
	}
	target, backend, _ := newTarget(items...)

	o := newService().LatestAlbum(t.Context(), target, MusicRequest{Artist: "kate bush", Shuffle: true}, nil)

	assert.Equal(t, "music.shuffling_album", o.Say.ID)
	assert.Equal(t, "Hounds of Love (Remaster)", o.Say.Params["title"], "the newest year wins, then the latest added")
	assert.Equal(t, "Play album/11 shuffle limit 100", lastCall(backend.Calls()))
}

func TestPlaylist(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	assert.Equal(t, "playlist.playing", svc.Playlist(t.Context(), target, "road trip", false, nil).Say.ID)
	assert.Equal(t, "playlist.shuffling", svc.Playlist(t.Context(), target, "road trip", true, nil).Say.ID)
	assert.Equal(t, []string{"Play playlist/70", "Play playlist/70 shuffle limit 100"}, backend.Calls())

	assert.Equal(t, "playlist.not_found", svc.Playlist(t.Context(), target, "gym", false, nil).Say.ID)
}
