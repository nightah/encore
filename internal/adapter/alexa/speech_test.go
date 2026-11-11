package alexa

import (
	"fmt"
	"log/slog"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

func speechSources(t *testing.T, locales map[string]string) *skillmodel.Sources {
	t.Helper()

	fsys := fstest.MapFS{
		"model.yaml":              {Data: []byte("intents: []\n")},
		"lang/en/utterances.yaml": {Data: []byte("")},
		"lang/en/responses.yaml":  {Data: []byte("goodbye: \"Bye.\"\n")},
	}

	for name, data := range locales {
		fsys["locales/"+name+".yaml"] = &fstest.MapFile{Data: []byte(data)}
	}

	src, err := skillmodel.Load(fsys)
	require.NoError(t, err)

	return src
}

func TestNewSpeechRequiresFallbackLocale(t *testing.T) {
	_, err := NewSpeech(speechSources(t, map[string]string{"en-AU": "language: en\n"}), slog.New(slog.DiscardHandler))
	require.EqualError(t, err, "fallback locale en-US is missing")

	s, err := NewSpeech(speechSources(t, map[string]string{"en-AU": "language: en\n", "en-US": "language: en\n"}), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Equal(t, "Bye.", s.Render("en-AU", actions.Message{ID: "goodbye"}))
}

func TestNewSpeechRejectsBrokenRoomPhrase(t *testing.T) {
	_, err := NewSpeech(speechSources(t, map[string]string{
		"en-US": "language: en\nutterances:\n  WatchMovie:\n    - \"play {Movie} [(in|on the {Room}]\"\n",
	}), slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "locale en-US")
}

func TestNewSpeechRejectsBrokenQueryCarrier(t *testing.T) {
	_, err := NewSpeech(speechSources(t, map[string]string{
		"en-US": "language: en\nutterances:\n  PlayMedia:\n    - \"(play|put on {Query}\"\n",
	}), slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "locale en-US")
}

func TestNewSpeechRejectsBrokenSeasonPattern(t *testing.T) {
	_, err := NewSpeech(speechSources(t, map[string]string{
		"en-US": "language: en\nutterances:\n  WatchSeason:\n    - \"play (season {Season} of {Show}\"\n",
	}), slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "locale en-US")
}

func TestQueryFormsRejectBrokenTVSample(t *testing.T) {
	model := skillmodel.Model{Intents: []skillmodel.Intent{{Name: "WatchLatestEpisode", Content: media.ContentTV}}}

	_, err := queryForms(model, map[string][]string{
		"PlayMedia":          {"play {Query}"},
		"WatchLatestEpisode": {"play the (latest|newest episode of {Show}"},
	})
	require.Error(t, err)
}

func TestNewSpeechRejectsLocaleWithoutLanguage(t *testing.T) {
	_, err := NewSpeech(speechSources(t, map[string]string{"en-US": "language: en\n", "xx-XX": "language: xx\n"}), slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, `unknown language "xx"`)
}

func TestLanguageName(t *testing.T) {
	for _, tc := range []struct{ locale, code, want string }{
		{"en-AU", "fre", "French"},
		{"en-AU", "fr", "French"},
		{"de-DE", "fre", "Französisch"},
		{"en-AU", "pt-BR", "Portuguese"},
		{"not a locale", "ger", "German"},
		{"en-AU", "", ""},
		{"en-AU", "!!", "!!"},
		{"en-AU", "qaa", "qaa"},
	} {
		assert.Equal(t, tc.want, languageName(tc.locale, tc.code), "%s in %s", tc.code, tc.locale)
	}
}

func TestSpokenList(t *testing.T) {
	assert.Empty(t, spokenList("en", nil))
	assert.Equal(t, "Dune", spokenList("en", []string{"Dune"}))
	assert.Equal(t, "Dune and Alien", spokenList("en", []string{"Dune", "Alien"}))
	assert.Equal(t, "Dune, Alien und Heat", spokenList("de", []string{"Dune", "Alien", "Heat"}))
	assert.Equal(t, "Dune and Alien", spokenList("xx", []string{"Dune", "Alien"}), "a language without its own word uses English")
}

func TestParseDurationRejectsUnitsInTheWrongPart(t *testing.T) {
	for _, bad := range []string{"P1H", "P1S", "PT1D", "PT1W", "PT1Y", "P-1D", "PT1.5.5M", "PTM"} {
		_, ok := parseDuration(bad)
		assert.False(t, ok, bad)
	}
}

func TestPropParseDurationClockParts(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		h := rapid.IntRange(0, 48).Draw(t, "hours")
		m := rapid.IntRange(0, 59).Draw(t, "minutes")
		s := rapid.IntRange(0, 59).Draw(t, "seconds")
		want := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second

		in := fmt.Sprintf("PT%dH%dM%dS", h, m, s)

		got, ok := parseDuration(in)
		if ok != (want > 0) || got != map[bool]time.Duration{true: want, false: 0}[ok] {
			t.Fatalf("parseDuration(%q) = %s, %t; want %s", in, got, ok, want)
		}
	})
}

func FuzzParseDuration(f *testing.F) {
	for _, seed := range []string{"PT1H30M", "P1DT2H", "PT1.5M", "P1M", "PT", "PT10X", "pt2m"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		d, ok := parseDuration(s)
		if ok && d <= 0 {
			t.Fatalf("parseDuration(%q) accepted a non-positive %s", s, d)
		}

		if !ok && d != 0 {
			t.Fatalf("parseDuration(%q) rejected it but returned %s", s, d)
		}
	})
}

