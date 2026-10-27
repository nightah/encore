package kodi

import (
	"math"
	"strings"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// Properties requested per kind. Every name exists in the Omega (API 13.5) schema.
var (
	movieFields = kodirpc.VideoFieldsMovie{
		"title", "sorttitle", "originaltitle", "year", "genre", "playcount", "resume", "runtime", "dateadded", "lastplayed",
		"trailer", "set", "setid",
	}
	tvShowFields = kodirpc.VideoFieldsTVShow{
		"title", "sorttitle", "originaltitle", "year", "genre", "playcount", "dateadded", "lastplayed",
	}
	episodeFields = kodirpc.VideoFieldsEpisode{
		"title", "originaltitle", "showtitle", "tvshowid", "season", "episode", "playcount", "resume", "runtime", "dateadded", "lastplayed",
	}
	musicVideoFields = kodirpc.VideoFieldsMusicVideo{
		"title", "artist", "album", "year", "genre", "playcount", "resume", "runtime", "dateadded", "lastplayed",
	}
	artistFields = kodirpc.AudioFieldsArtist{"sortname", "genre", "dateadded"}
	albumFields  = kodirpc.AudioFieldsAlbum{
		"title", "displayartist", "artist", "year", "genre", "playcount", "dateadded", "lastplayed",
	}
	songFields = kodirpc.AudioFieldsSong{
		"title", "displayartist", "artist", "album", "year", "genre", "playcount", "duration", "dateadded", "lastplayed",
	}
	playingFields = kodirpc.ListFieldsAll{
		"title", "sorttitle", "originaltitle", "showtitle", "tvshowid", "season", "episode", "artist", "album", "year",
		"genre", "playcount", "resume", "runtime", "duration", "dateadded", "lastplayed", "art", "thumbnail",
	}
)

// Empty or malformed values are zero.
func (m mapper) parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	t, err := time.ParseInLocation(kodiTimeLayout, s, m.zone.location())
	if err != nil {
		return time.Time{}
	}

	return t
}

func seconds[N int | float64](n N) time.Duration {
	return time.Duration(math.Round(float64(n) * float64(time.Second)))
}

func resumeOf(r *kodirpc.VideoResume) time.Duration {
	if r == nil {
		return 0
	}

	return seconds(r.Position)
}

func titleOr(title, label string) string {
	if title != "" {
		return title
	}

	return label
}

func artists(display string, names []string) string {
	if display != "" {
		return display
	}

	return strings.Join(names, ", ")
}

func genres(g kodirpc.ArrayString) []string {
	if len(g) == 0 {
		return nil
	}

	return []string(g)
}

func (m mapper) movie(v *kodirpc.VideoDetailsMovie) media.Item {
	return media.Item{
		Kind:          media.KindMovie,
		ID:            int(v.MovieID),
		Title:         titleOr(v.Title, v.Label),
		SortTitle:     v.Sorttitle,
		OriginalTitle: v.Originaltitle,
		Year:          v.Year,
		Genres:        genres(v.Genre),
		PlayCount:     v.Playcount,
		Resume:        resumeOf(v.Resume),
		Runtime:       seconds(v.Runtime),
		DateAdded:     m.parseTime(v.Dateadded),
		LastPlayed:    m.parseTime(v.Lastplayed),
		Trailer:       v.Trailer,
		SetID:         int(v.SetID),
		SetTitle:      v.Set,
	}
}

func (m mapper) tvShow(v *kodirpc.VideoDetailsTVShow) media.Item {
	return media.Item{
		Kind:          media.KindTVShow,
		ID:            int(v.TvshowID),
		Title:         titleOr(v.Title, v.Label),
		SortTitle:     v.Sorttitle,
		OriginalTitle: v.Originaltitle,
		Year:          v.Year,
		Genres:        genres(v.Genre),
		PlayCount:     v.Playcount,
		DateAdded:     m.parseTime(v.Dateadded),
		LastPlayed:    m.parseTime(v.Lastplayed),
	}
}

func (m mapper) episode(v *kodirpc.VideoDetailsEpisode) media.Item {
	return media.Item{
		Kind:          media.KindEpisode,
		ID:            int(v.EpisodeID),
		Title:         titleOr(v.Title, v.Label),
		OriginalTitle: v.Originaltitle,
		ShowID:        int(v.TvshowID),
		ShowTitle:     v.Showtitle,
		Season:        v.Season,
		Episode:       v.Episode,
		PlayCount:     v.Playcount,
		Resume:        resumeOf(v.Resume),
		Runtime:       seconds(v.Runtime),
		DateAdded:     m.parseTime(v.Dateadded),
		LastPlayed:    m.parseTime(v.Lastplayed),
	}
}

