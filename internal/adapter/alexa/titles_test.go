package alexa

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

func shippedSpeech(t *testing.T) *Speech {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return speech
}

func TestReinterpretFilmAndMusic(t *testing.T) {
	speech := shippedSpeech(t)

	testCases := []struct {
		name   string
		locale string
		have   string
		intent string
		slots  map[string]string
	}{
		{name: "ShouldReadMovieInEnAU", locale: "en-AU", have: "the movie frozen", intent: "WatchMovie", slots: map[string]string{"Movie": "frozen"}},
		{name: "ShouldReadMusicByArtistInEnAU", locale: "en-AU", have: "music by pink floyd", intent: "ListenToArtist", slots: map[string]string{"Artist": "pink floyd"}},
		{name: "ShouldReadSomethingByArtistInEnAU", locale: "en-AU", have: "something by pink floyd", intent: "ListenToArtist", slots: map[string]string{"Artist": "pink floyd"}},
		{name: "ShouldReadAlbumByArtistInEnAU", locale: "en-AU", have: "the album the wall by pink floyd", intent: "ListenToAlbum", slots: map[string]string{"Album": "the wall", "Artist": "pink floyd"}},
		{name: "ShouldReadTrailerInEnAU", locale: "en-AU", have: "frozen trailer", intent: "WatchMovieTrailer", slots: map[string]string{"Movie": "frozen"}},
		{name: "ShouldReadAllMusicAsPartyModeInEnAU", locale: "en-AU", have: "all music", intent: "PartyMode", slots: map[string]string{}},
		{name: "ShouldKeepTitleWithByAsPlayMediaInEnAU", locale: "en-AU", have: "stand by me", intent: "PlayMedia", slots: map[string]string{"Query": "stand by me"}},
		{name: "ShouldReadMovieInDeDE", locale: "de-DE", have: "den film die eiskönigin", intent: "WatchMovie", slots: map[string]string{"Movie": "die eiskönigin"}},
		{name: "ShouldReadSomethingByArtistInDeDE", locale: "de-DE", have: "etwas von pink floyd", intent: "ListenToArtist", slots: map[string]string{"Artist": "pink floyd"}},
		{name: "ShouldKeepTitleWithVonAsPlayMediaInDeDE", locale: "de-DE", have: "ein bisschen frieden von nicole", intent: "PlayMedia", slots: map[string]string{"Query": "ein bisschen frieden von nicole"}},
		{name: "ShouldReadMovieInFrFR", locale: "fr-FR", have: "le film la reine des neiges", intent: "WatchMovie", slots: map[string]string{"Movie": "la reine des neiges"}},
		{name: "ShouldReadSomethingByArtistInFrFR", locale: "fr-FR", have: "quelque chose de daft punk", intent: "ListenToArtist", slots: map[string]string{"Artist": "daft punk"}},
		{name: "ShouldReadMovieInEsES", locale: "es-ES", have: "la película frozen", intent: "WatchMovie", slots: map[string]string{"Movie": "frozen"}},
		{name: "ShouldReadSomethingByArtistInEsES", locale: "es-ES", have: "algo de shakira", intent: "ListenToArtist", slots: map[string]string{"Artist": "shakira"}},
		{name: "ShouldReadMovieInItIT", locale: "it-IT", have: "il film frozen", intent: "WatchMovie", slots: map[string]string{"Movie": "frozen"}},
		{name: "ShouldReadSomethingByArtistInItIT", locale: "it-IT", have: "qualcosa di vasco rossi", intent: "ListenToArtist", slots: map[string]string{"Artist": "vasco rossi"}},
		{name: "ShouldReadMovieInPtBR", locale: "pt-BR", have: "o filme cidade de deus", intent: "WatchMovie", slots: map[string]string{"Movie": "cidade de deus"}},
		{name: "ShouldReadSomethingByArtistInPtBR", locale: "pt-BR", have: "alguma coisa do caetano veloso", intent: "ListenToArtist", slots: map[string]string{"Artist": "caetano veloso"}},
		{name: "ShouldReadMovieInNlNL", locale: "nl-NL", have: "de film flodder", intent: "WatchMovie", slots: map[string]string{"Movie": "flodder"}},
		{name: "ShouldReadSomethingByArtistInNlNL", locale: "nl-NL", have: "iets van golden earring", intent: "ListenToArtist", slots: map[string]string{"Artist": "golden earring"}},
		{name: "ShouldReadMovieInArSA", locale: "ar-SA", have: "فيلم الرسالة", intent: "WatchMovie", slots: map[string]string{"Movie": "الرسالة"}},
		{name: "ShouldReadMusicByArtistInArSA", locale: "ar-SA", have: "موسيقى حق عمرو دياب", intent: "ListenToArtist", slots: map[string]string{"Artist": "عمرو دياب"}},
		{name: "ShouldReadSomethingByArtistInArSA", locale: "ar-SA", have: "شي من عمرو دياب", intent: "ListenToArtist", slots: map[string]string{"Artist": "عمرو دياب"}},
		{name: "ShouldReadMovieInHiIN", locale: "hi-IN", have: "शोले फ़िल्म", intent: "WatchMovie", slots: map[string]string{"Movie": "शोले"}},
		{name: "ShouldReadSomethingByArtistInHiIN", locale: "hi-IN", have: "अरिजीत सिंह का कुछ", intent: "ListenToArtist", slots: map[string]string{"Artist": "अरिजीत सिंह"}},
		{name: "ShouldKeepArtistAndSongAsPlayMediaInHiIN", locale: "hi-IN", have: "अरिजीत सिंह का तुम ही हो", intent: "PlayMedia", slots: map[string]string{"Query": "अरिजीत सिंह का तुम ही हो"}},
		{name: "ShouldReadMovieInJaJP", locale: "ja-JP", have: "映画の千と千尋の神隠し", intent: "WatchMovie", slots: map[string]string{"Movie": "千と千尋の神隠し"}},
		{name: "ShouldReadSongsSungByArtistInJaJP", locale: "ja-JP", have: "宇多田ヒカルが歌う曲", intent: "ListenToArtist", slots: map[string]string{"Artist": "宇多田ヒカル"}},
		{name: "ShouldReadSomethingByArtistInJaJP", locale: "ja-JP", have: "宇多田ヒカルの何か", intent: "ListenToArtist", slots: map[string]string{"Artist": "宇多田ヒカル"}},
		{name: "ShouldKeepArtistAndSongAsPlayMediaInJaJP", locale: "ja-JP", have: "宇多田ヒカルの曲のファーストラヴ", intent: "PlayMedia", slots: map[string]string{"Query": "宇多田ヒカルの曲のファーストラヴ"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}}
			actual := speech.Reinterpret(tc.locale, in)[0]

			values := map[string]string{}
			for name, slot := range actual.Slots {
				values[name] = slot.Value
			}

			assert.Equal(t, tc.intent, actual.Name)
			assert.Equal(t, tc.slots, values)
		})
	}
}

