package alexa

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
)

func (s slots) value(name string) string {
	return strings.TrimSpace(s.intent.Slots[name].Value)
}

// id returns the ID of a custom slot type's value: Alexa's entity resolution when it matched, otherwise a lookup of the heard words in the locale's values and synonyms. It returns "" when the slot is empty or unrecognised.
func (s slots) id(name, typ string) string {
	slot := s.intent.Slots[name]
	if ids := slot.ResolvedIDs(); len(ids) > 0 {
		return ids[0]
	}

	return s.speech.SlotID(s.locale, typ, slot.Value)
}

func (s slots) number(name string) (int, bool) {
	n, err := strconv.Atoi(s.value(name))

	return n, err == nil
}

func (s slots) duration(name string) (time.Duration, bool) {
	return parseDuration(s.value(name))
}

func (s slots) title(name string, kinds []media.Kind, pending []actions.Choice) actions.TitleRequest {
	return titleRequest(s.intent.Slots[name], kinds, pending)
}

var durationPart = regexp.MustCompile(`(\d+(?:\.\d+)?)([YMWDHS])`)

// parseDuration reads an ISO 8601 duration as Alexa's AMAZON.DURATION sends it, e.g. "PT1H30M" or "PT90S". M is months before the T and minutes after it; a month counts as 30 days and a year as 365.25.
func parseDuration(s string) (time.Duration, bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if !strings.HasPrefix(s, "P") || len(s) < 3 {
		return 0, false
	}

	date, clock, _ := strings.Cut(s[1:], "T")

	var total time.Duration

	units := map[bool]map[string]time.Duration{
		false: {"Y": 8766 * time.Hour, "M": 720 * time.Hour, "W": 168 * time.Hour, "D": 24 * time.Hour},
		true:  {"H": time.Hour, "M": time.Minute, "S": time.Second},
	}

	for _, part := range []struct {
		text  string
		clock bool
	}{{date, false}, {clock, true}} {
		matches := durationPart.FindAllStringSubmatch(part.text, -1)

		// Anything left over (e.g. "PT10X") makes the whole value unreadable.
		if durationPart.ReplaceAllString(part.text, "") != "" {
			return 0, false
		}

		for _, m := range matches {
			unit, ok := units[part.clock][m[2]]
			if !ok {
				return 0, false
			}

			n, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				return 0, false
			}

			// A duration past time.Duration's range (about 292 years) would wrap around, so it is unreadable too.
			v := n * float64(unit)
			if v >= float64(math.MaxInt64-total) {
				return 0, false
			}

			total += time.Duration(v)
		}
	}

	return total, total > 0
}

// recommendKinds maps a MEDIA_TYPE ID to what to recommend; "watch" and "listen" are Kanzi's generic video and audio recommendations.
var recommendKinds = map[string][]media.Kind{
	"movie":      {media.KindMovie},
	"tvshow":     {media.KindTVShow},
	"episode":    {media.KindEpisode},
	"musicvideo": {media.KindMusicVideo},
	"artist":     {media.KindArtist},
	"album":      {media.KindAlbum},
	"song":       {media.KindSong},
	"video":      {media.KindMovie, media.KindTVShow, media.KindEpisode, media.KindMusicVideo},
	"audio":      {media.KindArtist, media.KindAlbum, media.KindSong},
}

// whatsNewKinds maps a MEDIA_TYPE ID to what "what's new" lists.
var whatsNewKinds = map[string]media.Kind{
	"movie":      media.KindMovie,
	"tvshow":     media.KindTVShow,
	"episode":    media.KindEpisode,
	"musicvideo": media.KindMusicVideo,
	"artist":     media.KindAlbum,
	"album":      media.KindAlbum,
	"song":       media.KindAlbum,
	"audio":      media.KindAlbum,
	"video":      media.KindMovie,
}

var navActions = map[string]media.InputAction{
	"up": media.InputUp, "down": media.InputDown, "left": media.InputLeft, "right": media.InputRight,
	"select": media.InputSelect, "back": media.InputBack, "home": media.InputHome, "info": media.InputInfo,
	"menu": media.InputContextMenu, "pageup": media.InputPageUp, "pagedown": media.InputPageDown, "osd": media.InputOSD,
}

var moves = map[string]media.PictureCommand{
	"up": media.PictureMoveUp, "down": media.PictureMoveDown, "left": media.PictureMoveLeft, "right": media.PictureMoveRight,
}

var sections = map[string]media.Section{
	"movies": media.SectionMovies, "tvshows": media.SectionTVShows, "musicvideos": media.SectionMusicVideos,
	"music": media.SectionMusic, "artists": media.SectionArtists, "albums": media.SectionAlbums,
	"recentmovies": media.SectionRecentMovies, "recentepisodes": media.SectionRecentEpisodes,
	"recentmusicvideos": media.SectionRecentMusicVideos, "recentmusic": media.SectionRecentMusic,
	"favourites": media.SectionFavourites,
}

var powerCommands = map[string]media.SystemCommand{
	"shutdown": media.SystemShutdown, "reboot": media.SystemReboot, "hibernate": media.SystemHibernate,
	"suspend": media.SystemSuspend, "quit": media.SystemQuit,
}

var transports = map[string]media.Transport{
	"pause": media.TransportPause, "resume": media.TransportResume, "stop": media.TransportStop,
	"next": media.TransportNext, "previous": media.TransportPrevious,
}

func minutes(n int) time.Duration { return time.Duration(n) * time.Minute }
