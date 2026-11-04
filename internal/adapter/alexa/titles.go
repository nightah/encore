package alexa

import (
	"slices"
	"strings"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

// Generic reports whether a title slot holds words that name no title, only music in general or anything at all, as "music" does in "play music by queen" and "something" in "play something by queen", using the words locale's utterances put where ListenToSongOrAlbum puts {Title} in the samples of ListenToArtist. bare reports whether the words mean music on their own ("music", "some songs"), so asking for them alone plays any music.
func (s *Speech) Generic(locale, title string) (generic, bare bool) {
	words, ok := s.generic[locale]
	if !ok {
		locale, words = fallbackLocale, s.generic[fallbackLocale]
	}

	bare, generic = words[match.Normalize(s.languages[locale], title)]

	return generic, bare
}

// StripKind takes a word naming a kind of title off either end of a title, with the articles and particles that go with it: "the movie frozen" and "frozen movie" are "frozen", a movie, "the movie the wall" is "the wall", and "千と千尋の神隠しの映画" is "千と千尋の神隠し". It reports false when the title has no such word or nothing else.
func (s *Speech) StripKind(locale, title string) (string, media.Kind, bool) {
	nouns, ok := s.kindNouns[locale]
	if !ok {
		locale, nouns = fallbackLocale, s.kindNouns[fallbackLocale]
	}

	lang := s.languages[locale]
	words := units(lang, title)

	filler := func(i int) bool { return match.StopWord(lang, words[i]) }

	for _, n := range nouns {
		at := 0
		for at < len(words) && filler(at) {
			at++
		}

		if end := at + len(n.units); end < len(words) && unitsEqual(lang, words[at:end], n.units) {
			// A particle joining the word to the title goes with it ("映画の"); an article starts the title ("the movie the wall").
			for end < len(words) && filler(end) && !match.Article(lang, words[end]) {
				end++
			}

			if end < len(words) {
				return joinUnits(lang, words[end:]), n.kind, true
			}
		}

		last := len(words)
		for last > 0 && filler(last-1) {
			last--
		}

		if start := last - len(n.units); start > 0 && unitsEqual(lang, words[start:last], n.units) {
			for start > 0 && filler(start-1) {
				start--
			}

			if start > 0 {
				return joinUnits(lang, words[:start]), n.kind, true
			}
		}
	}

	return "", "", false
}

// unitsEqual compares words with a phrase unit by unit, as written or as the matcher normalises them, so Arabic "الفيلم" meets "فيلم".
func unitsEqual(lang string, words, phrase []string) bool {
	for i, w := range phrase {
		if strings.EqualFold(words[i], w) {
			continue
		}

		if n := match.Normalize(lang, w); n == "" || match.Normalize(lang, words[i]) != n {
			return false
		}
	}

	return true
}

// kindNouns collects the MEDIA_TYPE values and synonyms that name a kind of title (mediaTypeKinds), split into units, longest first.
func kindNouns(lang string, values []skillmodel.SlotValue) []kindNoun {
	var out []kindNoun

	for _, v := range values {
		kind, ok := mediaTypeKinds[v.ID]
		if !ok {
			continue
		}

		for _, word := range append([]string{v.Value}, v.Synonyms...) {
			out = append(out, kindNoun{units: units(lang, strings.ToLower(word)), kind: kind})
		}
	}

	slices.SortStableFunc(out, func(a, b kindNoun) int { return len(b.units) - len(a.units) })

	return out
}

// genericWords collects the words ListenToArtist's samples put where ListenToSongOrAlbum's put {Title}: "music", "some songs" and "something" from "play {Title} by {Artist}" and "play [some] (music|songs) by {Artist}", "曲" from "{Artist}が歌う{Title}を再生して" and "{Artist}が歌う曲を再生して". Each is keyed normalised, and is bare when it holds a MEDIA_TYPE word for music or songs.
func genericWords(lang string, utterances map[string][]string, values []skillmodel.SlotValue) (map[string]bool, error) {
	titles, err := expandUnits(lang, utterances["ListenToSongOrAlbum"])
	if err != nil {
		return nil, err
	}

	artists, err := expandUnits(lang, utterances["ListenToArtist"])
	if err != nil {
		return nil, err
	}

	var music [][]string

	for _, v := range values {
		if v.ID == "audio" || v.ID == "song" {
			for _, word := range append([]string{v.Value}, v.Synonyms...) {
				music = append(music, units(lang, strings.ToLower(word)))
			}
		}
	}

	out := map[string]bool{}

	for _, t := range titles {
		at := slices.Index(t, "{title}")
		if at < 0 {
			continue
		}

		before, after := t[:at], t[at+1:]

		for _, a := range artists {
			if len(a) <= len(before)+len(after) || !wordsEqual(a, before) || !wordsEqual(a[len(a)-len(after):], after) {
				continue
			}

			words := a[len(before) : len(a)-len(after)]
			if slices.ContainsFunc(words, isSlotRef) {
				continue
			}

			if key := match.Normalize(lang, joinUnits(lang, words)); key != "" {
				out[key] = out[key] || slices.ContainsFunc(music, func(m []string) bool { return containsPhrase(words, m) })
			}
		}
	}

	return out, nil
}

// expandUnits expands patterns into their lowercased samples, split into units.
func expandUnits(lang string, patterns []string) ([][]string, error) {
	var out [][]string

	for _, pattern := range patterns {
		samples, err := skillmodel.Expand(pattern)
		if err != nil {
			return nil, err
		}

		for _, sample := range samples {
			out = append(out, units(lang, strings.ToLower(sample)))
		}
	}

	return out, nil
}

// containsPhrase reports whether phrase appears in words.
func containsPhrase(words, phrase []string) bool {
	for i := 0; i+len(phrase) <= len(words); i++ {
		if wordsEqual(words[i:], phrase) {
			return true
		}
	}

	return false
}

// Reroute reads a music request whose title slot names no title (Generic) as the request it is: "play music by queen" heard as ListenToSongOrAlbum or ListenToSong is queen's music, and "play some songs" heard as PlayMedia or ListenToSong is any music, shuffled.
func (s *Speech) Reroute(locale string, intent Intent) Intent {
	slot := map[string]string{"ListenToSongOrAlbum": "Title", "ListenToSong": "Song", "PlayMedia": "Query"}[intent.Name]
	if slot == "" {
		return intent
	}

	generic, bare := s.Generic(locale, intent.Slots[slot].Value)
	artist := strings.TrimSpace(intent.Slots["Artist"].Value) != ""

	switch {
	case generic && artist:
		return Intent{Name: "ListenToArtist", Slots: keepSlots(intent.Slots, "Artist", "Room")}
	case bare && strings.TrimSpace(intent.Slots["Album"].Value) == "":
		return Intent{Name: "PartyMode", Slots: keepSlots(intent.Slots, "Room")}
	}

	return intent
}

// keepSlots copies the named slots that were filled.
func keepSlots(slots map[string]Slot, names ...string) map[string]Slot {
	out := map[string]Slot{}

	for _, name := range names {
		if slot, ok := slots[name]; ok {
			out[name] = slot
		}
	}

	return out
}