func TestReinterpretEndsWithTheQuery(t *testing.T) {
	speech := shippedSpeech(t)

	in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: "the album stand by me by ben e king"}}}
	readings := speech.Reinterpret("en-AU", in)

	require.Len(t, readings, maxQueryForms+1+maxLooseForms)
	assert.Equal(t, "ListenToAlbum", readings[0].Name)
	assert.Equal(t, "stand", readings[0].Slots["Album"].Value, "the form with the most words comes first")
	assert.Equal(t, in, readings[maxQueryForms])
	assert.Equal(t, "ListenToSongOrAlbum", readings[len(readings)-1].Name, "a loose reading comes after the query")

	seen := map[string]bool{}
	for _, r := range readings {
		key := r.Name + "|" + r.Slots["Album"].Value + "|" + r.Slots["Artist"].Value
		assert.False(t, seen[key], "readings are distinct: %s", key)
		seen[key] = true
	}
}

// TestReinterpretSplitsTitleAndArtistLast checks that a query that is only a title and an artist, as Amazon hears "play bohemian rhapsody by queen" in every locale tried, is read as that song or album once the query itself finds nothing, and that "stand by me" is still searched as it stands first.
func TestReinterpretSplitsTitleAndArtistLast(t *testing.T) {
	speech := shippedSpeech(t)

	testCases := []struct {
		name   string
		locale string
		have   string
		title  string
		artist string
	}{
		{name: "ShouldSplitByInEnUS", locale: "en-US", have: "bohemian rhapsody by queen", title: "bohemian rhapsody", artist: "queen"},
		{name: "ShouldSplitTitleWithByAfterSearchingItInEnAU", locale: "en-AU", have: "stand by me", title: "stand", artist: "me"},
		{name: "ShouldSplitDeInEsUS", locale: "es-US", have: "ojos asi de shakira", title: "ojos asi", artist: "shakira"},
		{name: "ShouldSplitDeInPtBR", locale: "pt-BR", have: "envolver de anitta", title: "envolver", artist: "anitta"},
		{name: "ShouldSplitVanInNlNL", locale: "nl-NL", have: "zij van marco borsato", title: "zij", artist: "marco borsato"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}}
			readings := speech.Reinterpret(tc.locale, in)

			require.Len(t, readings, 2)
			assert.Equal(t, in, readings[0], "the query is searched first")
			assert.Equal(t, Intent{Name: "ListenToSongOrAlbum", Slots: map[string]Slot{
				"Title":  {Name: "Title", Value: tc.title},
				"Artist": {Name: "Artist", Value: tc.artist},
			}}, readings[1])
		})
	}

	only := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: "frozen"}}}
	assert.Equal(t, []Intent{only}, speech.Reinterpret("en-AU", only), "a query with no artist in it is only itself")
}

