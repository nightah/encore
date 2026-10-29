package match

import "github.com/nightah/encore/internal/fuzzy/phonetic/en"

func dutch() *language {
	units := []string{"een", "twee", "drie", "vier", "vijf", "zes", "zeven", "acht", "negen"}
	tens := []string{"twintig", "dertig", "veertig", "vijftig", "zestig", "zeventig", "tachtig", "negentig"}

	// "een en twintig" is written "eenentwintig", and "tweeëntwintig" folds to "tweeentwintig".
	compounds := make(map[string]int)
	for ui, u := range units {
		for ti, t := range tens {
			compounds[u+"en"+t] = (ti+2)*10 + ui + 1
		}
	}

	ordinals := map[string]int{
		"eerste": 1, "tweede": 2, "derde": 3, "vierde": 4, "vijfde": 5, "zesde": 6, "zevende": 7, "achtste": 8,
		"negende": 9, "tiende": 10, "elfde": 11, "twaalfde": 12, "dertiende": 13, "veertiende": 14, "vijftiende": 15,
		"zestiende": 16, "zeventiende": 17, "achttiende": 18, "negentiende": 19, "honderdste": 100, "duizendste": 1000,
	}

	for ti, t := range tens {
		ordinals[t+"ste"] = (ti + 2) * 10
	}

	for w, v := range compounds {
		ordinals[w+"ste"] = v
	}

	return &language{
		code: "nl",
		half: "en een half",
		and:  "en",
		// "'t" and "'s" are the elided "het" and "des"; a possessive or plural apostrophe joins the word ("Ocean's", "opa's"). Dutch recognisers write the English "the" as heard, so both count.
		articles:    set("de", "het", "een", "t", "s", "the"),
		stop:        set("de", "het", "een", "t", "s", "the", "en", "van", "in", "op", "met", "voor", "te", "der", "den", "aan"),
		ordSuffixes: []string{"e", "de", "ste"},
		nums: numTable{
			units: []string{
				"nul", "", "twee", "drie", "vier", "vijf", "zes", "zeven", "acht", "negen", "tien",
				"elf", "twaalf", "dertien", "veertien", "vijftien", "zestien", "zeventien", "achttien", "negentien",
			},
			tens: tens,
			// English cardinals too: Dutch libraries keep English titles as published, so "Ocean's Eleven" meets "oceans elf".
			extraUnits: map[string]int{
				"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9,
				"ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
				"seventeen": 17, "eighteen": 18, "nineteen": 19,
			},
			extraTen: map[string]int{
				"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50, "sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90,
			},
			hundreds:  []string{"honderd", "eenhonderd", "hundred"},
			thousands: []string{"duizend", "eenduizend", "thousand"},
			cont:      map[string]int{"een": 1},
			compounds: compounds,
			ordinals:  ordinals,
		}.build(),
		fused:      true,
		unitsFirst: true,
		conj:       set("en"),
		parts:      set("deel", "hoofdstuk", "seizoen", "volume", "vol", "part", "chapter"),
		phonetic:   phoneticKeysOf(en.Encoder{}),
	}
}
