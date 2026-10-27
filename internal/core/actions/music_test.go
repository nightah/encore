package actions

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestPlaylist(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	assert.Equal(t, "playlist.playing", svc.Playlist(t.Context(), target, "road trip", false, nil).Say.ID)
	assert.Equal(t, "playlist.shuffling", svc.Playlist(t.Context(), target, "road trip", true, nil).Say.ID)
	assert.Equal(t, []string{"Play playlist/70", "Play playlist/70 shuffle limit 100"}, backend.Calls())

	assert.Equal(t, "playlist.not_found", svc.Playlist(t.Context(), target, "gym", false, nil).Say.ID)
}
