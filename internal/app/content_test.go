package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/core/media"
)

func TestCatalogKinds(t *testing.T) {
	assert.Equal(t, []media.Kind{
		media.KindMovie, media.KindTVShow,
		media.KindMusicVideo, media.KindArtist, media.KindAlbum, media.KindSong,
		media.KindPlaylist,
	}, catalogKinds([]string{"movies", "tv", "music"}))

	assert.Equal(t, []media.Kind{media.KindMovie, media.KindTVShow, media.KindPlaylist}, catalogKinds([]string{"tv", "movies"}), "without music, nothing of it is fetched")
	assert.Equal(t, []media.Kind{media.KindPlaylist}, catalogKinds(nil), "playlists can hold anything, so they stay")
}