func (m mapper) musicVideo(v *kodirpc.VideoDetailsMusicVideo) media.Item {
	return media.Item{
		Kind:       media.KindMusicVideo,
		ID:         int(v.MusicvideoID),
		Title:      titleOr(v.Title, v.Label),
		Year:       v.Year,
		Genres:     genres(v.Genre),
		Artist:     artists("", v.Artist),
		Album:      v.Album,
		PlayCount:  v.Playcount,
		Resume:     resumeOf(v.Resume),
		Runtime:    seconds(v.Runtime),
		DateAdded:  m.parseTime(v.Dateadded),
		LastPlayed: m.parseTime(v.Lastplayed),
	}
}

func (m mapper) artist(v *kodirpc.AudioDetailsArtist) media.Item {
	return media.Item{
		Kind:      media.KindArtist,
		ID:        int(v.ArtistID),
		Title:     titleOr(v.Artist, v.Label),
		SortTitle: v.Sortname,
		Genres:    genres(v.Genre),
		Artist:    titleOr(v.Artist, v.Label),
		DateAdded: m.parseTime(v.Dateadded),
	}
}

func (m mapper) album(v *kodirpc.AudioDetailsAlbum) media.Item {
	return media.Item{
		Kind:       media.KindAlbum,
		ID:         int(v.AlbumID),
		Title:      titleOr(v.Title, v.Label),
		Year:       v.Year,
		Genres:     genres(v.Genre),
		Artist:     artists(v.Displayartist, v.Artist),
		Album:      titleOr(v.Title, v.Label),
		PlayCount:  v.Playcount,
		DateAdded:  m.parseTime(v.Dateadded),
		LastPlayed: m.parseTime(v.Lastplayed),
	}
}

func (m mapper) song(v *kodirpc.AudioDetailsSong) media.Item {
	return media.Item{
		Kind:       media.KindSong,
		ID:         int(v.SongID),
		Title:      titleOr(v.Title, v.Label),
		Year:       v.Year,
		Genres:     genres(v.Genre),
		Artist:     artists(v.Displayartist, v.Artist),
		Album:      v.Album,
		PlayCount:  v.Playcount,
		Runtime:    seconds(v.Duration),
		DateAdded:  m.parseTime(v.Dateadded),
		LastPlayed: m.parseTime(v.Lastplayed),
	}
}

// The item may be outside the library, such as a file or a TV channel.
func (m mapper) playing(v *kodirpc.ListItemAll) media.Item {
	it := media.Item{
		ID:            int(v.ID),
		Title:         titleOr(v.Title, v.Label),
		SortTitle:     v.Sorttitle,
		OriginalTitle: v.Originaltitle,
		Year:          v.Year,
		Genres:        genres(v.Genre),
		ShowID:        int(v.TvshowID),
		ShowTitle:     v.Showtitle,
		Season:        v.Season,
		Episode:       v.Episode,
		Artist:        artists(v.Displayartist, v.Artist),
		Album:         v.Album,
		PlayCount:     v.Playcount,
		Resume:        resumeOf(v.Resume),
		Runtime:       seconds(max(v.Runtime, v.Duration)),
		DateAdded:     m.parseTime(v.Dateadded),
		LastPlayed:    m.parseTime(v.Lastplayed),
		Art:           artOf(v.Art, v.Thumbnail),
	}

	switch v.Type {
	case kodirpc.ListItemBaseTypeMovie:
		it.Kind = media.KindMovie
	case kodirpc.ListItemBaseTypeEpisode:
		it.Kind = media.KindEpisode
	case kodirpc.ListItemBaseTypeMusicvideo:
		it.Kind = media.KindMusicVideo
	case kodirpc.ListItemBaseTypeSong:
		it.Kind = media.KindSong
	default:
		// Not a library item; an ID would be meaningless.
		it.ID = 0
	}

	return it
}

func kindOf(typ string) (media.Kind, bool) {
	switch k := media.Kind(typ); k {
	case media.KindMovie, media.KindTVShow, media.KindEpisode, media.KindMusicVideo, media.KindSong, media.KindAlbum, media.KindArtist:
		return k, true
	}

	return "", false
}

func positionTime(d time.Duration) kodirpc.PlayerPositionTime {
	ms := d.Milliseconds()

	return kodirpc.PlayerPositionTime{
		Hours:        int(ms / 3600000),
		Minutes:      int(ms / 60000 % 60),
		Seconds:      int(ms / 1000 % 60),
		Milliseconds: int(ms % 1000),
	}
}

func globalDuration(t *kodirpc.GlobalTime) time.Duration {
	if t == nil {
		return 0
	}

	return time.Duration(t.Hours)*time.Hour + time.Duration(t.Minutes)*time.Minute +
		time.Duration(t.Seconds)*time.Second + time.Duration(t.Milliseconds)*time.Millisecond
}

// The item's thumbnail stands in for a missing thumb.
func artOf(a *kodirpc.MediaArtwork, thumbnail string) media.Artwork {
	art := media.Artwork{Thumb: thumbnail}

	if a != nil {
		art.Poster, art.Fanart = string(a.Poster), string(a.Fanart)
		if a.Thumb != "" {
			art.Thumb = string(a.Thumb)
		}
	}

	return art
}
