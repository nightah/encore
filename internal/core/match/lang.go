package match

import (
	"maps"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/fuzzy/phonetic/de"
	"github.com/nightah/encore/internal/fuzzy/phonetic/en"
	"github.com/nightah/encore/internal/fuzzy/phonetic/es"
	"github.com/nightah/encore/internal/fuzzy/phonetic/fr"
)

var languages = map[string]*language{
	"en": english(),
	"de": german(),
	"fr": french(),
	"es": spanish(),
	"it": italian(),
}

// Languages lists the language codes with their own title-matching rules, sorted.
func Languages() []string { return slices.Sorted(maps.Keys(languages)) }

// languageFor resolves a language or locale code ("de", "de-DE", "en_GB"), falling back to English.
func languageFor(code string) *language {
	code = strings.ToLower(code)
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		code = code[:i]
	}

	if l, ok := languages[code]; ok {
		return l
	}

	return languages["en"]
}

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}

	return m
}

func (t numTable) build() map[string]numWord {
	m := make(map[string]numWord)

	for i, w := range t.units {
		if w != "" {
			m[w] = numWord{val: i, kind: numUnit}
		}
	}

	for i, w := range t.tens {
		m[w] = numWord{val: (i + 2) * 10, kind: numTens}
	}

	for w, v := range t.extraUnits {
		m[w] = numWord{val: v, kind: numUnit}
	}

	for w, v := range t.extraTen {
		m[w] = numWord{val: v, kind: numTens}
	}

	for _, w := range t.hundreds {
		m[w] = numWord{val: 100, kind: numHundred}
	}

	for w, v := range t.hundredsOf {
		m[w] = numWord{val: v, kind: numHundred}
	}

	for _, w := range t.thousands {
		m[w] = numWord{val: 1000, kind: numThousand}
	}

	for _, w := range t.millions {
		m[w] = numWord{val: 1_000_000, kind: numMillion}
	}

	for w, v := range t.compounds {
		m[w] = numWord{val: v, kind: numCompound}
	}

	for w, v := range t.cont {
		m[w] = numWord{val: v, kind: numUnit, cont: true}
	}

	for w, v := range t.ordinals {
		m[w] = numWord{val: v, kind: kindOf(v), ordinal: true}
	}

	return m
}

func kindOf(v int) numKind {
	switch {
	case v == 100:
		return numHundred
	case v == 1000:
		return numThousand
	case v >= 20 && v%10 == 0:
		return numTens
	case v >= 20:
		return numCompound
	default:
		return numUnit
	}
}

// suffixed maps each stem+suffix to the stem's value.
func suffixed(stems map[string]int, suffixes ...string) map[string]int {
	m := make(map[string]int, len(stems)*len(suffixes))
	for s, v := range stems {
		for _, suf := range suffixes {
			m[s+suf] = v
		}
	}

	return m
}

func merge(ms ...map[string]int) map[string]int {
	out := make(map[string]int)
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}

	return out
}

func english() *language {
	return &language{
		code:        "en",
		half:        "and a half",
		and:         "and",
		articles:    set("the", "a", "an"),
		stop:        set("the", "a", "an", "of", "and", "in", "on", "to", "for", "at", "with"),
		ordSuffixes: []string{"st", "nd", "rd", "th"},
		nums: numTable{
			units: []string{
				"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
				"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen",
			},
			tens:      []string{"twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"},
			hundreds:  []string{"hundred"},
			thousands: []string{"thousand"},
			ordinals: map[string]int{
				"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "sixth": 6, "seventh": 7, "eighth": 8,
				"ninth": 9, "tenth": 10, "eleventh": 11, "twelfth": 12, "thirteenth": 13, "fourteenth": 14,
				"fifteenth": 15, "sixteenth": 16, "seventeenth": 17, "eighteenth": 18, "nineteenth": 19,
				"twentieth": 20, "thirtieth": 30, "fortieth": 40, "fiftieth": 50, "sixtieth": 60, "seventieth": 70,
				"eightieth": 80, "ninetieth": 90, "hundredth": 100,
			},
		}.build(),
		conj:     set("and"),
		parts:    set("part", "chapter", "volume", "vol"),
		phonetic: phoneticKeysOf(en.Encoder{}),
	}
}