// TestReinterpretCommands checks that a query that is a command's sample after PlayMedia's carrier, as Amazon heard "pon el volumen a 30", "metti in pausa la tv" and "नई फ़िल्में ढूँढो", is read as that command, and that a command whose slot takes any words is left alone, as a film called In Bruges isn't AudioStream's "play in {Language}".
func TestReinterpretCommands(t *testing.T) {
	speech := shippedSpeech(t)

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected Intent
	}{
		{name: "ShouldReadVolumeCommandInEsUS", locale: "es-US", have: "el volumen a 30", expected: Intent{Name: "VolumeSet", Slots: map[string]Slot{"Level": {Name: "Level", Value: "30"}}}},
		{name: "ShouldReadPauseCommandInItIT", locale: "it-IT", have: "in pausa la tv", expected: Intent{Name: "AMAZON.PauseIntent", Slots: map[string]Slot{}}},
		{name: "ShouldReadUpdateLibraryCommandInHiIN", locale: "hi-IN", have: "नई फ़िल्में", expected: Intent{Name: "UpdateLibrary", Slots: map[string]Slot{}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}}
			assert.Equal(t, []Intent{tc.expected, in}, speech.Reinterpret(tc.locale, in))
		})
	}

	bruges := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: "in bruges"}}}
	assert.Equal(t, []Intent{bruges}, speech.Reinterpret("en-AU", bruges))
}

