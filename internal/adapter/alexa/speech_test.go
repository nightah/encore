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

	"github.com/nightah/encore/internal/core/actions"
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
	assert.Equal(t, "Dune and Alien", spokenList("ja", []string{"Dune", "Alien"}), "a language without its own word uses English")
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
