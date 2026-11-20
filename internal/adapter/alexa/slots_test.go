package alexa

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

func TestParseDuration(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected time.Duration
		ok       bool
	}{
		{name: "ShouldParseSeconds", have: "PT90S", expected: 90 * time.Second, ok: true},
		{name: "ShouldParseMinutes", have: "PT10M", expected: 10 * time.Minute, ok: true},
		{name: "ShouldParseHoursAndMinutes", have: "PT1H30M", expected: 90 * time.Minute, ok: true},
		{name: "ShouldParseLowercase", have: "pt2m", expected: 2 * time.Minute, ok: true},
		{name: "ShouldParseFractionalMinutes", have: "PT1.5M", expected: 90 * time.Second, ok: true},
		{name: "ShouldParseDays", have: "P1D", expected: 24 * time.Hour, ok: true},
		{name: "ShouldParseDaysAndHours", have: "P1DT2H", expected: 26 * time.Hour, ok: true},
		{name: "ShouldParseWeeks", have: "P1W", expected: 7 * 24 * time.Hour, ok: true},
		{name: "ShouldParseMonthsAsThirtyDays", have: "P1M", expected: 30 * 24 * time.Hour, ok: true},
		{name: "ShouldTellMonthsFromMinutes", have: "P1MT1M", expected: 30*24*time.Hour + time.Minute, ok: true},
		{name: "ShouldParseZeroPart", have: "PT1H0M30S", expected: time.Hour + 30*time.Second, ok: true},
		{name: "ShouldRejectEmpty", have: ""},
		{name: "ShouldRejectBareP", have: "P"},
		{name: "ShouldRejectBarePT", have: "PT"},
		{name: "ShouldRejectWords", have: "10 minutes"},
		{name: "ShouldRejectUnknownUnit", have: "PT10X"},
		{name: "ShouldRejectMissingP", have: "T10M"},
		{name: "ShouldRejectZero", have: "PT0S"},
		{name: "ShouldRejectQuestionMark", have: "?"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := parseDuration(tc.have)

			if !tc.ok {
				assert.False(t, ok)

				return
			}

			assert.True(t, ok)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

// A duration too long for time.Duration is unreadable, not a wrapped or platform-dependent value.
func TestParseDurationRejectsOverflow(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected time.Duration
		ok       bool
	}{
		{name: "ShouldReject7000Years", have: "P7000Y"},
		{name: "ShouldReject300Years", have: "P300Y"},
		{name: "ShouldRejectRepeatedYearsThatOverflowTogether", have: "P250Y250Y250Y"},
		{name: "ShouldRejectTooManyHours", have: "PT9999999999999H"},
		{name: "ShouldRejectNumberTooLongToParse", have: "P1" + strings.Repeat("0", 400) + "D"},
		{name: "ShouldRejectSecondsJustPastTheLimit", have: "PT9223372036.854775808S"},
		{name: "ShouldReadTheLongestDurationsThatFit", have: "P290Y", expected: 290 * 8766 * time.Hour, ok: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := parseDuration(tc.have)

			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestSlotIDsResolveLocally(t *testing.T) {
	h, _, _ := newHandler(t)

	testCases := []struct {
		name     string
		locale   string
		typ      string
		have     string
		expected string
	}{
		{name: "ShouldResolveSynonymToID", locale: "en-AU", typ: "NAV_ACTION", have: "Context Menu", expected: "menu"},
		{name: "ShouldResolvePhraseToID", locale: "en-AU", typ: "NAV_ACTION", have: "show more", expected: "pagedown"},
		{name: "ShouldResolveInEnGB", locale: "en-GB", typ: "LIBRARY_SECTION", have: "new episodes", expected: "recentepisodes"},
		{name: "ShouldResolveInEnUS", locale: "en-US", typ: "ROTATE_DIRECTION", have: "anti clockwise", expected: "counterclockwise"},
		{name: "ShouldUseValueWithoutAnIDAsItsOwnID", locale: "en-AU", typ: "KODI_ROOM", have: "bedroom", expected: "bedroom"},
		{name: "ShouldFallBackForUnknownLocale", locale: "xx-XX", typ: "NAV_DIRECTION", have: "up", expected: "up"},
		{name: "ShouldNotResolveUnknownValue", locale: "en-AU", typ: "NAV_ACTION", have: "sideways"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, h.Speech.SlotID(tc.locale, tc.typ, tc.have))
		})
	}
}

func TestSlotIDPrefersEntityResolution(t *testing.T) {
	h, _, _ := newHandler(t)

	var s Slot
	require.NoError(t, json.Unmarshal([]byte(`{"name":"NavAction","value":"go down","resolutions":{"resolutionsPerAuthority":[
		{"authority":"amzn1.er-authority.echo-sdk.skill.NAV_ACTION","status":{"code":"ER_SUCCESS_MATCH"},"values":[{"value":{"name":"down","id":"down"}}]}]}}`), &s))

	sl := slots{intent: Intent{Slots: map[string]Slot{"NavAction": s}}, locale: "en-AU", speech: h.Speech}
	assert.Equal(t, "down", sl.id("NavAction", "NAV_ACTION"))
}

func TestSpeechTimesLanguagesAndLists(t *testing.T) {
	h, _, _ := newHandler(t)

	msg := actions.Message{
		ID:        "player.remaining_until",
		Durations: map[string]time.Duration{"remaining": 42 * time.Minute},
		Times:     map[string]time.Time{"end": time.Date(1, 1, 1, 21, 59, 0, 0, time.UTC)},
	}
	assert.Equal(t, "There's 42 minutes left. It'll finish at 9:59 pm.", h.Speech.Render("en-GB", msg))

	sub := actions.Message{ID: "streams.subtitles_language", Languages: map[string]string{"language": "fre"}}
	assert.Equal(t, "Subtitles: French.", h.Speech.Render("en-US", sub), "ISO 639-2 codes are named")

	sub.Languages["language"] = "pt"
	assert.Equal(t, "Subtitles: Portuguese.", h.Speech.Render("en-AU", sub))

	list := actions.Message{ID: "whatsnew.movies", Lists: map[string][]string{"titles": {"Dune", "Alien", "Heat"}}}
	assert.Equal(t, "You have Dune, Alien and Heat to watch.", h.Speech.Render("en-AU", list))

	assert.Equal(t, "15:04", spokenTime("de-DE", "de", time.Date(1, 1, 1, 15, 4, 0, 0, time.UTC)))
	assert.Equal(t, "Französisch", languageName("de-DE", "fre"))
	assert.Equal(t, "xx-nonsense", languageName("en-AU", "xx-nonsense"), "unknown codes are said as they are")
	assert.Equal(t, "A und B", spokenList("de", []string{"A", "B"}))
	assert.Equal(t, "A", spokenList("fr", []string{"A"}))
	assert.Empty(t, spokenList("en", nil))
}

func TestRoomTargeting(t *testing.T) {
	h, lounge, bedroom, rec := newRoomHandler(t)

	serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-in-bedroom/request.json"))
	assert.Empty(t, lounge.Calls())
	assert.Equal(t, []string{"Play movie/4 resume"}, bedroom.Calls())

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "Bedroom", rec.entries[0].Target)
	assert.Equal(t, "bedroom", rec.entries[0].Slots["Room"])
}

func TestRoomInSearchQuery(t *testing.T) {
	h, lounge, bedroom, rec := newRoomHandler(t)

	serveBody(t, h, mustRead(t, "testdata/golden/play-media-in-bedroom/request.json"))
	assert.Empty(t, lounge.Calls())
	assert.Equal(t, []string{"Play movie/4 resume"}, bedroom.Calls())

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "Bedroom", rec.entries[0].Target)
	assert.Equal(t, "hot fuzz in the bedroom", rec.entries[0].Slots["Query"], "the log keeps what was heard")
}

