package alexa

import (
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/skillmodel"
)

var placeholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// NewSpeech loads every locale's responses and slot values from src.
func NewSpeech(src *skillmodel.Sources, log *slog.Logger) (*Speech, error) {
	s := &Speech{
		responses:   map[string]map[string]string{},
		languages:   map[string]string{},
		slotIDs:     map[string]map[string]map[string]string{},
		roomPhrases: map[string][][]string{},
		log:         log,
	}

	for _, locale := range src.LocaleNames() {
		r, err := src.Responses(locale)
		if err != nil {
			return nil, err
		}

		s.responses[locale] = r
		s.languages[locale] = src.Locales[locale].Language

		types, err := src.Types(locale)
		if err != nil {
			return nil, err
		}

		s.slotIDs[locale] = slotIDs(s.languages[locale], types)

		utterances, err := src.Utterances(locale)
		if err != nil {
			return nil, err
		}

		if s.roomPhrases[locale], err = roomPhrases(utterances); err != nil {
			return nil, fmt.Errorf("locale %s: %w", locale, err)
		}
	}

	if _, ok := s.responses[fallbackLocale]; !ok {
		return nil, fmt.Errorf("fallback locale %s is missing", fallbackLocale)
	}

	return s, nil
}

// Render returns msg as plain text, or "" for a silent message.
func (s *Speech) Render(locale string, msg actions.Message) string {
	if msg.ID == "" {
		return ""
	}

	responses, ok := s.responses[locale]
	if !ok {
		locale, responses = fallbackLocale, s.responses[fallbackLocale]
	}

	text, ok := responses[msg.ID]
	if !ok && locale != fallbackLocale {
		// Not translated yet (Crowdin leaves untranslated messages out), so English is spoken, with English durations and lists.
		locale, text, ok = fallbackLocale, s.responses[fallbackLocale][msg.ID], s.responses[fallbackLocale][msg.ID] != ""
	}

	if !ok {
		s.log.Error("missing response", "locale", locale, "id", msg.ID)

		return ""
	}

	lang := s.languages[locale]

	return placeholder.ReplaceAllStringFunc(text, func(m string) string {
		name := m[1 : len(m)-1]

		if d, ok := msg.Durations[name]; ok {
			return spokenDuration(lang, d)
		}

		if t, ok := msg.Times[name]; ok {
			return spokenTime(lang, t)
		}

		if code, ok := msg.Languages[name]; ok {
			return languageName(locale, code)
		}

		if list, ok := msg.Lists[name]; ok {
			return spokenList(lang, list)
		}

		return msg.Params[name]
	})
}

// SlotID returns the ID of a custom slot type's value or synonym as heard in locale, or "" when it isn't one. Values without an ID are their own ID.
func (s *Speech) SlotID(locale, typ, heard string) string {
	ids, ok := s.slotIDs[locale]
	if !ok {
		ids = s.slotIDs[fallbackLocale]
	}

	return ids[typ][match.Normalize(s.languages[locale], heard)]
}

// RoomSplits returns the ways query can be read as "<title> <room phrase> <room>", using the phrases locale's utterances put before {Room}, longest room first. The caller decides which room, if any, is real.
func (s *Speech) RoomSplits(locale, query string) []RoomSplit {
	phrases, ok := s.roomPhrases[locale]
	if !ok {
		phrases = s.roomPhrases[fallbackLocale]
	}

	words := strings.Fields(query)

	var out []RoomSplit

	for i := 1; i < len(words); i++ {
		for _, phrase := range phrases {
			end := i + len(phrase)
			if end >= len(words) || !wordsEqual(words[i:end], phrase) {
				continue
			}

			out = append(out, RoomSplit{Title: strings.Join(words[:i], " "), Room: strings.Join(words[end:], " ")})
		}
	}

	return out
}

// roomPhrases collects the words that introduce an optional trailing room in utterances, such as "in the" and "on the" from "[(in|on) the {Room}]".
func roomPhrases(utterances map[string][]string) ([][]string, error) {
	seen := map[string]bool{}

	var out [][]string

	for _, intent := range slices.Sorted(maps.Keys(utterances)) {
		for _, pattern := range utterances[intent] {
			group, ok := roomGroup(pattern)
			if !ok {
				continue
			}

			phrases, err := skillmodel.Expand(group)
			if err != nil {
				return nil, err
			}

			for _, phrase := range phrases {
				if phrase = strings.ToLower(phrase); !seen[phrase] {
					seen[phrase] = true
					out = append(out, strings.Fields(phrase))
				}
			}
		}
	}

	return out, nil
}