func TestStripKind(t *testing.T) {
	speech := shippedSpeech(t)

	type want struct {
		title string
		kind  media.Kind
		ok    bool
	}

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected want
	}{
		{name: "ShouldStripLeadingMovie", locale: "en-AU", have: "the movie frozen", expected: want{"frozen", media.KindMovie, true}},
		{name: "ShouldStripTrailingMovieInAnyCase", locale: "en-AU", have: "Frozen Movie", expected: want{"Frozen", media.KindMovie, true}},
		{name: "ShouldKeepArticleInTitle", locale: "en-AU", have: "the movie the wall", expected: want{"the wall", media.KindMovie, true}},
		{name: "ShouldStripTrailingAlbum", locale: "en-AU", have: "the wall album", expected: want{"the wall", media.KindAlbum, true}},
		{name: "ShouldStripSpokenLettersTVShow", locale: "en-AU", have: "bluey t. v. show", expected: want{"bluey", media.KindTVShow, true}},
		{name: "ShouldStripLeadingBand", locale: "en-AU", have: "the band queen", expected: want{"queen", media.KindArtist, true}},
		{name: "ShouldStripTrailingMusicVideo", locale: "en-AU", have: "thriller music video", expected: want{"thriller", media.KindMusicVideo, true}},
		{name: "ShouldNotStripKindWordAlone", locale: "en-AU", have: "movie"},
		{name: "ShouldNotStripKindWordWithArticleAlone", locale: "en-AU", have: "the movie"},
		{name: "ShouldNotStripTitleWithoutKind", locale: "en-AU", have: "frozen"},
		{name: "ShouldNotStripEmptyTitle", locale: "en-AU"},
		{name: "ShouldStripLeadingFilmInDeDE", locale: "de-DE", have: "der film die eiskönigin", expected: want{"die eiskönigin", media.KindMovie, true}},
		{name: "ShouldStripTrailingFilmInFrFR", locale: "fr-FR", have: "la reine des neiges le film", expected: want{"la reine des neiges", media.KindMovie, true}},
		{name: "ShouldStripLeadingFilmInFrFR", locale: "fr-FR", have: "le film la reine des neiges", expected: want{"la reine des neiges", media.KindMovie, true}},
		{name: "ShouldStripLeadingSongInEsES", locale: "es-ES", have: "la canción despacito", expected: want{"despacito", media.KindSong, true}},
		{name: "ShouldStripTrailingFilmInItIT", locale: "it-IT", have: "la vita è bella il film", expected: want{"la vita è bella", media.KindMovie, true}},
		{name: "ShouldStripLeadingAlbumInPtBR", locale: "pt-BR", have: "o álbum tropicália", expected: want{"tropicália", media.KindAlbum, true}},
		{name: "ShouldStripLeadingProgrammaInNlNL", locale: "nl-NL", have: "het programma flikken", expected: want{"flikken", media.KindTVShow, true}},
		{name: "ShouldStripLeadingFilmInArSA", locale: "ar-SA", have: "الفيلم الرسالة", expected: want{"الرسالة", media.KindMovie, true}},
		{name: "ShouldStripLeadingSeriesInArSA", locale: "ar-SA", have: "مسلسل طاش ما طاش", expected: want{"طاش ما طاش", media.KindTVShow, true}},
		{name: "ShouldStripTrailingFilmInHiIN", locale: "hi-IN", have: "शोले फ़िल्म", expected: want{"शोले", media.KindMovie, true}},
		{name: "ShouldStripTrailingFilmWithPostpositionInHiIN", locale: "hi-IN", have: "शोले फ़िल्म को", expected: want{"शोले", media.KindMovie, true}},
		{name: "ShouldStripLeadingSongInHiIN", locale: "hi-IN", have: "गाना तुम ही हो", expected: want{"तुम ही हो", media.KindSong, true}},
		{name: "ShouldStripTrailingMovieInJaJP", locale: "ja-JP", have: "千と千尋の神隠しの映画", expected: want{"千と千尋の神隠し", media.KindMovie, true}},
		{name: "ShouldStripLeadingAlbumInJaJP", locale: "ja-JP", have: "アルバムのファーストラヴ", expected: want{"ファーストラヴ", media.KindAlbum, true}},
		{name: "ShouldStripAsEnglishInUnknownLocale", locale: "xx-XX", have: "frozen movie", expected: want{"frozen", media.KindMovie, true}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			title, kind, ok := speech.StripKind(tc.locale, tc.have)
			assert.Equal(t, tc.expected, want{title, kind, ok})
		})
	}
}

func TestGeneric(t *testing.T) {
	speech := shippedSpeech(t)

	type want struct{ generic, bare bool }

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected want
	}{
		{name: "ShouldReadMusicAsBareGeneric", locale: "en-AU", have: "music", expected: want{true, true}},
		{name: "ShouldReadSomeSongsAsBareGenericInAnyCase", locale: "en-AU", have: "Some Songs", expected: want{true, true}},
		{name: "ShouldReadSomethingAsGenericButNotBare", locale: "en-AU", have: "something", expected: want{true, false}},
		{name: "ShouldNotReadTitleAsGeneric", locale: "en-AU", have: "bohemian rhapsody", expected: want{false, false}},
		{name: "ShouldReadEtwasMusikAsBareGenericInDeDE", locale: "de-DE", have: "etwas musik", expected: want{true, true}},
		{name: "ShouldReadEtwasAsGenericButNotBareInDeDE", locale: "de-DE", have: "etwas", expected: want{true, false}},
		{name: "ShouldReadDesChansonsAsBareGenericInFrFR", locale: "fr-FR", have: "des chansons", expected: want{true, true}},
		{name: "ShouldReadDesTounesAsBareGenericInFrCA", locale: "fr-CA", have: "des tounes", expected: want{true, true}},
		{name: "ShouldReadAlgoDeMusicaAsBareGenericInEsMX", locale: "es-MX", have: "algo de música", expected: want{true, true}},
		{name: "ShouldReadUnPoDiMusicaAsBareGenericInItIT", locale: "it-IT", have: "un po' di musica", expected: want{true, true}},
		{name: "ShouldReadUmasMusicasAsBareGenericInPtBR", locale: "pt-BR", have: "umas músicas", expected: want{true, true}},
		{name: "ShouldReadWatNummersAsBareGenericInNlNL", locale: "nl-NL", have: "wat nummers", expected: want{true, true}},
		{name: "ShouldReadSongsAsBareGenericInArSA", locale: "ar-SA", have: "اغاني", expected: want{true, true}},
		{name: "ShouldReadSomethingAsGenericButNotBareInArSA", locale: "ar-SA", have: "شي", expected: want{true, false}},
		{name: "ShouldReadMusicAsBareGenericInHiIN", locale: "hi-IN", have: "संगीत", expected: want{true, true}},
		{name: "ShouldReadSomethingAsGenericButNotBareInHiIN", locale: "hi-IN", have: "कुछ", expected: want{true, false}},
		{name: "ShouldReadSongAsBareGenericInJaJP", locale: "ja-JP", have: "歌", expected: want{true, true}},
		{name: "ShouldReadAsEnglishInUnknownLocale", locale: "xx-XX", have: "music", expected: want{true, true}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			generic, bare := speech.Generic(tc.locale, tc.have)
			assert.Equal(t, tc.expected, want{generic, bare})
		})
	}
}

