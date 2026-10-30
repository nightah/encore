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

	for _, tc := range []struct {
		locale, query, intent string
		slots                 map[string]string
	}{
		{"en-AU", "the movie frozen", "WatchMovie", map[string]string{"Movie": "frozen"}},
		{"en-AU", "music by pink floyd", "ListenToArtist", map[string]string{"Artist": "pink floyd"}},
		{"en-AU", "something by pink floyd", "ListenToArtist", map[string]string{"Artist": "pink floyd"}},
		{"en-AU", "the album the wall by pink floyd", "ListenToAlbum", map[string]string{"Album": "the wall", "Artist": "pink floyd"}},
		{"en-AU", "frozen trailer", "WatchMovieTrailer", map[string]string{"Movie": "frozen"}},
		{"en-AU", "all music", "PartyMode", map[string]string{}},
		{"en-AU", "stand by me", "PlayMedia", map[string]string{"Query": "stand by me"}},
		{"de-DE", "den film die eiskönigin", "WatchMovie", map[string]string{"Movie": "die eiskönigin"}},
		{"de-DE", "etwas von pink floyd", "ListenToArtist", map[string]string{"Artist": "pink floyd"}},
		{"de-DE", "ein bisschen frieden von nicole", "PlayMedia", map[string]string{"Query": "ein bisschen frieden von nicole"}},
		{"fr-FR", "le film la reine des neiges", "WatchMovie", map[string]string{"Movie": "la reine des neiges"}},
		{"fr-FR", "quelque chose de daft punk", "ListenToArtist", map[string]string{"Artist": "daft punk"}},
		{"es-ES", "la película frozen", "WatchMovie", map[string]string{"Movie": "frozen"}},
		{"es-ES", "algo de shakira", "ListenToArtist", map[string]string{"Artist": "shakira"}},
		{"it-IT", "il film frozen", "WatchMovie", map[string]string{"Movie": "frozen"}},
		{"it-IT", "qualcosa di vasco rossi", "ListenToArtist", map[string]string{"Artist": "vasco rossi"}},
		{"pt-BR", "o filme cidade de deus", "WatchMovie", map[string]string{"Movie": "cidade de deus"}},
		{"pt-BR", "alguma coisa do caetano veloso", "ListenToArtist", map[string]string{"Artist": "caetano veloso"}},
		{"nl-NL", "de film flodder", "WatchMovie", map[string]string{"Movie": "flodder"}},
		{"nl-NL", "iets van golden earring", "ListenToArtist", map[string]string{"Artist": "golden earring"}},
		{"ar-SA", "فيلم الرسالة", "WatchMovie", map[string]string{"Movie": "الرسالة"}},
		{"ar-SA", "موسيقى حق عمرو دياب", "ListenToArtist", map[string]string{"Artist": "عمرو دياب"}},
		{"ar-SA", "شي من عمرو دياب", "ListenToArtist", map[string]string{"Artist": "عمرو دياب"}},
		{"hi-IN", "शोले फ़िल्म", "WatchMovie", map[string]string{"Movie": "शोले"}},
		{"hi-IN", "अरिजीत सिंह का कुछ", "ListenToArtist", map[string]string{"Artist": "अरिजीत सिंह"}},
		{"hi-IN", "अरिजीत सिंह का तुम ही हो", "PlayMedia", map[string]string{"Query": "अरिजीत सिंह का तुम ही हो"}},
		{"ja-JP", "映画の千と千尋の神隠し", "WatchMovie", map[string]string{"Movie": "千と千尋の神隠し"}},
		{"ja-JP", "宇多田ヒカルが歌う曲", "ListenToArtist", map[string]string{"Artist": "宇多田ヒカル"}},
		{"ja-JP", "宇多田ヒカルの何か", "ListenToArtist", map[string]string{"Artist": "宇多田ヒカル"}},
		{"ja-JP", "宇多田ヒカルの曲のファーストラヴ", "PlayMedia", map[string]string{"Query": "宇多田ヒカルの曲のファーストラヴ"}},
	} {
		in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.query}}}
		got := speech.Reinterpret(tc.locale, in)[0]

		values := map[string]string{}
		for name, slot := range got.Slots {
			values[name] = slot.Value
		}

		assert.Equal(t, tc.intent, got.Name, "%s %q", tc.locale, tc.query)
		assert.Equal(t, tc.slots, values, "%s %q", tc.locale, tc.query)
	}
}

func TestReinterpretEndsWithTheQuery(t *testing.T) {
	speech := shippedSpeech(t)

	in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: "the album stand by me by ben e king"}}}
	readings := speech.Reinterpret("en-AU", in)

	require.Len(t, readings, maxQueryForms+1)
	assert.Equal(t, "ListenToAlbum", readings[0].Name)
	assert.Equal(t, "stand", readings[0].Slots["Album"].Value, "the form with the most words comes first")
	assert.Equal(t, in, readings[len(readings)-1])

	seen := map[string]bool{}
	for _, r := range readings {
		key := r.Name + "|" + r.Slots["Album"].Value + "|" + r.Slots["Artist"].Value
		assert.False(t, seen[key], "readings are distinct: %s", key)
		seen[key] = true
	}
}