func TestRoomWithoutArticleInSearchQuery(t *testing.T) {
	h, lounge, bedroom, rec := newRoomHandler(t)

	serveBody(t, h, intentRequest(t, "PlayMedia", map[string]slotSpec{"Query": {value: "hot fuzz on bedroom"}}))
	assert.Empty(t, lounge.Calls())
	assert.Equal(t, []string{"Play movie/4 resume"}, bedroom.Calls(), "\"on bedroom\" names the room as \"on the bedroom\" does")

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "Bedroom", rec.entries[0].Target)
}

func TestRoomInSearchQueryUnknownRoom(t *testing.T) {
	h, _, bedroom, rec := newRoomHandler(t)

	req := mustRead(t, "testdata/golden/play-media-in-bedroom/request.json")
	req = bytes.Replace(req, []byte(`"value": "hot fuzz in the bedroom"`), []byte(`"value": "hot fuzz in the attic"`), 1)

	serveBody(t, h, req)
	assert.Empty(t, bedroom.Calls())

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "Lounge", rec.entries[0].Target, "a phrase naming no room stays part of the title")
	assert.Contains(t, rec.entries[0].Speech, "hot fuzz in the attic")
}

func TestRoomInSearchQueryLookupFails(t *testing.T) {
	h, lounge, bedroom, rec := newRoomHandler(t)

	req := mustRead(t, "testdata/golden/play-media-in-bedroom/request.json")
	req = bytes.Replace(req, []byte(`"value": "hot fuzz in the bedroom"`), []byte(`"value": "hot fuzz in the cellar"`), 1)

	serveBody(t, h, req)
	assert.Empty(t, lounge.Calls(), "a room that can't be looked up isn't guessed at")
	assert.Empty(t, bedroom.Calls())

	require.Len(t, rec.entries, 1)
	require.ErrorIs(t, rec.entries[0].Err, errRoomLookup)
	assert.Empty(t, rec.entries[0].Target)
}

