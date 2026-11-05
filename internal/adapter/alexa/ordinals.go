package alexa

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// firstOrdinals maps the ways Alexa writes "first" with a digit in a search query, per language, to the word the samples spell out ("the 1st episode of" for "the first episode of"). Only "first" is a literal word in samples; other ordinals reach the Ordinal slot, which writtenOrdinals reads.
var firstOrdinals = map[string]map[string]string{
	"de": {"1.": "erste"},
	"en": {"1st": "first"},
	"es": {"1º": "primer", "1°": "primer", "1.º": "primer", "1er": "primer", "1.er": "primer", "1ro": "primer", "1ª": "primera", "1.ª": "primera", "1ra": "primera"},
	"fr": {"1er": "premier", "1re": "première", "1ère": "première"},
	"hi": {"1ला": "पहला"},
	"it": {"1º": "primo", "1°": "primo", "1ª": "prima"},
	"nl": {"1e": "eerste", "1ste": "eerste"},
	"pt": {"1º": "primeiro", "1°": "primeiro", "1ª": "primeira", "1.º": "primeiro", "1.ª": "primeira"},
}

// unspacedOnes holds, per language written without spaces, how its samples spell the digit 1 beside a counter that makes it an ordinal: Japanese writes "第一話" and "一話目", which Alexa writes "第1話" and "1話目".
var unspacedOnes = map[string]unspacedOne{
	"ja": {numeral: "一", before: []string{"第"}, after: []string{"話"}},
}

// writtenOrdinals matches an ordinal written with digits the way Alexa writes it in each language ("2nd", "2e", "2ª", "2.", "第2"), capturing the number.
var writtenOrdinals = map[string]*regexp.Regexp{
	"de": regexp.MustCompile(`^(\d+)\.$`),
	"en": regexp.MustCompile(`^(\d+)(?:st|nd|rd|th)$`),
	"es": regexp.MustCompile(`^(\d+)\.?(?:º|°|ª|o|a|er|ro|ra|do|da|to|ta|vo|va|no|na|mo|ma)$`),
	"fr": regexp.MustCompile(`^(\d+)(?:er|re|ère|e|ème|eme|nd|nde)$`),
	"hi": regexp.MustCompile(`^(\d+)(?:ला|रा|था|ठा|वाँ|वां|वा)$`),
	"it": regexp.MustCompile(`^(\d+)[º°ªoa]$`),
	"ja": regexp.MustCompile(`^第(\d+)$`),
	"nl": regexp.MustCompile(`^(\d+)(?:e|ste|de)$`),
	"pt": regexp.MustCompile(`^(\d+)\.?[º°ªoa]$`),
}

// episodeCode matches a show followed by the shorthand for a season and episode, or a season alone, as Alexa writes it when asked for "bluey s two e three": "bluey s2e3", "bluey s02e03", "bluey s. 2 e. 3", "bluey s2 ep3", "bluey s2".
var episodeCode = regexp.MustCompile(`(?i)^(.+?)\s+s\.?\s?(\d{1,3})(?:\s?(?:e|ep)\.?\s?(\d{1,4}))?$`)

// Ordinal reads an ordinal said in locale as its number: a value or synonym of the ORDINAL slot type ("second", "deuxième", "दूसरा"), the digits Alexa writes for one ("2nd", "2e", "2ª") or a plain number.
func (s *Speech) Ordinal(locale, heard string) (int, bool) {
	heard = strings.TrimSpace(heard)

	if id := s.SlotID(locale, "ORDINAL", heard); id != "" {
		n, err := strconv.Atoi(id)

		return n, err == nil
	}

	lang, ok := s.languages[locale]
	if !ok {
		lang = s.languages[fallbackLocale]
	}

	return writtenOrdinal(lang, heard)
}

// writtenOrdinal reads an ordinal written with digits in lang, or a plain number.
func writtenOrdinal(lang, heard string) (int, bool) {
	if n, err := strconv.Atoi(heard); err == nil {
		return n, n >= 0
	}

	re, ok := writtenOrdinals[lang]
	if !ok {
		return 0, false
	}

	m := re.FindStringSubmatch(strings.ToLower(heard))
	if m == nil {
		return 0, false
	}

	n, err := strconv.Atoi(m[1])

	return n, err == nil
}

// spellOrdinals returns words with "first" written as the samples spell it (firstOrdinals, unspacedOnes), for matching against literal sample words; number slots still read the digits in words as they are.
func spellOrdinals(lang string, words []string) []string {
	out := slices.Clone(words)
	one, unspacedLang := unspacedOnes[lang]

	for i, w := range words {
		if o, ok := firstOrdinals[lang][strings.ToLower(w)]; ok {
			out[i] = o

			continue
		}

		if unspacedLang && w == "1" && (i > 0 && slices.Contains(one.before, words[i-1]) || i+1 < len(words) && slices.Contains(one.after, words[i+1])) {
			out[i] = one.numeral
		}
	}

	return out
}

// codedEpisode reads a search query ending in an episode code (episodeCode) as WatchEpisode, or WatchSeason for a season alone.
func codedEpisode(query string) (Intent, bool) {
	m := episodeCode.FindStringSubmatch(strings.TrimSpace(query))
	if m == nil {
		return Intent{}, false
	}

	slots := map[string]Slot{
		"Show":   {Name: "Show", Value: m[1]},
		"Season": {Name: "Season", Value: strconv.Itoa(atoi(m[2]))},
	}

	if m[3] == "" {
		return Intent{Name: "WatchSeason", Slots: slots}, true
	}

	slots["Episode"] = Slot{Name: "Episode", Value: strconv.Itoa(atoi(m[3]))}

	return Intent{Name: "WatchEpisode", Slots: slots}, true
}

// atoi reads digits episodeCode matched, which always parse.
func atoi(digits string) int {
	n, _ := strconv.Atoi(digits)

	return n
}