// roomGroup returns what comes before {Room} inside the optional group that ends a pattern with it, "(in|on) the" for "... [(in|on) the {Room}]".
func roomGroup(pattern string) (string, bool) {
	end := strings.LastIndex(pattern, "{Room}]")
	if end < 0 || strings.TrimSpace(pattern[end+len("{Room}]"):]) != "" {
		return "", false
	}

	depth := 0

	for i := end - 1; i >= 0; i-- {
		switch pattern[i] {
		case ']':
			depth++
		case '[':
			if depth == 0 {
				group := strings.TrimSpace(pattern[i+1 : end])

				return group, group != ""
			}

			depth--
		}
	}

	return "", false
}

func wordsEqual(words, phrase []string) bool {
	for i, w := range phrase {
		if !strings.EqualFold(words[i], w) {
			return false
		}
	}

	return true
}

func slotIDs(lang string, types map[string][]skillmodel.SlotValue) map[string]map[string]string {
	out := make(map[string]map[string]string, len(types))

	for typ, values := range types {
		ids := map[string]string{}

		for _, v := range values {
			id := v.ID
			if id == "" {
				id = v.Value
			}

			for _, word := range append([]string{v.Value}, v.Synonyms...) {
				if k := match.Normalize(lang, word); k != "" {
					if _, taken := ids[k]; !taken {
						ids[k] = id
					}
				}
			}
		}

		out[typ] = ids
	}

	return out
}

// spokenTime words a clock time: twelve-hour with am or pm in English, as people say it, and 24-hour elsewhere.
func spokenTime(lang string, t time.Time) string {
	if lang == "en" {
		return t.Format("3:04 pm")
	}

	return t.Format("15:04")
}

// languageName names a language code (BCP 47 or ISO 639-2) in the locale's language, e.g. "fre" is "French" in English and "Französisch" in German.
func languageName(locale, code string) string {
	base, err := language.ParseBase(code)
	if err != nil {
		tag, terr := language.Parse(code)
		if terr != nil {
			return code
		}

		base, _ = tag.Base()
	}

	listener, err := language.Parse(locale)
	if err != nil {
		listener = language.English
	}

	if name := display.Languages(listener).Name(base); name != "" {
		return name
	}

	return code
}

var listAnd = map[string]string{"en": "and", "de": "und", "fr": "et", "es": "y", "it": "e"}

func spokenList(lang string, items []string) string {
	and, ok := listAnd[lang]
	if !ok {
		and = listAnd["en"]
	}

	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}

	return strings.Join(items[:len(items)-1], ", ") + " " + and + " " + items[len(items)-1]
}

// durationWords holds the words for spoken durations per language: hour, hours, minute, minutes, and the phrase for under a minute.
var durationWords = map[string][5]string{
	"en": {"hour", "hours", "minute", "minutes", "less than a minute"},
	"de": {"Stunde", "Stunden", "Minute", "Minuten", "weniger als eine Minute"},
	"fr": {"heure", "heures", "minute", "minutes", "moins d'une minute"},
	"es": {"hora", "horas", "minuto", "minutos", "menos de un minuto"},
	"it": {"ora", "ore", "minuto", "minuti", "meno di un minuto"},
}

func spokenDuration(lang string, d time.Duration) string {
	w, ok := durationWords[lang]
	if !ok {
		w = durationWords["en"]
	}

	minutes := int(d.Round(time.Minute).Minutes())
	if minutes < 1 {
		return w[4]
	}

	unit := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}

		return strconv.Itoa(n) + " " + many
	}

	var parts []string

	if h := minutes / 60; h > 0 {
		parts = append(parts, unit(h, w[0], w[1]))
	}

	if m := minutes % 60; m > 0 {
		parts = append(parts, unit(m, w[2], w[3]))
	}

	return strings.Join(parts, " ")
}
