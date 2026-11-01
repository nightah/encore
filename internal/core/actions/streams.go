package actions

import (
	"context"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
	"golang.org/x/text/unicode/norm"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// Subtitles turns subtitles on or off, steps through them, or picks one by language, and says which is now showing.
func (s *Service) Subtitles(ctx context.Context, t Target, req StreamRequest) Outcome {
	change := media.StreamChange{Step: req.Step}

	if req.Step == "" {
		streams, err := t.Backend.Streams(ctx)
		if err != nil {
			return failed(t, err)
		}

		i, ok := streamIn(streams.Subtitles, req.Language, req.Listener)
		if !ok {
			return done(say("streams.no_subtitles_in", map[string]string{"language": req.Language}))
		}

		change = media.StreamChange{Index: i}
	}

	if err := t.Backend.SetSubtitle(ctx, change); err != nil {
		return failed(t, err)
	}

	if req.Step == media.StreamOff {
		return done(say("streams.subtitles_off", nil))
	}

	streams, err := t.Backend.Streams(ctx)
	if err != nil {
		return failed(t, err)
	}

	if len(streams.Subtitles) == 0 {
		return done(say("streams.no_subtitles", nil))
	}

	return done(streamMessage("streams.subtitles", streams.CurrentSubtitle))
}

// AudioStream steps through the audio streams or picks one by language, and says which is now playing.
func (s *Service) AudioStream(ctx context.Context, t Target, req StreamRequest) Outcome {
	change := media.StreamChange{Step: req.Step}

	if req.Step == "" {
		streams, err := t.Backend.Streams(ctx)
		if err != nil {
			return failed(t, err)
		}

		i, ok := streamIn(streams.Audio, req.Language, req.Listener)
		if !ok {
			return done(say("streams.no_audio_in", map[string]string{"language": req.Language}))
		}

		change = media.StreamChange{Index: i}
	}

	if err := t.Backend.SetAudioStream(ctx, change); err != nil {
		return failed(t, err)
	}

	streams, err := t.Backend.Streams(ctx)
	if err != nil {
		return failed(t, err)
	}

	return done(streamMessage("streams.audio", streams.CurrentAudio))
}

// DownloadSubtitles opens the subtitle search dialog.
func (s *Service) DownloadSubtitles(ctx context.Context, t Target) Outcome {
	if err := t.Backend.ShowView(ctx, media.View{Section: media.SectionSubtitleSearch}); err != nil {
		return failure(t, err)
	}

	return done(say("streams.subtitle_search", nil))
}

// streamMessage names the current stream by language, falling back to its name, then to saying nothing specific. ID is extended with "_language", "_named" or "_unknown".
func streamMessage(id string, cur *media.Stream) Message {
	switch {
	case cur == nil:
		return say(id+"_unknown", nil)
	case languageTag(cur.Language) != "":
		msg := say(id+"_language", nil)
		msg.Languages = map[string]string{"language": languageTag(cur.Language)}

		return msg
	case cur.Name != "":
		return say(id+"_named", map[string]string{"name": cur.Name})
	default:
		return say(id+"_unknown", nil)
	}
}

// languageTag canonicalises a stream language, which Kodi reports as BCP 47 from API 13.1000 ("en", "pt-BR") and as ISO 639-2 before ("eng", "fre"). It returns "" when the language is missing or unknown.
func languageTag(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}

	tag, err := language.Parse(code)
	if err != nil || tag == language.Und {
		return ""
	}

	base, conf := tag.Base()
	if conf == language.No {
		return ""
	}

	return base.String()
}

// streamIn finds the first stream whose language the listener named, comparing with the language's name in the listener's language, in English and in itself.
func streamIn(streams []media.Stream, spoken, listener string) (int, bool) {
	want := streamKey(listener, spoken)
	if want == "" {
		return 0, false
	}

	namers := []display.Namer{display.English.Languages()}
	if tag, err := language.Parse(listener); err == nil {
		namers = append(namers, display.Languages(tag))
	}

	for _, st := range streams {
		code := languageTag(st.Language)
		if code == "" {
			continue
		}

		base := language.MustParseBase(code)
		names := []string{code, display.Self.Name(base)}

		for _, n := range namers {
			names = append(names, n.Name(base))
		}

		for _, n := range names {
			if n != "" && streamKey(listener, n) == want {
				return st.Index, true
			}
		}
	}

	return 0, false
}

// streamKey is a language name as streamIn compares it: normalised, after dropping the Devanagari nukta, which recognisers write or leave out ("फ़्रेंच", "फ्रेंच") while x/text always writes it. NFD first splits a letter with a nukta (ज़) into the letter and the nukta.
func streamKey(listener, s string) string {
	return match.Normalize(listener, strings.ReplaceAll(norm.NFD.String(s), nukta, ""))
}