func TestNewSpeechRejectsBrokenMusicSample(t *testing.T) {
	_, err := NewSpeech(speechSources(t, map[string]string{
		"en-US": "language: en\nutterances:\n  ListenToArtist:\n    - \"(play|put on {Artist}\"\n",
	}), slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "locale en-US")
}

// A locale Encore has no model for reads a lone command the fallback locale's way.
func TestBareCommandInAnUnknownLocale(t *testing.T) {
	h, _, _ := newHandler(t)

	want, ok := h.Speech.BareCommand(fallbackLocale, "select")
	require.True(t, ok, "select is a remote command in English")

	got, ok := h.Speech.BareCommand("xx-XX", "select")
	require.True(t, ok)
	assert.Equal(t, want, got)

	_, ok = h.Speech.BareCommand("xx-XX", "Dune")
	assert.False(t, ok, "a title is no command")
}

func TestNewSpeechRejectsBrokenAnswerAndContinueSamples(t *testing.T) {
	for intent, pattern := range map[string]string{
		"AMAZON.YesIntent": "(yes|yeah",
		continueIntent:     "continue (watching|playing",
		nextEpisodeIntent:  "continue (watching|playing {Show}",
	} {
		t.Run(intent, func(t *testing.T) {
			_, err := NewSpeech(speechSources(t, map[string]string{
				"en-US": fmt.Sprintf("language: en\nutterances:\n  %s:\n    - %q\n", intent, pattern),
			}), slog.New(slog.DiscardHandler))
			require.ErrorContains(t, err, "locale en-US")
		})
	}
}

func TestAnswersAndLastShowFallBackToTheFallbackLocale(t *testing.T) {
	s, err := NewSpeech(speechSources(t, map[string]string{
		"en-US": "language: en\nutterances:\n" +
			"  AMAZON.YesIntent:\n    - \"yes\"\n" +
			"  ContinueWatching:\n    - \"continue watching the last show\"\n" +
			"  WatchNextEpisode:\n    - \"continue watching {Show}\"\n",
	}), slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	name, ok := s.AnswerIntent("xx-XX", "Yes")
	assert.True(t, ok, "a locale Encore doesn't have reads answers as en-US does")
	assert.Equal(t, "AMAZON.YesIntent", name)
	assert.True(t, s.MeansLastShow("xx-XX", "the last show"))
	assert.False(t, s.MeansLastShow("xx-XX", "frasier"))
}

func TestRenderDropsAStopAfterANameEndingInOne(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	s, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	switching := func(channel string) actions.Message {
		return actions.Message{ID: "livetv.switching", Params: map[string]string{"channel": channel}}
	}

	assert.Equal(t, "Switching to 9Go!", s.Render("en-AU", switching("9Go!")), "not 9Go!.")
	assert.Equal(t, "Switching to Who's Line?", s.Render("en-AU", switching("Who's Line?")))
	assert.Equal(t, "Switching to Seven.", s.Render("en-AU", switching("Seven")))
	assert.Equal(t, "Switching to Racing.com.", s.Render("en-AU", switching("Racing.com")), "a dot inside a name isn't the end of it")
	assert.Equal(t, "9Go!に切り替えます。", s.Render("ja-JP", switching("9Go!")), "only a stop straight after the name goes")
	assert.Equal(t, "Switching to 9Go! for Hot Seat!", s.Render("en-AU", actions.Message{ID: "livetv.switching_programme", Params: map[string]string{"channel": "9Go!", "title": "Hot Seat!"}}))
}

func TestWithoutLeadIn(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	s, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	movie := func(name, value string, more ...Slot) Intent {
		slots := map[string]Slot{"Movie": {Name: "Movie", Value: value}}
		for _, sl := range more {
			slots[sl.Name] = sl
		}

		return Intent{Name: name, Slots: slots}
	}

	assert.Equal(t, []Intent{movie("WatchMovie", "الرسالة")}, s.WithoutLeadIn("ar-SA", movie("WatchMovie", "اشوف فيلم الرسالة")), "the end of ابغى اشوف فيلم {Movie}")
	assert.Equal(t, []Intent{movie("WatchMovie", "dune")}, s.WithoutLeadIn("en-AU", movie("WatchMovie", "The Movie dune")), "ignoring case")
	assert.Equal(t, []Intent{movie("WatchMovie", "dune", Slot{Name: "Room", Value: "lounge"})}, s.WithoutLeadIn("en-AU", movie("WatchMovie", "movie dune", Slot{Name: "Room", Value: "lounge"})), "other slots are kept")
	assert.Empty(t, s.WithoutLeadIn("en-AU", movie("WatchMovie", "dune")), "a title without a lead-in has no other reading")
	assert.Empty(t, s.WithoutLeadIn("en-AU", movie("WatchMovie", "the movie")), "nor one that is all lead-in")
	assert.Empty(t, s.WithoutLeadIn("en-AU", movie("WatchMovie", "")))
	assert.Equal(t, s.WithoutLeadIn("en-US", movie("WatchMovie", "movie dune")), s.WithoutLeadIn("xx-XX", movie("WatchMovie", "movie dune")), "an unknown locale reads as the fallback")
}

func TestLeadInsRejectsABrokenPattern(t *testing.T) {
	_, err := leadIns("en", map[string][]string{"WatchMovie": {"play (the {Movie}"}})
	require.Error(t, err)
}
