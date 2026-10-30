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

	speech, err := NewSpeech(src, log)
	require.NoError(t, err)

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
)

// TestWordsLeftInTitleSlots plays what was meant when Alexa leaves a kind of title ("the movie"), or words that name no title at all ("music", "something"), in a title slot, in every language the skill speaks.
func TestWordsLeftInTitleSlots(t *testing.T) {
	for _, tc := range []struct {
		name, locale, intent string
		slots                map[string]string
		want                 string
	}{
		// A film, album or artist's sample heard as "play X".
		{"en film sample", "en-AU", "PlayMedia", map[string]string{"Query": "the movie hot fuzz"}, playHotFuzz},
		{"en album sample", "en-AU", "PlayMedia", map[string]string{"Query": "the album innuendo"}, playInnuendo},
		{"en music by", "en-AU", "PlayMedia", map[string]string{"Query": "music by queen"}, playQueen},
		{"en put on music by", "en-GB", "PlayMedia", map[string]string{"Query": "some songs by the beatles"}, playBeatles},
		{"de film sample", "de-DE", "PlayMedia", map[string]string{"Query": "den film hot fuzz"}, playHotFuzz},
		{"de music by", "de-DE", "PlayMedia", map[string]string{"Query": "musik von queen"}, playQueen},
		{"fr film sample", "fr-FR", "PlayMedia", map[string]string{"Query": "le film hot fuzz"}, playHotFuzz},
		{"fr music by", "fr-CA", "PlayMedia", map[string]string{"Query": "de la musique de queen"}, playQueen},
		{"es film sample", "es-MX", "PlayMedia", map[string]string{"Query": "la película hot fuzz"}, playHotFuzz},
		{"es music by", "es-ES", "PlayMedia", map[string]string{"Query": "música de queen"}, playQueen},
		{"it film sample", "it-IT", "PlayMedia", map[string]string{"Query": "il film hot fuzz"}, playHotFuzz},
		{"it music by", "it-IT", "PlayMedia", map[string]string{"Query": "un po' di musica dei queen"}, playQueen},
		{"pt film sample", "pt-BR", "PlayMedia", map[string]string{"Query": "o filme hot fuzz"}, playHotFuzz},
		{"pt music by", "pt-BR", "PlayMedia", map[string]string{"Query": "músicas do queen"}, playQueen},
		{"nl film sample", "nl-NL", "PlayMedia", map[string]string{"Query": "de film hot fuzz"}, playHotFuzz},
		{"nl music by", "nl-NL", "PlayMedia", map[string]string{"Query": "wat muziek van queen"}, playQueen},
		{"ar film sample", "ar-SA", "PlayMedia", map[string]string{"Query": "فيلم الرسالة"}, playRisala},
		{"ar music by", "ar-SA", "PlayMedia", map[string]string{"Query": "أغاني عمرو دياب"}, playAmrDiab},
		{"hi film sample", "hi-IN", "PlayMedia", map[string]string{"Query": "फ़िल्म शोले"}, playSholay},
		{"hi film sample after the title", "hi-IN", "PlayMedia", map[string]string{"Query": "शोले मूवी"}, playSholay},
		{"hi music by", "hi-IN", "PlayMedia", map[string]string{"Query": "अरिजीत सिंह के गाने"}, playArijit},
		{"ja film sample", "ja-JP", "PlayMedia", map[string]string{"Query": "映画の千と千尋の神隠し"}, playSpirited},
		{"ja music by", "ja-JP", "PlayMedia", map[string]string{"Query": "宇多田ヒカルの曲"}, playUtada},

		// A kind of title said where no sample has it: taken off once the whole title finds nothing.
		{"en kind after the title", "en-AU", "PlayMedia", map[string]string{"Query": "hot fuzz movie"}, playHotFuzz},
		{"en kind in the movie slot", "en-US", "WatchMovie", map[string]string{"Movie": "hot fuzz film"}, playHotFuzz},
		{"en album after the title", "en-CA", "PlayMedia", map[string]string{"Query": "innuendo album"}, playAsTitle},
		{"de kind after the title", "de-DE", "PlayMedia", map[string]string{"Query": "hot fuzz film"}, playHotFuzz},
		{"fr kind after the title", "fr-FR", "PlayMedia", map[string]string{"Query": "hot fuzz le film"}, playHotFuzz},
		{"es kind in the movie slot", "es-US", "WatchMovie", map[string]string{"Movie": "la peli hot fuzz"}, playHotFuzz},
		{"it kind after the title", "it-IT", "PlayMedia", map[string]string{"Query": "hot fuzz il film"}, playHotFuzz},
		{"pt kind in the movie slot", "pt-BR", "WatchMovie", map[string]string{"Movie": "filme hot fuzz"}, playHotFuzz},
		{"nl kind after the title", "nl-NL", "PlayMedia", map[string]string{"Query": "hot fuzz film"}, playHotFuzz},
		{"ar kind in the movie slot", "ar-SA", "WatchMovie", map[string]string{"Movie": "فيلم الرسالة"}, playRisala},
		{"ar kind with its article", "ar-SA", "PlayMedia", map[string]string{"Query": "الرسالة الفيلم"}, playRisala},
		{"hi kind after the title", "hi-IN", "PlayMedia", map[string]string{"Query": "शोले पिक्चर"}, playSholay},
		{"ja kind after the title", "ja-JP", "PlayMedia", map[string]string{"Query": "千と千尋の神隠しの映画"}, playSpirited},
		{"ja kind in the movie slot", "ja-JP", "WatchMovie", map[string]string{"Movie": "ムービーの千と千尋の神隠し"}, playSpirited},

		// Words that name no title, with an artist: the artist's music.
		{"en music", "en-AU", "ListenToSongOrAlbum", map[string]string{"Title": "music", "Artist": "queen"}, playQueen},
		{"en something", "en-IN", "ListenToSongOrAlbum", map[string]string{"Title": "something", "Artist": "queen"}, playQueen},
		{"en some songs as a song", "en-AU", "ListenToSong", map[string]string{"Song": "some songs", "Artist": "the beatles"}, playBeatles},
		{"de etwas", "de-DE", "ListenToSongOrAlbum", map[string]string{"Title": "etwas", "Artist": "queen"}, playQueen},
		{"fr quelque chose", "fr-FR", "ListenToSongOrAlbum", map[string]string{"Title": "quelque chose", "Artist": "queen"}, playQueen},
		{"es algo", "es-ES", "ListenToSongOrAlbum", map[string]string{"Title": "algo", "Artist": "queen"}, playQueen},
		{"it qualcosa", "it-IT", "ListenToSongOrAlbum", map[string]string{"Title": "qualcosa", "Artist": "queen"}, playQueen},
		{"pt alguma coisa", "pt-BR", "ListenToSongOrAlbum", map[string]string{"Title": "alguma coisa", "Artist": "queen"}, playQueen},
		{"nl iets", "nl-NL", "ListenToSongOrAlbum", map[string]string{"Title": "iets", "Artist": "queen"}, playQueen},
		{"ar shi", "ar-SA", "ListenToSongOrAlbum", map[string]string{"Title": "شي", "Artist": "عمرو دياب"}, playAmrDiab},
		{"ar aghani", "ar-SA", "ListenToSongOrAlbum", map[string]string{"Title": "أغاني", "Artist": "عمرو دياب"}, playAmrDiab},
		{"hi kuchh", "hi-IN", "ListenToSongOrAlbum", map[string]string{"Title": "कुछ", "Artist": "अरिजीत सिंह"}, playArijit},
		{"hi music", "hi-IN", "ListenToSongOrAlbum", map[string]string{"Title": "म्यूज़िक", "Artist": "अरिजीत सिंह"}, playArijit},
		{"ja kyoku", "ja-JP", "ListenToSongOrAlbum", map[string]string{"Title": "曲", "Artist": "宇多田ヒカル"}, playUtada},

		// Music in general on its own: any music.
		{"en some songs", "en-AU", "PlayMedia", map[string]string{"Query": "some songs"}, playParty},
		{"de musik", "de-DE", "PlayMedia", map[string]string{"Query": "musik"}, playParty},
		{"fr de la musique", "fr-FR", "PlayMedia", map[string]string{"Query": "de la musique"}, playParty},
		{"es música", "es-ES", "PlayMedia", map[string]string{"Query": "música"}, playParty},
		{"it musica", "it-IT", "PlayMedia", map[string]string{"Query": "musica"}, playParty},
		{"pt músicas", "pt-BR", "PlayMedia", map[string]string{"Query": "músicas"}, playParty},
		{"nl muziek", "nl-NL", "PlayMedia", map[string]string{"Query": "muziek"}, playParty},
		{"ar aghani alone", "ar-SA", "PlayMedia", map[string]string{"Query": "أغاني"}, playParty},
		{"hi music alone", "hi-IN", "PlayMedia", map[string]string{"Query": "म्यूज़िक"}, playParty},
		{"ja kyoku alone", "ja-JP", "PlayMedia", map[string]string{"Query": "曲"}, playParty},
		{"en a song by no one", "en-AU", "ListenToSong", map[string]string{"Song": "songs"}, playParty},

		// A reading that finds nothing gives way to the title itself.
		{"en a title that reads as a genre", "en-AU", "PlayMedia", map[string]string{"Query": "the sound of music"}, playSoundOfIt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, lounge, _ := newWorldHandler(t)
			serveBody(t, h, localeRequest(t, tc.locale, tc.intent, tc.slots))
			assert.Contains(t, lounge.Calls(), tc.want)
		})
	}
}

