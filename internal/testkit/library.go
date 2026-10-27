package testkit

import (
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// SampleLibrary covers the tricky cases handlers must cope with: a remake sharing a title, a sequel in a collection, a fully watched show, a show with unwatched episodes, an artist with several albums, and a song that is also a music video.
func SampleLibrary() []media.Item {
	added := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	later := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	sciFi := []string{"Science Fiction"}

	return []media.Item{
		{Kind: media.KindMovie, ID: 1, Title: "Dune", Year: 1984, Runtime: 137 * time.Minute, DateAdded: added, Genres: sciFi, PlayCount: 1},
		{Kind: media.KindMovie, ID: 2, Title: "Dune", Year: 2021, Runtime: 155 * time.Minute, DateAdded: added, Genres: sciFi, SetID: 1, SetTitle: "Dune Collection", PlayCount: 1},
		{Kind: media.KindMovie, ID: 3, Title: "Dune: Part Two", Year: 2024, Runtime: 166 * time.Minute, DateAdded: later, Genres: sciFi, SetID: 1, SetTitle: "Dune Collection"},
		{Kind: media.KindMovie, ID: 4, Title: "Hot Fuzz", Year: 2007, Runtime: 121 * time.Minute, DateAdded: added, Genres: []string{"Comedy", "Action"}, Trailer: "plugin://plugin.video.youtube/play/?video_id=ayTnvVpj9t4"},
		{Kind: media.KindTVShow, ID: 10, Title: "Severance", Year: 2022, Genres: []string{"Drama", "Mystery"}, LastPlayed: time.Date(2025, 2, 1, 21, 0, 0, 0, time.UTC)},
		{Kind: media.KindEpisode, ID: 101, ShowID: 10, ShowTitle: "Severance", Season: 2, Episode: 3, Title: "Who Is Alive?", PlayCount: 1, Runtime: 50 * time.Minute, DateAdded: later},
		{Kind: media.KindEpisode, ID: 102, ShowID: 10, ShowTitle: "Severance", Season: 2, Episode: 4, Title: "Woe's Hollow", Runtime: 49 * time.Minute, DateAdded: later},
		{Kind: media.KindTVShow, ID: 20, Title: "Bluey", Year: 2018, Genres: []string{"Animation", "Kids"}, PlayCount: 1, LastPlayed: time.Date(2025, 3, 1, 8, 0, 0, 0, time.UTC)},
		{Kind: media.KindEpisode, ID: 201, ShowID: 20, ShowTitle: "Bluey", Season: 3, Episode: 12, Title: "Rain", PlayCount: 2, Runtime: 7 * time.Minute, DateAdded: added},
		{Kind: media.KindEpisode, ID: 103, ShowID: 10, ShowTitle: "Severance", Season: 2, Episode: 5, Title: "Trojan's Horse", Runtime: 49 * time.Minute, DateAdded: later},
		{Kind: media.KindArtist, ID: 30, Title: "Queen", Artist: "Queen", Genres: []string{"Rock"}},
		{Kind: media.KindArtist, ID: 31, Title: "The Beatles", Artist: "The Beatles", Genres: []string{"Rock", "Pop"}},
		{Kind: media.KindAlbum, ID: 40, Title: "A Night at the Opera", Artist: "Queen", Year: 1975, Genres: []string{"Rock"}, DateAdded: added},
		{Kind: media.KindAlbum, ID: 41, Title: "Innuendo", Artist: "Queen", Year: 1991, Genres: []string{"Rock"}, DateAdded: later},
		{Kind: media.KindAlbum, ID: 42, Title: "Abbey Road", Artist: "The Beatles", Year: 1969, Genres: []string{"Rock"}, DateAdded: added},
		{Kind: media.KindSong, ID: 50, Title: "Bohemian Rhapsody", Artist: "Queen", Album: "A Night at the Opera", Genres: []string{"Rock"}, DateAdded: added, Runtime: 355 * time.Second},
		{Kind: media.KindSong, ID: 51, Title: "Love of My Life", Artist: "Queen", Album: "A Night at the Opera", Genres: []string{"Rock"}, DateAdded: later},
		{Kind: media.KindSong, ID: 52, Title: "Come Together", Artist: "The Beatles", Album: "Abbey Road", Genres: []string{"Rock", "Pop"}, DateAdded: added},
		{Kind: media.KindMusicVideo, ID: 60, Title: "Bohemian Rhapsody", Artist: "Queen", Genres: []string{"Rock"}, DateAdded: added},
		{Kind: media.KindPlaylist, ID: 70, Title: "Road Trip"},
	}
}
