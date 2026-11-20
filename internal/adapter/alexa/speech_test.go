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
	testCases := []struct {
		name     string
		locale   string
		have     string
		expected string
	}{
		{name: "ShouldNameISO6392Code", locale: "en-AU", have: "fre", expected: "French"},
		{name: "ShouldNameISO6391Code", locale: "en-AU", have: "fr", expected: "French"},
		{name: "ShouldNameInTheLocaleLanguage", locale: "de-DE", have: "fre", expected: "Französisch"},
		{name: "ShouldNameRegionalCode", locale: "en-AU", have: "pt-BR", expected: "Portuguese"},
		{name: "ShouldFallBackForInvalidLocale", locale: "not a locale", have: "ger", expected: "German"},
		{name: "ShouldSayNothingForEmptyCode", locale: "en-AU"},
		{name: "ShouldSayInvalidCodeAsItIs", locale: "en-AU", have: "!!", expected: "!!"},
		{name: "ShouldSayReservedCodeAsItIs", locale: "en-AU", have: "qaa", expected: "qaa"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, languageName(tc.locale, tc.have))
		})
	}
}

func TestSpokenList(t *testing.T) {
	testCases := []struct {
		name     string
		lang     string
		have     []string
		expected string
	}{
		{name: "ShouldSayNothingForNoTitles", lang: "en"},
		{name: "ShouldSayASingleTitle", lang: "en", have: []string{"Dune"}, expected: "Dune"},
		{name: "ShouldJoinTwoTitlesWithAnd", lang: "en", have: []string{"Dune", "Alien"}, expected: "Dune and Alien"},
		{name: "ShouldJoinThreeTitlesInGerman", lang: "de", have: []string{"Dune", "Alien", "Heat"}, expected: "Dune, Alien und Heat"},
		{name: "ShouldUseEnglishForLanguageWithoutItsOwnWord", lang: "xx", have: []string{"Dune", "Alien"}, expected: "Dune and Alien"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, spokenList(tc.lang, tc.have))
		})
	}
}

func TestParseDurationRejectsUnitsInTheWrongPart(t *testing.T) {
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldRejectHoursBeforeT", have: "P1H"},
		{name: "ShouldRejectSecondsBeforeT", have: "P1S"},
		{name: "ShouldRejectDaysAfterT", have: "PT1D"},
		{name: "ShouldRejectWeeksAfterT", have: "PT1W"},
		{name: "ShouldRejectYearsAfterT", have: "PT1Y"},
		{name: "ShouldRejectNegative", have: "P-1D"},
		{name: "ShouldRejectTwoDecimalPoints", have: "PT1.5.5M"},
		{name: "ShouldRejectUnitWithoutNumber", have: "PTM"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := parseDuration(tc.have)
			assert.False(t, ok)
		})
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
	testCases := []struct {
		name    string
		intent  string
		pattern string
	}{
		{name: "ShouldRejectBrokenYesSample", intent: "AMAZON.YesIntent", pattern: "(yes|yeah"},
		{name: "ShouldRejectBrokenContinueSample", intent: continueIntent, pattern: "continue (watching|playing"},
		{name: "ShouldRejectBrokenNextEpisodeSample", intent: nextEpisodeIntent, pattern: "continue (watching|playing {Show}"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSpeech(speechSources(t, map[string]string{
				"en-US": fmt.Sprintf("language: en\nutterances:\n  %s:\n    - %q\n", tc.intent, tc.pattern),
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

	testCases := []struct {
		name     string
		locale   string
		have     actions.Message
		expected string
	}{
		{name: "ShouldDropStopAfterExclamationMark", locale: "en-AU", have: switching("9Go!"), expected: "Switching to 9Go!"},
		{name: "ShouldDropStopAfterQuestionMark", locale: "en-AU", have: switching("Who's Line?"), expected: "Switching to Who's Line?"},
		{name: "ShouldKeepStopAfterPlainName", locale: "en-AU", have: switching("Seven"), expected: "Switching to Seven."},
		{name: "ShouldKeepStopAfterDotInsideName", locale: "en-AU", have: switching("Racing.com"), expected: "Switching to Racing.com."},
		{name: "ShouldDropOnlyStopStraightAfterName", locale: "ja-JP", have: switching("9Go!"), expected: "9Go!に切り替えます。"},
		{name: "ShouldDropStopAfterTitleEndingInOne", locale: "en-AU", have: actions.Message{ID: "livetv.switching_programme", Params: map[string]string{"channel": "9Go!", "title": "Hot Seat!"}}, expected: "Switching to 9Go! for Hot Seat!"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, s.Render(tc.locale, tc.have))
		})
	}
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

	assert.Equal(t, s.WithoutLeadIn("en-US", movie("WatchMovie", "movie dune")), s.WithoutLeadIn("xx-XX", movie("WatchMovie", "movie dune")), "an unknown locale reads as the fallback")

	testCases := []struct {
		name     string
		locale   string
		have     Intent
		expected []Intent
	}{
		{name: "ShouldDropTheEndOfAnArabicLeadIn", locale: "ar-SA", have: movie("WatchMovie", "اشوف فيلم الرسالة"), expected: []Intent{movie("WatchMovie", "الرسالة")}},
		{name: "ShouldDropLeadInIgnoringCase", locale: "en-AU", have: movie("WatchMovie", "The Movie dune"), expected: []Intent{movie("WatchMovie", "dune")}},
		{name: "ShouldKeepOtherSlots", locale: "en-AU", have: movie("WatchMovie", "movie dune", Slot{Name: "Room", Value: "lounge"}), expected: []Intent{movie("WatchMovie", "dune", Slot{Name: "Room", Value: "lounge"})}},
		{name: "ShouldGiveNoOtherReadingWithoutLeadIn", locale: "en-AU", have: movie("WatchMovie", "dune")},
		{name: "ShouldGiveNoReadingForTitleThatIsAllLeadIn", locale: "en-AU", have: movie("WatchMovie", "the movie")},
		{name: "ShouldGiveNoReadingForEmptyTitle", locale: "en-AU", have: movie("WatchMovie", "")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := s.WithoutLeadIn(tc.locale, tc.have)

			if tc.expected == nil {
				assert.Empty(t, actual)

				return
			}

			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestLeadInsRejectsABrokenPattern(t *testing.T) {
	_, err := leadIns("en", map[string][]string{"WatchMovie": {"play (the {Movie}"}})
	require.Error(t, err)
}
