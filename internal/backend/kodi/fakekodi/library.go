package fakekodi

func (m Movie) id() int                { return m.MovieID }
func (m Movie) title() string          { return m.Title }
func (m Movie) playcount() int         { return m.PlayCount }
func (m Movie) dateadded() string      { return m.DateAdded }
func (t TVShow) id() int               { return t.TVShowID }
func (t TVShow) title() string         { return t.Title }
func (t TVShow) playcount() int        { return t.PlayCount }
func (t TVShow) dateadded() string     { return t.DateAdded }
func (e Episode) id() int              { return e.EpisodeID }
func (e Episode) title() string        { return e.Title }
func (e Episode) playcount() int       { return e.PlayCount }
func (e Episode) dateadded() string    { return e.DateAdded }
func (v MusicVideo) id() int           { return v.MusicVideoID }
func (v MusicVideo) title() string     { return v.Title }
func (v MusicVideo) playcount() int    { return v.PlayCount }
func (v MusicVideo) dateadded() string { return v.DateAdded }
func (a Artist) id() int               { return a.ArtistID }
func (a Artist) title() string         { return a.Artist }
func (Artist) playcount() int          { return 0 }
func (a Artist) dateadded() string     { return a.DateAdded }
func (a Album) id() int                { return a.AlbumID }
func (a Album) title() string          { return a.Title }
func (a Album) playcount() int         { return a.PlayCount }
func (a Album) dateadded() string      { return a.DateAdded }
func (s Song) id() int                 { return s.SongID }
func (s Song) title() string           { return s.Title }
func (s Song) playcount() int          { return s.PlayCount }
func (s Song) dateadded() string       { return s.DateAdded }

