package alexa

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/testkit"
)

// worldLibrary adds titles named in the skill's other scripts to the sample library, so requests can be phrased as people there would.
func worldLibrary() []media.Item {
	return append(testkit.SampleLibrary(),
		media.Item{Kind: media.KindMovie, ID: 900, Title: "الرسالة", Year: 1976},
		media.Item{Kind: media.KindArtist, ID: 901, Title: "عمرو دياب", Artist: "عمرو دياب"},
		media.Item{Kind: media.KindMovie, ID: 902, Title: "शोले", Year: 1975},
		media.Item{Kind: media.KindArtist, ID: 903, Title: "अरिजीत सिंह", Artist: "अरिजीत सिंह"},
		media.Item{Kind: media.KindMovie, ID: 904, Title: "千と千尋の神隠し", Year: 2001},
		media.Item{Kind: media.KindArtist, ID: 905, Title: "宇多田ヒカル", Artist: "宇多田ヒカル"},
		media.Item{Kind: media.KindMovie, ID: 906, Title: "The Sound of Music", Year: 1965},
	)
}

// newWorldHandler is newRoomHandler over worldLibrary.
func newWorldHandler(t *testing.T, disabled ...media.Content) (*Handler, *testkit.Backend, *testkit.Backend) {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	log := slog.New(slog.DiscardHandler)
	speech := sharedSpeech(t)

	items := worldLibrary()
	lounge, bedroom := testkit.NewBackend(items...), testkit.NewBackend(items...)
	matcher := &testkit.Matcher{Docs: testkit.Docs(items...)}

	svc := actions.NewService(log)
	svc.SetSeed(1)

	h := &Handler{
		Service:       svc,
		Speech:        speech,
		IntentContent: src.Model.IntentContent(),
		Targets: fakeTargets{
			target:  actions.Target{Name: "Lounge", Backend: lounge, Matcher: matcher, Disabled: disabled},
			bedroom: actions.Target{Name: "Bedroom", Backend: bedroom, Matcher: matcher, Disabled: disabled},
		},
		Recorder: &memRecorder{},
		Log:      log,
		Now:      func() time.Time { return time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC) },
	}

	return h, lounge, bedroom
}

// localeRequest builds an IntentRequest from the linked Echo in locale.
func localeRequest(t *testing.T, locale, name string, slots map[string]string) []byte {
	t.Helper()

	var env RequestEnvelope
	env.Version = "1.0"
	env.Context.System.Device.DeviceID = linkedDevice
	env.Request = Request{Type: TypeIntent, RequestID: "amzn1.echo-api.request.1", Locale: locale, Intent: Intent{Name: name, Slots: map[string]Slot{}}}

	for slotName, value := range slots {
		env.Request.Intent.Slots[slotName] = Slot{Name: slotName, Value: value}
	}

	b, err := json.Marshal(env)
	require.NoError(t, err)

	return b
}

const (
	playHotFuzz   = "Play movie/4 resume"
	playInnuendo  = "Play album/41"
	playAsTitle   = "Play album/41 resume"
	playQueen     = "Play artist/30 shuffle limit 100"
	playBeatles   = "Play artist/31 shuffle limit 100"
	playParty     = "PartyMode"
	playRisala    = "Play movie/900 resume"
	playAmrDiab   = "Play artist/901 shuffle limit 100"
	playSholay    = "Play movie/902 resume"
	playArijit    = "Play artist/903 shuffle limit 100"
	playSpirited  = "Play movie/904 resume"
	playUtada     = "Play artist/905 shuffle limit 100"
	playSoundOfIt = "Play movie/906 resume"
	playBohemian  = "Play song/50"
)