func TestReroute(t *testing.T) {
	speech := shippedSpeech(t)

	slot := func(name, value string) Slot { return Slot{Name: name, Value: value} }

	testCases := []struct {
		name     string
		have     Intent
		expected Intent
	}{
		{name: "ShouldRerouteMusicByAnArtist", have: Intent{Name: "ListenToSongOrAlbum", Slots: map[string]Slot{"Title": slot("Title", "music"), "Artist": slot("Artist", "queen"), "Room": slot("Room", "")}}, expected: Intent{Name: "ListenToArtist", Slots: map[string]Slot{"Artist": slot("Artist", "queen"), "Room": slot("Room", "")}}},
		{name: "ShouldKeepASongCalledSomething", have: Intent{Name: "ListenToSong", Slots: map[string]Slot{"Song": slot("Song", "something")}}, expected: Intent{Name: "ListenToSong", Slots: map[string]Slot{"Song": slot("Song", "something")}}},
		{name: "ShouldKeepSongsFromAnAlbum", have: Intent{Name: "ListenToSong", Slots: map[string]Slot{"Song": slot("Song", "songs"), "Album": slot("Album", "innuendo")}}, expected: Intent{Name: "ListenToSong", Slots: map[string]Slot{"Song": slot("Song", "songs"), "Album": slot("Album", "innuendo")}}},
		{name: "ShouldRerouteMusicInARoomToPartyMode", have: Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": slot("Query", "music")}}, expected: Intent{Name: "PartyMode", Slots: map[string]Slot{}}},
		{name: "ShouldKeepATitle", have: Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": slot("Query", "hot fuzz")}}, expected: Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": slot("Query", "hot fuzz")}}},
		{name: "ShouldKeepAnotherIntent", have: Intent{Name: "ListenToArtist", Slots: map[string]Slot{"Artist": slot("Artist", "music")}}, expected: Intent{Name: "ListenToArtist", Slots: map[string]Slot{"Artist": slot("Artist", "music")}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.Reroute("en-AU", tc.have))
		})
	}
}

// TestRerouteReadsTheKindOffASearch covers a search Amazon hears as the plain one, with the kind of title left in the query: ar-SA "دوري على فيلم الرسالة" came as GlobalSearch for "فيلم الرسالة", though "دوري على فيلم {Query}" is SearchMovies' own sample, and a search typed into Kodi would then include the word for film.
func TestRerouteReadsTheKindOffASearch(t *testing.T) {
	speech := shippedSpeech(t)

	search := func(intent, query string) Intent {
		return Intent{Name: intent, Slots: map[string]Slot{"Query": {Name: "Query", Value: query}}}
	}

	testCases := []struct {
		name     string
		locale   string
		have     string
		expected Intent
	}{
		{name: "ShouldSearchMoviesForFilmInArSA", locale: "ar-SA", have: "فيلم الرسالة", expected: search("SearchMovies", "الرسالة")},
		{name: "ShouldSearchTVShowsForSeriesInArSA", locale: "ar-SA", have: "مسلسل باب الحارة", expected: search("SearchTVShows", "باب الحارة")},
		{name: "ShouldSearchMoviesForFilm", locale: "en-AU", have: "the film dune", expected: search("SearchMovies", "dune")},
		{name: "ShouldSearchTVShowsForShow", locale: "en-AU", have: "the show bluey", expected: search("SearchTVShows", "bluey")},
		{name: "ShouldSearchMusicForBand", locale: "en-AU", have: "the band queen", expected: search("SearchMusic", "queen")},
		{name: "ShouldSearchMusicForAlbum", locale: "en-AU", have: "the album innuendo", expected: search("SearchMusic", "innuendo")},
		// A title without a kind word, and a kind there is no search of its own for, are searched as they are.
		{name: "ShouldSearchTitleWithoutKindAsItIs", locale: "en-AU", have: "dune", expected: search("GlobalSearch", "dune")},
		{name: "ShouldSearchKindWithoutItsOwnSearchAsItIs", locale: "en-AU", have: "the song bohemian rhapsody", expected: search("GlobalSearch", "the song bohemian rhapsody")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.Reroute(tc.locale, search("GlobalSearch", tc.have)))
		})
	}

	empty := Intent{Name: "GlobalSearch"}
	assert.Equal(t, empty, speech.Reroute("en-AU", empty), "a search without a query is left as it is")
}