func TestRoomGroup(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
		ok       bool
	}{
		{name: "ShouldReadAlternativesAndArticle", have: "play {Movie} [(in|on) the {Room}]", expected: "(in|on) the", ok: true},
		{name: "ShouldReadOptionalArticle", have: "play {Movie} [(in|on) [the] {Room}]", expected: "(in|on) [the]", ok: true},
		{name: "ShouldReadGermanAlternatives", have: "play [the] {Movie} [(im|in der) {Room}]", expected: "(im|in der)", ok: true},
		{name: "ShouldRejectRoomWithoutPhrase", have: "play {Movie} [{Room}]"},
		{name: "ShouldRejectRoomOutsideOptionalPart", have: "{Command} (in|on) the {Room}"},
		{name: "ShouldRejectRoomNotAtTheEnd", have: "play {Movie} [in the {Room}] please"},
		{name: "ShouldRejectUnopenedBracket", have: "play {Movie} in the {Room}]"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := roomGroup(tc.have)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestRoomSplits(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected []RoomSplit
	}{
		// The bare "in" and "on" also read the article as part of the room; no room is called "the lounge room", so the caller moves on.
		{name: "ShouldSplitWithAndWithoutArticleAfterIn", locale: "en-AU", have: "frozen in the lounge room", expected: []RoomSplit{{"frozen", "lounge room"}, {"frozen", "the lounge room"}}},
		{name: "ShouldSplitWithAndWithoutArticleAfterOn", locale: "en-AU", have: "frozen on the TV", expected: []RoomSplit{{"frozen", "TV"}, {"frozen", "the TV"}}},
		{name: "ShouldSplitRoomWithoutArticle", locale: "en-AU", have: "the martian on TV", expected: []RoomSplit{{"the martian", "TV"}}},
		// Without a room called Paris, the caller searches the whole query.
		{name: "ShouldSplitPlaceInTitle", locale: "en-AU", have: "murder in paris", expected: []RoomSplit{{"murder", "paris"}}},
		{name: "ShouldOfferEverySplitWhenTitleHasIn", locale: "en-AU", have: "the man in the iron mask in the lounge", expected: []RoomSplit{{"the man", "iron mask in the lounge"}, {"the man", "the iron mask in the lounge"}, {"the man in the iron mask", "lounge"}, {"the man in the iron mask", "the lounge"}}},
		{name: "ShouldNotSplitTitleWithoutRoom", locale: "en-AU", have: "frozen"},
		{name: "ShouldNotSplitRoomWithoutTitle", locale: "en-AU", have: "in the lounge"},
		{name: "ShouldSplitArticleAloneAsRoom", locale: "en-AU", have: "frozen in the", expected: []RoomSplit{{"frozen", "the"}}},
		{name: "ShouldSplitImInDeDE", locale: "de-DE", have: "die eiskönigin im wohnzimmer", expected: []RoomSplit{{"die eiskönigin", "wohnzimmer"}}},
		{name: "ShouldSplitInDerInDeDE", locale: "de-DE", have: "die eiskönigin in der küche", expected: []RoomSplit{{"die eiskönigin", "küche"}}},
		{name: "ShouldSplitAuInFrFR", locale: "fr-FR", have: "la reine des neiges au salon", expected: []RoomSplit{{"la reine des neiges", "salon"}}},
		{name: "ShouldSplitEnElInEsES", locale: "es-ES", have: "frozen en el salón", expected: []RoomSplit{{"frozen", "salón"}}},
		{name: "ShouldSplitNelInItIT", locale: "it-IT", have: "frozen nel salotto", expected: []RoomSplit{{"frozen", "salotto"}}},
		{name: "ShouldSplitAsEnglishInUnknownLocale", locale: "xx-XX", have: "frozen in the lounge", expected: []RoomSplit{{"frozen", "lounge"}, {"frozen", "the lounge"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.RoomSplits(tc.locale, tc.have))
		})
	}
}

func TestReinterpret(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	type want struct {
		intent string
		slots  map[string]string
	}

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected want
	}{
		{name: "ShouldReadNextEpisode", locale: "en-AU", have: "the next episode of bluey", expected: want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
		{name: "ShouldKeepRoomInShow", locale: "en-AU", have: "next episode of bluey in the lounge", expected: want{"WatchNextEpisode", map[string]string{"Show": "bluey in the lounge"}}},
		{name: "ShouldReadWhereILeftOffAsNextEpisode", locale: "en-AU", have: "bluey from where I left off", expected: want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
		{name: "ShouldReadLatestEpisodeInAnyCase", locale: "en-AU", have: "The Latest Episode of Bluey", expected: want{"WatchLatestEpisode", map[string]string{"Show": "Bluey"}}},
		{name: "ShouldReadFirstEpisodeFrom1st", locale: "en-AU", have: "the 1st episode of bluey", expected: want{"WatchFirstEpisode", map[string]string{"Show": "bluey"}}},
		{name: "ShouldReadFirstEpisodeFromFirst", locale: "en-AU", have: "the first episode of bluey", expected: want{"WatchFirstEpisode", map[string]string{"Show": "bluey"}}},
		{name: "ShouldReadRandomEpisode", locale: "en-AU", have: "a random episode of bluey", expected: want{"WatchRandomEpisode", map[string]string{"Show": "bluey"}}},
		{name: "ShouldReadSeasonAndEpisode", locale: "en-AU", have: "season 2 episode 4 of severance", expected: want{"WatchEpisode", map[string]string{"Show": "severance", "Season": "2", "Episode": "4"}}},
		{name: "ShouldKeepSeasonAndEpisodeInWordsAsPlayMedia", locale: "en-AU", have: "season two episode four of severance", expected: want{"PlayMedia", map[string]string{"Query": "season two episode four of severance"}}},
		{name: "ShouldKeepPlainTitleAsPlayMedia", locale: "en-AU", have: "bluey", expected: want{"PlayMedia", map[string]string{"Query": "bluey"}}},
		{name: "ShouldKeepPhraseWithoutShowAsPlayMedia", locale: "en-AU", have: "the next episode of", expected: want{"PlayMedia", map[string]string{"Query": "the next episode of"}}},
		{name: "ShouldKeepTitleWithInAsPlayMedia", locale: "en-AU", have: "the man in the iron mask", expected: want{"PlayMedia", map[string]string{"Query": "the man in the iron mask"}}},
		{name: "ShouldReadNextEpisodeInDeDE", locale: "de-DE", have: "die nächste folge von bluey", expected: want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
		// Amazon's es-US model takes the accents off what it heard, so "álbum", "capítulo" and "tráiler" arrive bare.
		{name: "ShouldReadAlbumWithoutAccentInEsUS", locale: "es-US", have: "el album pies descalzos", expected: want{"ListenToAlbum", map[string]string{"Album": "pies descalzos"}}},
		{name: "ShouldReadCapituloWithoutAccentInEsUS", locale: "es-US", have: "la casa de papel capitulo 3 de la temporada 2", expected: want{"WatchEpisode", map[string]string{"Show": "la casa de papel", "Episode": "3", "Season": "2"}}}, //nolint:misspell // Spanish "capítulo" as es-US hears it.
		{name: "ShouldReadNachsteWithoutUmlautInDeDE", locale: "de-DE", have: "die nachste folge von bluey", expected: want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
		// As Amazon wrote them: numbers as digits, German compounds split, a word hyphenated, and playlists.
		{name: "ShouldReadDigitOneAsRandomEpisodeInEsUS", locale: "es-US", have: "1 capitulo de la casa de papel", expected: want{"WatchRandomEpisode", map[string]string{"Show": "la casa de papel"}}},               //nolint:misspell // Spanish "capítulo" as es-US hears it.
		{name: "ShouldReadSuperscriptOrdinalAsFirstEpisodeInEsUS", locale: "es-US", have: "el 1.ᵉʳ capitulo de la casa de papel", expected: want{"WatchFirstEpisode", map[string]string{"Show": "la casa de papel"}}}, //nolint:misspell // Spanish "capítulo" as es-US hears it.
		{name: "ShouldReadDigitOneAsFirstEpisodeInArSA", locale: "ar-SA", have: "1 حلقة من مسلسل باب الحارة", expected: want{"WatchFirstEpisode", map[string]string{"Show": "باب الحارة"}}},
		{name: "ShouldReadSplitCompoundInDeDE", locale: "de-DE", have: "mir das musik video zu bohemian rhapsody", expected: want{"WatchMusicVideo", map[string]string{"MusicVideo": "bohemian rhapsody"}}},
		{name: "ShouldReadHyphenatedWordInEsUS", locale: "es-US", have: "to-dos mis videoclips en aleatorio", expected: want{"ShuffleMusicVideos", map[string]string{}}},
		{name: "ShouldReadPlaylistInEsUS", locale: "es-US", have: "la lista de musica viaje", expected: want{"PlayPlaylist", map[string]string{"Playlist": "viaje"}}},
		{name: "ShouldReadPlaylistInHiIN", locale: "hi-IN", have: "म्यूज़िक प्लेलिस्ट सफ़र", expected: want{"PlayPlaylist", map[string]string{"Playlist": "सफ़र"}}},
		{name: "ShouldReadAsEnglishInUnknownLocale", locale: "xx-XX", have: "the next episode of bluey", expected: want{"WatchNextEpisode", map[string]string{"Show": "bluey"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}}
			actual := speech.Reinterpret(tc.locale, in)[0]

			values := map[string]string{}
			for name, slot := range actual.Slots {
				values[name] = slot.Value
			}

			assert.Equal(t, tc.expected, want{actual.Name, values})
		})
	}
}

func TestEpisodeInSearchQuery(t *testing.T) {
	t.Run("ShouldKeepUnknownRoomAsPartOfShow", func(t *testing.T) {
		h, lounge, bedroom, rec := newRoomHandler(t)

		req := mustRead(t, "testdata/golden/play-media-latest-episode-in-bedroom/request.json")
		req = bytes.Replace(req, []byte(`in the bedroom"`), []byte(`in the attic"`), 1)

		serveBody(t, h, req)
		assert.Empty(t, bedroom.Calls())
		assert.Empty(t, lounge.Calls(), "no show is called \"severance in the attic\"")

		require.Len(t, rec.entries, 1)
		assert.Equal(t, "Lounge", rec.entries[0].Target)
		assert.Equal(t, "the latest episode of severance in the attic", rec.entries[0].Slots["Query"], "the log keeps what was heard")
	})

	t.Run("ShouldDeclineWithoutTV", func(t *testing.T) {
		h, backend := withDisabled(t, false, media.ContentTV)
		res := serveBody(t, h, mustRead(t, "testdata/golden/play-media-next-episode/request.json"))
		assert.Contains(t, string(res), "This skill is set up without that kind of title.")
		assert.Empty(t, backend.Calls())
	})
}

func TestFollowupRoundTrip(t *testing.T) {
	h, backend, rec := newHandler(t)

	ask := serveBody(t, h, mustRead(t, "testdata/golden/shutdown-asks-first/request.json"))

	var resp ResponseEnvelope
	require.NoError(t, json.Unmarshal(ask, &resp))
	require.NotNil(t, resp.SessionAttributes[sessionFollowup])
	assert.Empty(t, backend.Calls(), "nothing happens until the user confirms")

	var yes RequestEnvelope
	require.NoError(t, json.Unmarshal(mustRead(t, "testdata/golden/shutdown-confirmed/request.json"), &yes))
	yes.Session.Attributes = resp.SessionAttributes

	body, err := json.Marshal(yes)
	require.NoError(t, err)

	serveBody(t, h, body)
	assert.Equal(t, []string{"System shutdown"}, backend.Calls())

	require.Len(t, rec.entries, 2)
	assert.Equal(t, "asked", rec.entries[0].Outcome)
}

func TestFoldLatin(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldFoldAccentAndCase", have: "Álbum", expected: "album"},
		{name: "ShouldFoldAcuteAccent", have: "capítulo", expected: "capitulo"}, //nolint:misspell // Spanish "capítulo" without its accent.
		{name: "ShouldFoldUmlaut", have: "nächste", expected: "nachste"},
		{name: "ShouldLeaveJapaneseAlone", have: "が", expected: "が"},
		{name: "ShouldLeaveDevanagariAlone", have: "ज़िंदगी", expected: "ज़िंदगी"},
		{name: "ShouldFoldCedilla", have: "Ça va", expected: "ca va"},
		{name: "ShouldLeaveArabicAlone", have: "تريلر", expected: "تريلر"},
		{name: "ShouldFoldDotBelow", have: "Ṭrailer", expected: "trailer"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, foldLatin(tc.have))
		})
	}
}