func TestWordsLeftInTitleSlotsPlayNothingElse(t *testing.T) {
	for _, tc := range []struct {
		name, locale, intent string
		slots                map[string]string
	}{
		{"something alone is no music", "en-AU", "PlayMedia", map[string]string{"Query": "something"}},
		{"a kind word alone is no title", "en-AU", "PlayMedia", map[string]string{"Query": "the movie"}},
		{"a kind the request rules out", "en-AU", "WatchMovie", map[string]string{"Movie": "innuendo album"}},
		{"an artist nobody has", "en-AU", "ListenToSongOrAlbum", map[string]string{"Title": "music", "Artist": "abba"}},
		{"a film nobody has", "en-AU", "PlayMedia", map[string]string{"Query": "conclave movie"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, lounge, _ := newWorldHandler(t)
			res := serveBody(t, h, localeRequest(t, tc.locale, tc.intent, tc.slots))
			assert.Empty(t, lounge.Calls())
			assert.Contains(t, string(res), "couldn't find")
		})
	}
}

func TestWordsLeftInTitleSlotsKeepTheRoom(t *testing.T) {
	h, lounge, bedroom := newWorldHandler(t)
	serveBody(t, h, localeRequest(t, "en-AU", "PlayMedia", map[string]string{"Query": "music by queen in the bedroom"}))
	assert.Empty(t, lounge.Calls())
	assert.Equal(t, []string{playQueen}, bedroom.Calls())

	h, lounge, bedroom = newWorldHandler(t)
	serveBody(t, h, localeRequest(t, "en-AU", "ListenToSongOrAlbum", map[string]string{"Title": "something", "Artist": "queen", "Room": "bedroom"}))
	assert.Empty(t, lounge.Calls())
	assert.Equal(t, []string{playQueen}, bedroom.Calls())
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