func TestRerouteReadsTheSeasonOffTheShow(t *testing.T) {
	speech := shippedSpeech(t)

	season := func(show, season, ordinal string) Intent {
		slots := map[string]Slot{"Show": {Name: "Show", Value: show}, "Room": {Name: "Room"}}
		if season != "" {
			slots["Season"] = Slot{Name: "Season", Value: season}
		}

		if ordinal != "" {
			slots["Ordinal"] = Slot{Name: "Ordinal", Value: ordinal}
		}

		return Intent{Name: "WatchSeason", Slots: slots}
	}

	testCases := []struct {
		name     string
		locale   string
		have     Intent
		expected Intent
	}{
		{name: "ShouldReadSeasonPutInTheShowInEsUS", locale: "es-US", have: season("2 de la casa de papel", "", ""), expected: season("la casa de papel", "2", "")},
		{name: "ShouldReadSeasonByEachLanguagesOwnJoin", locale: "de-DE", have: season("2 von dark", "", ""), expected: season("dark", "2", "")},
		{name: "ShouldReadSeasonInEnglish", locale: "en-AU", have: season("3 of breaking bad", "", ""), expected: season("breaking bad", "3", "")},
		{name: "ShouldReadSeasonAsEnglishInLocaleWithoutSeasonsOfItsOwn", locale: "xx-XX", have: season("3 of breaking bad", "", ""), expected: season("breaking bad", "3", "")},
		{name: "ShouldKeepShowThatStartsWithANumberAndNoJoin", locale: "en-AU", have: season("24 hour party people", "", ""), expected: season("24 hour party people", "", "")},
		{name: "ShouldKeepShowWhenSeasonWasHeardAlready", locale: "es-US", have: season("2 de la casa de papel", "4", ""), expected: season("2 de la casa de papel", "4", "")},
		{name: "ShouldKeepShowWhenOrdinalWasHeardAlready", locale: "es-US", have: season("2 de la casa de papel", "", "tercera"), expected: season("2 de la casa de papel", "", "tercera")},
		{name: "ShouldKeepShowTooShortToHoldAShow", locale: "es-US", have: season("2 de", "", ""), expected: season("2 de", "", "")},
		{name: "ShouldKeepShowThatDoesNotStartWithANumber", locale: "en-AU", have: season("the 3 of us", "", ""), expected: season("the 3 of us", "", "")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.Reroute(tc.locale, tc.have))
		})
	}
}

func TestSeasonJoinsRejectBrokenPatterns(t *testing.T) {
	_, err := seasonJoins(map[string][]string{"WatchSeason": {"play (season {Season} of {Show}"}})
	require.Error(t, err)
}

func TestGenericWordsNeedTheTitleSlot(t *testing.T) {
	got, err := genericWords("en", map[string][]string{
		"ListenToSongOrAlbum": {"play {Title} by {Artist}", "play the record by {Artist}"},
		"ListenToArtist":      {"play music by {Artist}", "play the record by {Artist}"},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"music": false, "record": false}, got, "a sample without {Title} adds nothing of its own, and without MEDIA_TYPE words nothing is bare")
}

func TestGenericWordsRejectBrokenPatterns(t *testing.T) {
	_, err := genericWords("en", map[string][]string{"ListenToSongOrAlbum": {"play (a|b"}}, nil)
	require.Error(t, err)

	_, err = genericWords("en", map[string][]string{"ListenToArtist": {"play [a"}}, nil)
	require.Error(t, err)
}