func german() *language {
	units := map[string]int{"ein": 1, "zwei": 2, "drei": 3, "vier": 4, "funf": 5, "sechs": 6, "sieben": 7, "acht": 8, "neun": 9}
	tens := map[string]int{"zwanzig": 20, "dreissig": 30, "vierzig": 40, "funfzig": 50, "sechzig": 60, "siebzig": 70, "achtzig": 80, "neunzig": 90}

	compounds := make(map[string]int)
	for u, uv := range units {
		for t, tv := range tens {
			compounds[u+"und"+t] = uv + tv
		}
	}

	ordStems := map[string]int{
		"erst": 1, "zweit": 2, "dritt": 3, "viert": 4, "funft": 5, "sechst": 6, "siebt": 7, "acht": 8, "neunt": 9,
		"zehnt": 10, "elft": 11, "zwolft": 12, "zwanzigst": 20,
	}

	return &language{
		code:     "de",
		half:     "einhalb",
		and:      "und",
		articles: set("der", "die", "das", "ein", "eine"),
		stop:     set("der", "die", "das", "ein", "eine", "und", "von", "im", "in", "den", "dem", "des", "zu", "mit"),
		nums: numTable{
			units: []string{
				"null", "eins", "zwei", "drei", "vier", "funf", "sechs", "sieben", "acht", "neun", "zehn",
				"elf", "zwolf", "dreizehn", "vierzehn", "funfzehn", "sechzehn", "siebzehn", "achtzehn", "neunzehn",
			},
			tens:       []string{"zwanzig", "dreissig", "vierzig", "funfzig", "sechzig", "siebzig", "achtzig", "neunzig"},
			extraUnits: map[string]int{"fuenf": 5, "zwoelf": 12, "fuenfzehn": 15},
			extraTen:   map[string]int{"fuenfzig": 50},
			hundreds:   []string{"hundert", "einhundert"},
			thousands:  []string{"tausend", "eintausend"},
			cont:       map[string]int{"ein": 1, "eine": 1},
			compounds:  compounds,
			ordinals:   suffixed(ordStems, "e", "er", "en", "es", "em"),
		}.build(),
		fused:      true,
		unitsFirst: true,
		conj:       set("und"),
		parts:      set("teil", "kapitel"),
		phonetic:   germanKeys(de.Encoder{}),
	}
}

func french() *language {
	return &language{
		code:        "fr",
		half:        "et demi",
		and:         "et",
		articles:    set("le", "la", "les", "l", "un", "une"),
		stop:        set("le", "la", "les", "l", "un", "une", "de", "des", "du", "d", "et", "en", "au", "aux"),
		elide:       true,
		ordSuffixes: []string{"e", "eme", "er", "re", "ere"},
		vigesimal:   true,
		nums: numTable{
			units: []string{
				"zero", "", "deux", "trois", "quatre", "cinq", "six", "sept", "huit", "neuf", "",
				"onze", "douze", "treize", "quatorze", "quinze", "seize",
			},
			tens:      []string{"vingt", "trente", "quarante", "cinquante", "soixante"}, //nolint:misspell // French number words.
			extraTen:  map[string]int{"dix": 10, "vingts": 20},
			hundreds:  []string{"cent", "cents"},
			thousands: []string{"mille"},
			cont:      map[string]int{"un": 1, "une": 1},
			ordinals: map[string]int{
				"premier": 1, "premiere": 1, "second": 2, "seconde": 2, "deuxieme": 2, "troisieme": 3, "quatrieme": 4,
				"cinquieme": 5, "sixieme": 6, "septieme": 7, "huitieme": 8, "neuvieme": 9, "dixieme": 10,
				"onzieme": 11, "douzieme": 12, "vingtieme": 20,
			},
		}.build(),
		conj:           set("et"),
		parts:          set("partie", "chapitre"),
		phonetic:       phoneticKeysOf(fr.Encoder{}),
		soundAlikeLead: true,
	}
}

