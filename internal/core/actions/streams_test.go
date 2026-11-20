package actions

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/core/media"
)

func withStreams(t *testing.T, english, german, french string) (Target, func() []string) {
	t.Helper()

	target, backend := playingTarget(t)
	backend.SetStreams(media.Streams{
		Audio:        []media.Stream{{Index: 0, Name: "Main", Language: english}, {Index: 1, Name: "Dub", Language: french}},
		CurrentAudio: &media.Stream{Index: 0, Name: "Main", Language: english},
		Subtitles: []media.Stream{
			{Index: 0, Name: "English", Language: english},
			{Index: 1, Name: "Deutsch", Language: german},
			{Index: 2, Name: "Signs", Language: ""},
		},
		CurrentSubtitle: &media.Stream{Index: 0, Name: "English", Language: english},
	})

	return target, backend.Calls
}

func TestSubtitles(t *testing.T) {
	testCases := []struct {
		name string
		have [3]string
	}{
		{name: "ShouldReadOmegaISO6392Codes", have: [3]string{"eng", "ger", "fre"}},
		{name: "ShouldReadPiersBCP47Tags", have: [3]string{"en-AU", "de", "fr"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target, calls := withStreams(t, tc.have[0], tc.have[1], tc.have[2])
			svc := newService()
			ctx := t.Context()

			o := svc.Subtitles(ctx, target, StreamRequest{Step: media.StreamOn})
			assert.Equal(t, "streams.subtitles_language", o.Say.ID)
			assert.Equal(t, "en", o.Say.Languages["language"])

			o = svc.Subtitles(ctx, target, StreamRequest{Step: media.StreamNext})
			assert.Equal(t, "de", o.Say.Languages["language"])

			o = svc.Subtitles(ctx, target, StreamRequest{Step: media.StreamNext})
			assert.Equal(t, "streams.subtitles_named", o.Say.ID, "a stream without a language is named instead")
			assert.Equal(t, "Signs", o.Say.Params["name"])

			o = svc.Subtitles(ctx, target, StreamRequest{Language: "german", Listener: "en-AU"})
			assert.Equal(t, "de", o.Say.Languages["language"])

			o = svc.Subtitles(ctx, target, StreamRequest{Language: "Deutsch", Listener: "en-AU"})
			assert.Equal(t, "de", o.Say.Languages["language"], "a language's own name works too")

			o = svc.Subtitles(ctx, target, StreamRequest{Language: "Japanese", Listener: "en-AU"})
			assert.Equal(t, "streams.no_subtitles_in", o.Say.ID)
			assert.Equal(t, "Japanese", o.Say.Params["language"])

			o = svc.Subtitles(ctx, target, StreamRequest{Step: media.StreamOff})
			assert.Equal(t, "streams.subtitles_off", o.Say.ID)

			assert.Contains(t, calls(), "SetSubtitle index 1")
		})
	}
}

func TestAudioStream(t *testing.T) {
	target, calls := withStreams(t, "eng", "ger", "fre")
	svc := newService()

	o := svc.AudioStream(t.Context(), target, StreamRequest{Step: media.StreamNext})
	assert.Equal(t, "streams.audio_language", o.Say.ID)
	assert.Equal(t, "fr", o.Say.Languages["language"])

	o = svc.AudioStream(t.Context(), target, StreamRequest{Language: "English", Listener: "en-US"})
	assert.Equal(t, "en", o.Say.Languages["language"])
	assert.Contains(t, calls(), "SetAudioStream index 0")

	o = svc.AudioStream(t.Context(), target, StreamRequest{Language: "Italian", Listener: "en-US"})
	assert.Equal(t, "streams.no_audio_in", o.Say.ID)
}

func TestStreamsNothingPlaying(t *testing.T) {
	target, _, _ := newTarget()

	assert.Equal(t, "playback.nothing_playing", newService().Subtitles(t.Context(), target, StreamRequest{Step: media.StreamOn}).Say.ID)
	assert.Equal(t, "playback.nothing_playing", newService().AudioStream(t.Context(), target, StreamRequest{Language: "French"}).Say.ID)
}

func TestSubtitlesWhenThereAreNone(t *testing.T) {
	target, _ := playingTarget(t)

	o := newService().Subtitles(t.Context(), target, StreamRequest{Step: media.StreamOn})
	assert.Equal(t, "streams.no_subtitles", o.Say.ID)
}

func TestDownloadSubtitles(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().DownloadSubtitles(t.Context(), target)
	assert.Equal(t, "streams.subtitle_search", o.Say.ID)
	assert.Equal(t, []string{"ShowView subtitlesearch"}, backend.Calls())
}