// TestWordsLeftInTitleSlots plays what was meant when Alexa leaves a kind of title ("the movie"), or words that name no title at all ("music", "something"), in a title slot, in every language the skill speaks.
func TestWordsLeftInTitleSlots(t *testing.T) {
	testCases := []struct {
		name     string
		locale   string
		intent   string
		slots    map[string]string
		expected string
	}{
		// A film, album or artist's sample heard as "play X".
		{name: "ShouldPlayFilmSampleInEnAU", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "the movie hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayAlbumSampleInEnAU", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "the album innuendo"}, expected: playInnuendo},
		{name: "ShouldPlayMusicByInEnAU", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "music by queen"}, expected: playQueen},
		{name: "ShouldPlayPutOnMusicByInEnGB", locale: "en-GB", intent: "PlayMedia", slots: map[string]string{"Query": "some songs by the beatles"}, expected: playBeatles},
		{name: "ShouldPlayFilmSampleInDeDE", locale: "de-DE", intent: "PlayMedia", slots: map[string]string{"Query": "den film hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayMusicByInDeDE", locale: "de-DE", intent: "PlayMedia", slots: map[string]string{"Query": "musik von queen"}, expected: playQueen},
		{name: "ShouldPlayFilmSampleInFrFR", locale: "fr-FR", intent: "PlayMedia", slots: map[string]string{"Query": "le film hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayMusicByInFrCA", locale: "fr-CA", intent: "PlayMedia", slots: map[string]string{"Query": "de la musique de queen"}, expected: playQueen},
		{name: "ShouldPlayFilmSampleInEsMX", locale: "es-MX", intent: "PlayMedia", slots: map[string]string{"Query": "la película hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayMusicByInEsES", locale: "es-ES", intent: "PlayMedia", slots: map[string]string{"Query": "música de queen"}, expected: playQueen},
		{name: "ShouldPlayFilmSampleInItIT", locale: "it-IT", intent: "PlayMedia", slots: map[string]string{"Query": "il film hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayMusicByInItIT", locale: "it-IT", intent: "PlayMedia", slots: map[string]string{"Query": "un po' di musica dei queen"}, expected: playQueen},
		{name: "ShouldPlayFilmSampleInPtBR", locale: "pt-BR", intent: "PlayMedia", slots: map[string]string{"Query": "o filme hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayMusicByInPtBR", locale: "pt-BR", intent: "PlayMedia", slots: map[string]string{"Query": "músicas do queen"}, expected: playQueen},
		{name: "ShouldPlayFilmSampleInNlNL", locale: "nl-NL", intent: "PlayMedia", slots: map[string]string{"Query": "de film hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayMusicByInNlNL", locale: "nl-NL", intent: "PlayMedia", slots: map[string]string{"Query": "wat muziek van queen"}, expected: playQueen},
		{name: "ShouldPlayFilmSampleInArSA", locale: "ar-SA", intent: "PlayMedia", slots: map[string]string{"Query": "فيلم الرسالة"}, expected: playRisala},
		{name: "ShouldPlayMusicByInArSA", locale: "ar-SA", intent: "PlayMedia", slots: map[string]string{"Query": "أغاني عمرو دياب"}, expected: playAmrDiab},
		{name: "ShouldPlayFilmSampleInHiIN", locale: "hi-IN", intent: "PlayMedia", slots: map[string]string{"Query": "फ़िल्म शोले"}, expected: playSholay},
		{name: "ShouldPlayFilmSampleAfterTheTitleInHiIN", locale: "hi-IN", intent: "PlayMedia", slots: map[string]string{"Query": "शोले मूवी"}, expected: playSholay},
		{name: "ShouldPlayMusicByInHiIN", locale: "hi-IN", intent: "PlayMedia", slots: map[string]string{"Query": "अरिजीत सिंह के गाने"}, expected: playArijit},
		{name: "ShouldPlayFilmSampleInJaJP", locale: "ja-JP", intent: "PlayMedia", slots: map[string]string{"Query": "映画の千と千尋の神隠し"}, expected: playSpirited},
		{name: "ShouldPlayMusicByInJaJP", locale: "ja-JP", intent: "PlayMedia", slots: map[string]string{"Query": "宇多田ヒカルの曲"}, expected: playUtada},

		// A song and its artist with only the word between them: split once the whole query finds nothing.
		{name: "ShouldPlaySongByArtistInEnUS", locale: "en-US", intent: "PlayMedia", slots: map[string]string{"Query": "bohemian rhapsody by queen"}, expected: playBohemian},
		{name: "ShouldPlaySongByArtistInEsUS", locale: "es-US", intent: "PlayMedia", slots: map[string]string{"Query": "bohemian rhapsody de queen"}, expected: playBohemian},
		{name: "ShouldPlaySongByArtistInPtBR", locale: "pt-BR", intent: "PlayMedia", slots: map[string]string{"Query": "bohemian rhapsody do queen"}, expected: playBohemian},
		{name: "ShouldPlaySongByArtistInNlNL", locale: "nl-NL", intent: "PlayMedia", slots: map[string]string{"Query": "bohemian rhapsody van queen"}, expected: playBohemian},

		// A command heard as a search.
		{name: "ShouldReadVolumeHeardAsASearchInEsUS", locale: "es-US", intent: "PlayMedia", slots: map[string]string{"Query": "el volumen a 30"}, expected: "Volume set 30"},

		// A kind of title said where no sample has it: taken off once the whole title finds nothing.
		{name: "ShouldPlayKindAfterTheTitleInEnAU", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "hot fuzz movie"}, expected: playHotFuzz},
		{name: "ShouldPlayKindInTheMovieSlotInEnUS", locale: "en-US", intent: "WatchMovie", slots: map[string]string{"Movie": "hot fuzz film"}, expected: playHotFuzz},
		{name: "ShouldPlayAlbumAfterTheTitleInEnCA", locale: "en-CA", intent: "PlayMedia", slots: map[string]string{"Query": "innuendo album"}, expected: playAsTitle},
		{name: "ShouldPlayKindAfterTheTitleInDeDE", locale: "de-DE", intent: "PlayMedia", slots: map[string]string{"Query": "hot fuzz film"}, expected: playHotFuzz},
		{name: "ShouldPlayKindAfterTheTitleInFrFR", locale: "fr-FR", intent: "PlayMedia", slots: map[string]string{"Query": "hot fuzz le film"}, expected: playHotFuzz},
		{name: "ShouldPlayKindInTheMovieSlotInEsUS", locale: "es-US", intent: "WatchMovie", slots: map[string]string{"Movie": "la peli hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayKindAfterTheTitleInItIT", locale: "it-IT", intent: "PlayMedia", slots: map[string]string{"Query": "hot fuzz il film"}, expected: playHotFuzz},
		{name: "ShouldPlayKindInTheMovieSlotInPtBR", locale: "pt-BR", intent: "WatchMovie", slots: map[string]string{"Movie": "filme hot fuzz"}, expected: playHotFuzz},
		{name: "ShouldPlayKindAfterTheTitleInNlNL", locale: "nl-NL", intent: "PlayMedia", slots: map[string]string{"Query": "hot fuzz film"}, expected: playHotFuzz},
		{name: "ShouldPlayKindInTheMovieSlotInArSA", locale: "ar-SA", intent: "WatchMovie", slots: map[string]string{"Movie": "فيلم الرسالة"}, expected: playRisala},
		{name: "ShouldPlayKindWithItsArticleInArSA", locale: "ar-SA", intent: "PlayMedia", slots: map[string]string{"Query": "الرسالة الفيلم"}, expected: playRisala},
		{name: "ShouldPlayKindAfterTheTitleInHiIN", locale: "hi-IN", intent: "PlayMedia", slots: map[string]string{"Query": "शोले पिक्चर"}, expected: playSholay},
		{name: "ShouldPlayKindInTheMovieSlotInHiIN", locale: "hi-IN", intent: "WatchMovie", slots: map[string]string{"Movie": "फ़िल्म शोले"}, expected: playSholay},
		{name: "ShouldPlayKindAfterTheTitleInJaJP", locale: "ja-JP", intent: "PlayMedia", slots: map[string]string{"Query": "千と千尋の神隠しの映画"}, expected: playSpirited},
		{name: "ShouldPlayKindInTheMovieSlotInJaJP", locale: "ja-JP", intent: "WatchMovie", slots: map[string]string{"Movie": "ムービーの千と千尋の神隠し"}, expected: playSpirited},

		// Words that name no title, with an artist: the artist's music.
		{name: "ShouldPlayArtistForMusicInEnAU", locale: "en-AU", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "music", "Artist": "queen"}, expected: playQueen},
		{name: "ShouldPlayArtistForSomethingInEnIN", locale: "en-IN", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "something", "Artist": "queen"}, expected: playQueen},
		{name: "ShouldPlayArtistForSomeSongsAsASongInEnAU", locale: "en-AU", intent: "ListenToSong", slots: map[string]string{"Song": "some songs", "Artist": "the beatles"}, expected: playBeatles},
		{name: "ShouldPlayArtistForEtwasInDeDE", locale: "de-DE", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "etwas", "Artist": "queen"}, expected: playQueen},
		{name: "ShouldPlayArtistForQuelqueChoseInFrFR", locale: "fr-FR", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "quelque chose", "Artist": "queen"}, expected: playQueen},
		{name: "ShouldPlayArtistForAlgoInEsES", locale: "es-ES", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "algo", "Artist": "queen"}, expected: playQueen},
		{name: "ShouldPlayArtistForQualcosaInItIT", locale: "it-IT", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "qualcosa", "Artist": "queen"}, expected: playQueen},
		{name: "ShouldPlayArtistForAlgumaCoisaInPtBR", locale: "pt-BR", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "alguma coisa", "Artist": "queen"}, expected: playQueen},
		{name: "ShouldPlayArtistForIetsInNlNL", locale: "nl-NL", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "iets", "Artist": "queen"}, expected: playQueen},
		{name: "ShouldPlayArtistForShiInArSA", locale: "ar-SA", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "شي", "Artist": "عمرو دياب"}, expected: playAmrDiab},
		{name: "ShouldPlayArtistForAghaniInArSA", locale: "ar-SA", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "أغاني", "Artist": "عمرو دياب"}, expected: playAmrDiab},
		{name: "ShouldPlayArtistForKuchhInHiIN", locale: "hi-IN", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "कुछ", "Artist": "अरिजीत सिंह"}, expected: playArijit},
		{name: "ShouldPlayArtistForMusicInHiIN", locale: "hi-IN", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "म्यूज़िक", "Artist": "अरिजीत सिंह"}, expected: playArijit},
		{name: "ShouldPlayArtistForKyokuInJaJP", locale: "ja-JP", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "曲", "Artist": "宇多田ヒカル"}, expected: playUtada},

		// Music in general on its own: any music.
		{name: "ShouldPlayAnyMusicForSomeSongsInEnAU", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "some songs"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForMusikInDeDE", locale: "de-DE", intent: "PlayMedia", slots: map[string]string{"Query": "musik"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForDeLaMusiqueInFrFR", locale: "fr-FR", intent: "PlayMedia", slots: map[string]string{"Query": "de la musique"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForMusicaInEsES", locale: "es-ES", intent: "PlayMedia", slots: map[string]string{"Query": "música"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForMusicaInItIT", locale: "it-IT", intent: "PlayMedia", slots: map[string]string{"Query": "musica"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForMusicasInPtBR", locale: "pt-BR", intent: "PlayMedia", slots: map[string]string{"Query": "músicas"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForMuziekInNlNL", locale: "nl-NL", intent: "PlayMedia", slots: map[string]string{"Query": "muziek"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForAghaniAloneInArSA", locale: "ar-SA", intent: "PlayMedia", slots: map[string]string{"Query": "أغاني"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForMusicAloneInHiIN", locale: "hi-IN", intent: "PlayMedia", slots: map[string]string{"Query": "म्यूज़िक"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForKyokuAloneInJaJP", locale: "ja-JP", intent: "PlayMedia", slots: map[string]string{"Query": "曲"}, expected: playParty},
		{name: "ShouldPlayAnyMusicForASongByNoOneInEnAU", locale: "en-AU", intent: "ListenToSong", slots: map[string]string{"Song": "songs"}, expected: playParty},

		// A reading that finds nothing gives way to the title itself.
		{name: "ShouldPlayATitleThatReadsAsAGenreInEnAU", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "the sound of music"}, expected: playSoundOfIt},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, lounge, _ := newWorldHandler(t)
			serveBody(t, h, localeRequest(t, tc.locale, tc.intent, tc.slots))
			assert.Contains(t, lounge.Calls(), tc.expected)
		})
	}
}