func spanish() *language {
	compounds := map[string]int{}
	for i, u := range []string{"uno", "dos", "tres", "cuatro", "cinco", "seis", "siete", "ocho", "nueve"} {
		compounds["veinti"+u] = 21 + i
	}

	hundredsOf := suffixed(map[string]int{
		"doscient": 200, "trescient": 300, "cuatrocient": 400, "quinient": 500, "seiscient": 600, "setecient": 700,
		"ochocient": 800, "novecient": 900,
	}, "os", "as")

	return &language{
		code:        "es",
		half:        "y medio",
		and:         "y",
		articles:    set("el", "la", "los", "las", "un", "una"),
		stop:        set("el", "la", "los", "las", "un", "una", "de", "del", "y", "en", "al"),
		ordSuffixes: []string{"o", "a", "er"},
		nums: numTable{
			units: []string{
				"cero", "uno", "dos", "tres", "cuatro", "cinco", "seis", "siete", "ocho", "nueve", "diez",
				"once", "doce", "trece", "catorce", "quince", "dieciseis", "diecisiete", "dieciocho", "diecinueve",
			},
			tens:       []string{"veinte", "treinta", "cuarenta", "cincuenta", "sesenta", "setenta", "ochenta", "noventa"},
			hundreds:   []string{"cien", "ciento"},
			hundredsOf: hundredsOf,
			thousands:  []string{"mil"},
			cont:       map[string]int{"un": 1, "una": 1},
			compounds:  compounds,
			ordinals: merge(
				suffixed(map[string]int{
					"primer": 1, "segund": 2, "tercer": 3, "cuart": 4, "quint": 5, "sext": 6, "septim": 7,
					"octav": 8, "noven": 9, "decim": 10,
				}, "o", "a"),
				map[string]int{"primer": 1, "tercer": 3},
			),
		}.build(),
		conj:     set("y"),
		parts:    set("parte", "capitulo"), //nolint:misspell // Spanish for "chapter".
		phonetic: phoneticKeysOf(es.Encoder{}),
	}
}

func italian() *language {
	compounds := map[string]int{}
	units := []string{"uno", "due", "tre", "quattro", "cinque", "sei", "sette", "otto", "nove"}

	for ti, t := range []string{"venti", "trenta", "quaranta", "cinquanta", "sessanta", "settanta", "ottanta", "novanta"} {
		for ui, u := range units {
			stem := t
			if u == "uno" || u == "otto" {
				stem = t[:len(t)-1]
			}

			compounds[stem+u] = (ti+2)*10 + ui + 1
		}
	}

	// "cento" and its multiples drop their final o before a vowel inside a word ("centottanta" 180, "novecentuno" 901).
	hundredsOf := map[string]int{}
	elided := map[string]string{"cent": "cento"}

	for i, u := range units[1:] {
		hundredsOf[u+"cento"] = (i + 2) * 100
		elided[u+"cent"] = u + "cento"
	}

	return &language{
		code:        "it",
		half:        "e mezzo",
		and:         "e",
		articles:    set("il", "lo", "la", "i", "gli", "le", "l", "un", "uno", "una"),
		stop:        set("il", "lo", "la", "i", "gli", "le", "l", "un", "uno", "una", "di", "e", "del", "della", "in"),
		elide:       true,
		ordSuffixes: []string{"o", "a"},
		nums: numTable{
			units: []string{
				"zero", "", "due", "tre", "quattro", "cinque", "sei", "sette", "otto", "nove", "dieci",
				"undici", "dodici", "tredici", "quattordici", "quindici", "sedici", "diciassette", "diciotto", "diciannove",
			},
			tens:       []string{"venti", "trenta", "quaranta", "cinquanta", "sessanta", "settanta", "ottanta", "novanta"},
			hundreds:   []string{"cento"},
			hundredsOf: hundredsOf,
			thousands:  []string{"mille", "mila"},
			millions:   []string{"milione", "milioni"},
			cont:       map[string]int{"un": 1, "uno": 1, "una": 1},
			compounds:  compounds,
			ordinals: suffixed(map[string]int{
				"prim": 1, "second": 2, "terz": 3, "quart": 4, "quint": 5, "sest": 6, "settim": 7, "ottav": 8,
				"non": 9, "decim": 10,
			}, "o", "a"),
		}.build(),
		fused:    true,
		elided:   elided,
		conj:     set("e"),
		parts:    set("parte", "capitolo"),
		phonetic: phoneticKeysOf(en.Encoder{}),
	}
}