func TestUnknownRoomIsReadAsTheEndOfTheTitle(t *testing.T) {
	murder := media.Item{Kind: media.KindMovie, ID: 50, Title: "Murder in Paris", Year: 2023}
	live := media.Item{Kind: media.KindMovie, ID: 51, Title: "Live on Broadway", Year: 2019}

	testCases := []struct {
		name   string
		intent string
		slots  map[string]slotSpec
		played []string
		said   string
	}{
		{name: "ShouldReadRoomCutOffAfterInAsPartOfTitle", intent: "WatchMovie", slots: map[string]slotSpec{"Movie": {value: "murder"}, "Room": {value: "paris"}}, played: []string{"Play movie/50 resume"}},
		{name: "ShouldReadRoomCutOffAfterOnAsPartOfTitle", intent: "WatchMovie", slots: map[string]slotSpec{"Movie": {value: "live"}, "Room": {value: "broadway"}}, played: []string{"Play movie/51 resume"}},
		{name: "ShouldSayUnknownRoomWhenNoTitleMatchesEither", intent: "WatchMovie", slots: map[string]slotSpec{"Movie": {value: "dune"}, "Room": {value: "den"}}, said: "error.unknown_room"},
		{name: "ShouldStillPlayInKnownRoom", intent: "WatchMovie", slots: map[string]slotSpec{"Movie": {value: "hot fuzz"}, "Room": {value: "bedroom"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, lounge, bedroom, rec := newRoomHandlerWith(t, murder, live)

			serveBody(t, h, intentRequest(t, tc.intent, tc.slots))
			require.Len(t, rec.entries, 1)

			switch {
			case tc.said != "":
				assert.Empty(t, lounge.Calls())
				assert.Empty(t, bedroom.Calls())
				assert.Equal(t, "I don't know a room called den.", rec.entries[0].Speech)
			case tc.played == nil:
				assert.Empty(t, lounge.Calls())
				assert.Equal(t, []string{"Play movie/4 resume"}, bedroom.Calls())
			default:
				assert.Equal(t, tc.played, lounge.Calls(), "the title plays on this Echo's Kodi instance")
				assert.Empty(t, bedroom.Calls())
				assert.Equal(t, "Lounge", rec.entries[0].Target)
			}
		})
	}
}

func TestRoomAsTitle(t *testing.T) {
	speech := sharedSpeech(t)

	readings := speech.RoomAsTitle("en-AU", Intent{Name: "WatchMovie", Slots: map[string]Slot{"Movie": {Name: "Movie", Value: "murder"}, "Room": {Name: "Room", Value: "paris"}}})

	var titles []string
	for _, r := range readings {
		assert.NotContains(t, r.Slots, "Room")
		titles = append(titles, r.Slots["Movie"].Value)
	}

	assert.Equal(t, []string{"murder in the paris", "murder on the paris", "murder in paris", "murder on paris"}, titles)

	assert.Nil(t, speech.RoomAsTitle("en-AU", Intent{Name: "WatchMovie", Slots: map[string]Slot{"Room": {Name: "Room", Value: "paris"}}}), "no title to put it on")
	assert.Nil(t, speech.RoomAsTitle("en-AU", Intent{Name: "Pause", Slots: map[string]Slot{"Room": {Name: "Room", Value: "paris"}}}), "an intent with no title before its room")
	assert.NotEmpty(t, speech.RoomAsTitle("xx-XX", Intent{Name: "WatchMovie", Slots: map[string]Slot{"Movie": {Name: "Movie", Value: "murder"}, "Room": {Name: "Room", Value: "paris"}}}), "an unknown locale reads as English")
}

func TestRoomAsTitleSkipsRoomsThatAreNotAtTheEnd(t *testing.T) {
	// No shipped language puts some rooms after the title and others before it or inside a phrase, but one could.
	speech := &Speech{
		languages: map[string]string{"xx-XX": "en"},
		roomPhrases: map[string][]roomPhrase{"xx-XX": {
			{before: []string{"in"}, first: true},
			{before: []string{"by"}, after: []string{"please"}},
			{after: []string{"room"}},
			{before: []string{"at"}},
		}},
		roomTitles: map[string]map[string][]string{"xx-XX": {"WatchMovie": {"Movie"}}},
	}

	readings := speech.RoomAsTitle("xx-XX", Intent{Name: "WatchMovie", Slots: map[string]Slot{"Movie": {Name: "Movie", Value: "murder"}, "Room": {Name: "Room", Value: "paris"}}})
	require.Len(t, readings, 1)
	assert.Equal(t, "murder at paris", readings[0].Slots["Movie"].Value)
}
