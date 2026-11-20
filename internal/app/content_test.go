package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/core/media"
)

func TestCatalogKinds(t *testing.T) {
	testCases := []struct {
		name     string
		have     []string
		expected []media.Kind
	}{
		{name: "ShouldFetchEveryKindOfEachContent", have: []string{"movies", "tv", "music"}, expected: []media.Kind{media.KindMovie, media.KindTVShow, media.KindMusicVideo, media.KindArtist, media.KindAlbum, media.KindSong, media.KindPlaylist}},
		{name: "ShouldFetchNothingOfMusicWithoutIt", have: []string{"tv", "movies"}, expected: []media.Kind{media.KindMovie, media.KindTVShow, media.KindPlaylist}},
		// Playlists can hold anything, so they stay.
		{name: "ShouldKeepPlaylistsWithoutContent", expected: []media.Kind{media.KindPlaylist}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, catalogKinds(tc.have))
		})
	}
}