func TestLanguageTag(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldReadISO6392English", have: "eng", expected: "en"},
		{name: "ShouldReadISO6392BibliographicFrench", have: "fre", expected: "fr"},
		{name: "ShouldReadISO6392TerminologicFrench", have: "fra", expected: "fr"},
		{name: "ShouldReadISO6392German", have: "ger", expected: "de"},
		{name: "ShouldDropTheRegionOfPortuguese", have: "pt-BR", expected: "pt"},
		{name: "ShouldDropTheRegionOfEnglish", have: "en-AU", expected: "en"},
		{name: "ShouldReadEmptyAsNoLanguage"},
		{name: "ShouldReadAnUnknownCodeAsNoLanguage", have: "xx"},
		{name: "ShouldReadUndeterminedAsNoLanguage", have: "und"},
		{name: "ShouldReadTextAsNoLanguage", have: "not a code"},
		// Tags that parse but name no language: a private-use tag, and an undetermined one whose region (Antarctica) suggests none.
		{name: "ShouldReadAPrivateUseTagAsNoLanguage", have: "x-dub"},
		{name: "ShouldReadAnUndeterminedTagWithARegionAsNoLanguage", have: "und-AQ"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, languageTag(tc.have))
		})
	}
}

func TestStreamMessage(t *testing.T) {
	testCases := []struct {
		name      string
		have      *media.Stream
		expected  string
		params    map[string]string
		languages map[string]string
	}{
		{name: "ShouldSayAnUnknownStream", expected: "streams.audio_unknown"},
		{name: "ShouldSayTheLanguage", have: &media.Stream{Language: "fre", Name: "Dub"}, expected: "streams.audio_language", languages: map[string]string{"language": "fr"}},
		{name: "ShouldSayTheNameWithoutALanguage", have: &media.Stream{Name: "Commentary"}, expected: "streams.audio_named", params: map[string]string{"name": "Commentary"}},
		{name: "ShouldSayUnknownForAnUnknownLanguageWithoutAName", have: &media.Stream{Language: "xx"}, expected: "streams.audio_unknown"},
		{name: "ShouldSayUnknownForNothingAtAll", have: &media.Stream{}, expected: "streams.audio_unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := streamMessage("streams.audio", tc.have)

			assert.Equal(t, tc.expected, msg.ID)
			assert.Equal(t, tc.params, msg.Params)
			assert.Equal(t, tc.languages, msg.Languages)
		})
	}
}

func TestStreamIn(t *testing.T) {
	streams := []media.Stream{{Index: 3, Language: ""}, {Index: 4, Language: "ger"}, {Index: 5, Language: "fre"}, {Index: 6, Language: "fra"}, {Index: 7, Language: "eng"}}

	testCases := []struct {
		name     string
		have     string
		listener string
		expected int
		ok       bool
	}{
		{name: "ShouldFindFrenchInEnglish", have: "french", listener: "en-AU", expected: 5, ok: true},
		{name: "ShouldFindFrenchInGerman", have: "Französisch", listener: "de-DE", expected: 5, ok: true},
		{name: "ShouldFindGermanInFrench", have: "allemand", listener: "fr", expected: 4, ok: true},
		{name: "ShouldFindGermanByItsOwnNameWithoutAListener", have: "deutsch", expected: 4, ok: true},
		{name: "ShouldFindGermanWithAnUnknownListener", have: "German", listener: "not a language", expected: 4, ok: true},
		{name: "ShouldFindALanguageCode", have: "fr", listener: "en", expected: 5, ok: true},
		// Hindi with and without the nukta, which recognisers write either way.
		{name: "ShouldFindFrenchInHindiWithTheNukta", have: "फ़्रेंच", listener: "hi-IN", expected: 5, ok: true},
		{name: "ShouldFindFrenchInHindiWithoutTheNukta", have: "फ्रेंच", listener: "hi-IN", expected: 5, ok: true},
		{name: "ShouldFindEnglishInHindiWithoutTheNukta", have: "अंग्रेजी", listener: "hi-IN", expected: 7, ok: true},
		{name: "ShouldFindEnglishInHindiWithTheNukta", have: "अंग्रेज़ी", listener: "hi-IN", expected: 7, ok: true},
		{name: "ShouldFindFrenchInArabic", have: "الفرنسية", listener: "ar-SA", expected: 5, ok: true},
		{name: "ShouldFindFrenchInJapanese", have: "フランス語", listener: "ja-JP", expected: 5, ok: true},
		{name: "ShouldNotFindEmpty", listener: "en"},
		{name: "ShouldNotFindAnUnknownLanguage", have: "klingon", listener: "en"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := streamIn(streams, tc.have, tc.listener)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.expected, actual)
		})
	}

	_, ok := streamIn(nil, "french", "en")
	assert.False(t, ok)
}
