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
	for name, codes := range map[string][3]string{
		"omega ISO 639-2": {"eng", "ger", "fre"},
		"piers BCP 47":    {"en-AU", "de", "fr"},
	} {
		t.Run(name, func(t *testing.T) {
			target, calls := withStreams(t, codes[0], codes[1], codes[2])
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
	for in, want := range map[string]string{"eng": "en", "fre": "fr", "fra": "fr", "ger": "de", "pt-BR": "pt", "en-AU": "en", "": "", "xx": "", "und": "", "not a code": ""} {
		assert.Equal(t, want, languageTag(in), in)
	}
}

func TestStreamMessage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cur       *media.Stream
		id        string
		params    map[string]string
		languages map[string]string
	}{
		{"unknown stream", nil, "streams.audio_unknown", nil, nil},
		{"language", &media.Stream{Language: "fre", Name: "Dub"}, "streams.audio_language", nil, map[string]string{"language": "fr"}},
		{"name only", &media.Stream{Name: "Commentary"}, "streams.audio_named", map[string]string{"name": "Commentary"}, nil},
		{"unknown language and name", &media.Stream{Language: "xx"}, "streams.audio_unknown", nil, nil},
		{"nothing at all", &media.Stream{}, "streams.audio_unknown", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := streamMessage("streams.audio", tc.cur)

			assert.Equal(t, tc.id, msg.ID)
			assert.Equal(t, tc.params, msg.Params)
			assert.Equal(t, tc.languages, msg.Languages)
		})
	}
}

func TestStreamIn(t *testing.T) {
	streams := []media.Stream{{Index: 3, Language: ""}, {Index: 4, Language: "ger"}, {Index: 5, Language: "fre"}, {Index: 6, Language: "fra"}}

	for _, tc := range []struct {
		spoken, listener string
		want             int
		ok               bool
	}{
		{"french", "en-AU", 5, true},
		{"Französisch", "de-DE", 5, true},
		{"allemand", "fr", 4, true},
		{"deutsch", "", 4, true},
		{"German", "not a language", 4, true},
		{"fr", "en", 5, true},
		{"", "en", 0, false},
		{"klingon", "en", 0, false},
	} {
		i, ok := streamIn(streams, tc.spoken, tc.listener)
		assert.Equal(t, tc.ok, ok, tc.spoken)
		assert.Equal(t, tc.want, i, tc.spoken)
	}

	_, ok := streamIn(nil, "french", "en")
	assert.False(t, ok)
}