// DefaultLibrary is a small library covering the matcher's tricky cases: remakes and sequels sharing a title, roman numerals, diacritics, a show that exists in two countries, a title added "just now", an episode part way through, and enough music for album and artist playback.
func DefaultLibrary() *Library {
	sciFi := []string{"Science Fiction"}

	return &Library{
		Movies: []Movie{
			{MovieID: 1, Label: "Dune", Title: "Dune", Year: 1984, PlayCount: 1, Runtime: 8220, DateAdded: "2019-03-02 10:00:00", LastPlayed: "2020-01-01 21:00:00", Genre: sciFi, Trailer: "plugin://plugin.video.youtube/play/?video_id=KwPTIEWTYEI"},
			{MovieID: 2, Label: "Dune", Title: "Dune", Year: 2021, PlayCount: 1, Runtime: 9300, DateAdded: "2022-01-15 10:00:00", LastPlayed: "2022-02-01 20:00:00", Genre: sciFi, Set: "Dune Collection", SetID: 1},
			{MovieID: 3, Label: "Dune: Part Two", Title: "Dune: Part Two", Year: 2024, Runtime: 9960, DateAdded: "2024-06-01 10:00:00", Genre: sciFi, Resume: Resume{Position: 3605.5, Total: 9960}, Set: "Dune Collection", SetID: 1},
			{MovieID: 4, Label: "Rocky II", Title: "Rocky II", Year: 1979, Runtime: 7140, DateAdded: "2020-05-05 10:00:00", Genre: []string{"Drama"}},
			{MovieID: 5, Label: "Amélie", Title: "Amélie", SortTitle: "Amelie", OriginalTitle: "Le Fabuleux Destin d'Amélie Poulain", Year: 2001, Runtime: 7320, DateAdded: "2020-05-06 10:00:00", Genre: []string{"Comedy", "Romance"}},
			{MovieID: 6, Label: "Hot Fuzz", Title: "Hot Fuzz", Year: 2007, PlayCount: 3, Runtime: 7260, DateAdded: "2018-11-11 10:00:00", LastPlayed: "2024-12-24 22:10:00", Genre: []string{"Comedy"}},
			{MovieID: 7, Label: "Conclave", Title: "Conclave", Year: 2024, Runtime: 7200, DateAdded: "2026-10-26 20:37:00", Genre: []string{"Thriller"}},
		},
		TVShows: []TVShow{
			{TVShowID: 1, Label: "Severance", Title: "Severance", Year: 2022, Genre: []string{"Drama", "Mystery"}, Episode: 3, WatchedEpisodes: 1, DateAdded: "2022-02-18 10:00:00"},
			{TVShowID: 2, Label: "Bluey", Title: "Bluey", Year: 2018, Genre: []string{"Animation", "Kids"}, Episode: 1, DateAdded: "2021-01-01 10:00:00"},
			{TVShowID: 3, Label: "The Office (US)", Title: "The Office (US)", SortTitle: "Office (US)", Year: 2005, Genre: []string{"Comedy"}, DateAdded: "2020-01-01 10:00:00"},
			{TVShowID: 4, Label: "The Office (UK)", Title: "The Office (UK)", SortTitle: "Office (UK)", Year: 2001, Genre: []string{"Comedy"}, DateAdded: "2020-01-01 10:00:00"},
		},
		Episodes: []Episode{
			{EpisodeID: 101, TVShowID: 1, ShowTitle: "Severance", Season: 2, Episode: 3, Title: "Who Is Alive?", Label: "2x03. Who Is Alive?", PlayCount: 1, Runtime: 2820, DateAdded: "2025-01-31 10:00:00", LastPlayed: "2025-02-01 21:00:00"},
			{EpisodeID: 102, TVShowID: 1, ShowTitle: "Severance", Season: 2, Episode: 4, Title: "Woe's Hollow", Label: "2x04. Woe's Hollow", Runtime: 3000, DateAdded: "2025-02-07 10:00:00"},
			{EpisodeID: 103, TVShowID: 1, ShowTitle: "Severance", Season: 2, Episode: 5, Title: "Trojan's Horse", Label: "2x05. Trojan's Horse", Runtime: 2940, DateAdded: "2025-02-14 10:00:00"},
			{EpisodeID: 201, TVShowID: 2, ShowTitle: "Bluey", Season: 3, Episode: 12, Title: "Rain", Label: "3x12. Rain", Runtime: 420, DateAdded: "2022-06-01 10:00:00"},
		},
		MusicVideos: []MusicVideo{
			{MusicVideoID: 1, Label: "Bohemian Rhapsody", Title: "Bohemian Rhapsody", Artist: []string{"Queen"}, Album: "A Night at the Opera", Year: 1975, Genre: []string{"Rock"}, Runtime: 359, DateAdded: "2023-03-03 10:00:00"},
		},
		Artists: []Artist{
			{ArtistID: 1, Label: "Queen", Artist: "Queen", SortName: "Queen", Genre: []string{"Rock"}, DateAdded: "2021-05-01 10:00:00"},
			{ArtistID: 2, Label: "The Beatles", Artist: "The Beatles", SortName: "Beatles, The", Genre: []string{"Rock", "Pop"}, DateAdded: "2021-05-01 10:00:00"},
		},
		Albums: []Album{
			{AlbumID: 1, Label: "A Night at the Opera", Title: "A Night at the Opera", Artist: []string{"Queen"}, DisplayArtist: "Queen", Year: 1975, Genre: []string{"Rock"}, PlayCount: 2, DateAdded: "2021-05-01 10:00:00"},
			{AlbumID: 2, Label: "Abbey Road", Title: "Abbey Road", Artist: []string{"The Beatles"}, DisplayArtist: "The Beatles", Year: 1969, Genre: []string{"Rock"}, DateAdded: "2021-05-01 10:00:00"},
		},
		Songs: []Song{
			{SongID: 1, Label: "Bohemian Rhapsody", Title: "Bohemian Rhapsody", Artist: []string{"Queen"}, DisplayArtist: "Queen", Album: "A Night at the Opera", AlbumID: 1, Year: 1975, Genre: []string{"Rock"}, Track: 11, Duration: 355, PlayCount: 5, DateAdded: "2021-05-01 10:00:00"},
			{SongID: 2, Label: "Love of My Life", Title: "Love of My Life", Artist: []string{"Queen"}, DisplayArtist: "Queen", Album: "A Night at the Opera", AlbumID: 1, Year: 1975, Genre: []string{"Rock"}, Track: 9, Duration: 219, DateAdded: "2021-05-01 10:00:00"},
			{SongID: 3, Label: "Come Together", Title: "Come Together", Artist: []string{"The Beatles"}, DisplayArtist: "The Beatles", Album: "Abbey Road", AlbumID: 2, Year: 1969, Genre: []string{"Rock"}, Track: 1, Duration: 259, DateAdded: "2021-05-01 10:00:00"},
		},
		Playlists: []Playlist{
			{File: "special://profile/playlists/video/Kids.xsp", Label: "Kids", Media: "video", Entries: []string{"/media/tv/Bluey/S03E12.mkv"}},
			{File: "special://profile/playlists/music/Road Trip.m3u", Label: "Road Trip", Media: "music", Entries: []string{"/media/music/Queen/11.flac", "/media/music/Queen/09.flac", "/media/music/Beatles/01.flac"}},
		},
		Favourites: []Favourite{
			{Title: "Kids Movies", Type: "window", Window: "10025", WindowParameter: "videodb://movies/genres/7/"},
			{Title: "Radio National", Type: "media", Path: "http://live-radio01.mediahubaustralia.com/2RNW/mp3/"},
			{Title: "Weather", Type: "script", Path: "script.weather"},
		},
		Addons: []Addon{
			{AddonID: "plugin.video.youtube", Type: "xbmc.python.pluginsource", Name: "YouTube", Enabled: true},
			{AddonID: "script.globalsearch", Type: "xbmc.python.script", Name: "Global Search", Enabled: true},
			{AddonID: "skin.estuary", Type: "xbmc.gui.skin", Name: "Estuary", Enabled: true},
			{AddonID: "plugin.video.disabled", Type: "xbmc.python.pluginsource", Name: "Disabled Plugin"},
		},
	}
}