func TestStripKind(t *testing.T) {
	speech := shippedSpeech(t)

	type want struct {
		title string
		kind  media.Kind
		ok    bool
	}

	for _, tc := range []struct {
		locale, title string
		want          want
	}{
		{"en-AU", "the movie frozen", want{"frozen", media.KindMovie, true}},
		{"en-AU", "Frozen Movie", want{"Frozen", media.KindMovie, true}},
		{"en-AU", "the movie the wall", want{"the wall", media.KindMovie, true}},
		{"en-AU", "the wall album", want{"the wall", media.KindAlbum, true}},
		{"en-AU", "bluey t. v. show", want{"bluey", media.KindTVShow, true}},
		{"en-AU", "the band queen", want{"queen", media.KindArtist, true}},
		{"en-AU", "thriller music video", want{"thriller", media.KindMusicVideo, true}},
		{"en-AU", "movie", want{"", "", false}},
		{"en-AU", "the movie", want{"", "", false}},
		{"en-AU", "frozen", want{"", "", false}},
		{"en-AU", "", want{"", "", false}},
		{"de-DE", "der film die eiskönigin", want{"die eiskönigin", media.KindMovie, true}},
		{"fr-FR", "la reine des neiges le film", want{"la reine des neiges", media.KindMovie, true}},
		{"fr-FR", "le film la reine des neiges", want{"la reine des neiges", media.KindMovie, true}},
		{"es-ES", "la canción despacito", want{"despacito", media.KindSong, true}},
		{"it-IT", "la vita è bella il film", want{"la vita è bella", media.KindMovie, true}},
		{"pt-BR", "o álbum tropicália", want{"tropicália", media.KindAlbum, true}},
		{"nl-NL", "het programma flikken", want{"flikken", media.KindTVShow, true}},
		{"ar-SA", "الفيلم الرسالة", want{"الرسالة", media.KindMovie, true}},
		{"ar-SA", "مسلسل طاش ما طاش", want{"طاش ما طاش", media.KindTVShow, true}},
		{"hi-IN", "शोले फ़िल्म", want{"शोले", media.KindMovie, true}},
		{"hi-IN", "शोले फ़िल्म को", want{"शोले", media.KindMovie, true}},
		{"hi-IN", "गाना तुम ही हो", want{"तुम ही हो", media.KindSong, true}},
		{"ja-JP", "千と千尋の神隠しの映画", want{"千と千尋の神隠し", media.KindMovie, true}},
		{"ja-JP", "アルバムのファーストラヴ", want{"ファーストラヴ", media.KindAlbum, true}},
		{"xx-XX", "frozen movie", want{"frozen", media.KindMovie, true}},
	} {
		title, kind, ok := speech.StripKind(tc.locale, tc.title)
		assert.Equal(t, tc.want, want{title, kind, ok}, "%s %q", tc.locale, tc.title)
	}
}

func TestGeneric(t *testing.T) {
	speech := shippedSpeech(t)

	type want struct{ generic, bare bool }

	for _, tc := range []struct {
		locale, title string
		want          want
	}{
		{"en-AU", "music", want{true, true}},
		{"en-AU", "Some Songs", want{true, true}},
		{"en-AU", "something", want{true, false}},
		{"en-AU", "bohemian rhapsody", want{false, false}},
		{"de-DE", "etwas musik", want{true, true}},
		{"de-DE", "etwas", want{true, false}},
		{"fr-FR", "des chansons", want{true, true}},
		{"fr-CA", "des tounes", want{true, true}},
		{"es-MX", "algo de música", want{true, true}},
		{"it-IT", "un po' di musica", want{true, true}},
		{"pt-BR", "umas músicas", want{true, true}},
		{"nl-NL", "wat nummers", want{true, true}},
		{"ar-SA", "اغاني", want{true, true}},
		{"ar-SA", "شي", want{true, false}},
		{"hi-IN", "संगीत", want{true, true}},
		{"hi-IN", "कुछ", want{true, false}},
		{"ja-JP", "歌", want{true, true}},
		{"xx-XX", "music", want{true, true}},
	} {
		generic, bare := speech.Generic(tc.locale, tc.title)
		assert.Equal(t, tc.want, want{generic, bare}, "%s %q", tc.locale, tc.title)
	}
}

func TestReroute(t *testing.T) {
	speech := shippedSpeech(t)

	slot := func(name, value string) Slot { return Slot{Name: name, Value: value} }

	for _, tc := range []struct {
		name string
		in   Intent
		want Intent
	}{
		{
			"music by an artist",
			Intent{Name: "ListenToSongOrAlbum", Slots: map[string]Slot{"Title": slot("Title", "music"), "Artist": slot("Artist", "queen"), "Room": slot("Room", "")}},
			Intent{Name: "ListenToArtist", Slots: map[string]Slot{"Artist": slot("Artist", "queen"), "Room": slot("Room", "")}},
		},
		{
			"a song called something",
			Intent{Name: "ListenToSong", Slots: map[string]Slot{"Song": slot("Song", "something")}},
			Intent{Name: "ListenToSong", Slots: map[string]Slot{"Song": slot("Song", "something")}},
		},
		{
			"songs from an album",
			Intent{Name: "ListenToSong", Slots: map[string]Slot{"Song": slot("Song", "songs"), "Album": slot("Album", "innuendo")}},
			Intent{Name: "ListenToSong", Slots: map[string]Slot{"Song": slot("Song", "songs"), "Album": slot("Album", "innuendo")}},
		},
		{
			"music in a room",
			Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": slot("Query", "music")}},
			Intent{Name: "PartyMode", Slots: map[string]Slot{}},
		},
		{
			"a title",
			Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": slot("Query", "hot fuzz")}},
			Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": slot("Query", "hot fuzz")}},
		},
		{
			"another intent",
			Intent{Name: "ListenToArtist", Slots: map[string]Slot{"Artist": slot("Artist", "music")}},
			Intent{Name: "ListenToArtist", Slots: map[string]Slot{"Artist": slot("Artist", "music")}},
		},
	} {
		assert.Equal(t, tc.want, speech.Reroute("en-AU", tc.in), tc.name)
	}
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
