package alexa

import (
	"slices"
	"strings"
)

// firstOrdinals maps the ways Alexa writes "first" with a digit in a search query, per language, to the word the samples spell out ("the 1st episode of" for "the first episode of").
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