func TestWordsLeftInTitleSlotsPlayNothingElse(t *testing.T) {
	testCases := []struct {
		name   string
		locale string
		intent string
		slots  map[string]string
	}{
		{name: "ShouldPlayNoMusicForSomethingAlone", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "something"}},
		{name: "ShouldPlayNoTitleForAKindWordAlone", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "the movie"}},
		{name: "ShouldPlayNothingForAKindTheRequestRulesOut", locale: "en-AU", intent: "WatchMovie", slots: map[string]string{"Movie": "innuendo album"}},
		{name: "ShouldPlayNothingForAnArtistNobodyHas", locale: "en-AU", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "music", "Artist": "abba"}},
		{name: "ShouldPlayNothingForAFilmNobodyHas", locale: "en-AU", intent: "PlayMedia", slots: map[string]string{"Query": "conclave movie"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, lounge, _ := newWorldHandler(t)
			res := serveBody(t, h, localeRequest(t, tc.locale, tc.intent, tc.slots))
			assert.Empty(t, lounge.Calls())
			assert.Contains(t, string(res), "couldn't find")
		})
	}
}

func TestWordsLeftInTitleSlotsKeepTheRoom(t *testing.T) {
	testCases := []struct {
		name   string
		intent string
		slots  map[string]string
	}{
		{name: "ShouldKeepRoomNamedInTheQuery", intent: "PlayMedia", slots: map[string]string{"Query": "music by queen in the bedroom"}},
		{name: "ShouldKeepRoomSlot", intent: "ListenToSongOrAlbum", slots: map[string]string{"Title": "something", "Artist": "queen", "Room": "bedroom"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h, lounge, bedroom := newWorldHandler(t)
			serveBody(t, h, localeRequest(t, "en-AU", tc.intent, tc.slots))
			assert.Empty(t, lounge.Calls())
			assert.Equal(t, []string{playQueen}, bedroom.Calls())
		})
	}
}

func TestWordsLeftInTitleSlotsRespectContentGroups(t *testing.T) {
	const declined = "This skill is set up without that kind of title."

	h, lounge, _ := newWorldHandler(t, media.ContentMusic)
	res := serveBody(t, h, localeRequest(t, "en-AU", "PlayMedia", map[string]string{"Query": "music by queen"}))
	assert.Contains(t, string(res), declined)
	assert.Empty(t, lounge.Calls())

	h, lounge, _ = newWorldHandler(t, media.ContentMusic)
	serveBody(t, h, localeRequest(t, "en-AU", "PlayMedia", map[string]string{"Query": "the sound of music"}))
	assert.Equal(t, []string{playSoundOfIt}, lounge.Calls(), "a music reading the skill can't play gives way to the film")

	h, lounge, _ = newWorldHandler(t, media.ContentMusic)
	res = serveBody(t, h, localeRequest(t, "en-AU", "PlayMedia", map[string]string{"Query": "innuendo album"}))
	assert.NotContains(t, lounge.Calls(), playAsTitle, "an album is no title the skill can play without music")
	assert.Contains(t, string(res), "couldn't find")
}
