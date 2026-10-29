package match

import (
	"strings"

	"github.com/nightah/encore/internal/fuzzy/phonetic/pt"
)

func portuguese() *language {
	hundredsOf := suffixed(map[string]int{
		"duzent": 200, "trezent": 300, "quatrocent": 400, "quinhent": 500, "seiscent": 600, "setecent": 700,
		"oitocent": 800, "novecent": 900,
	}, "os", "as")

	return &language{
		code:        "pt",
		half:        "e meio",
		and:         "e",
		articles:    set("o", "a", "os", "as", "um", "uma", "uns", "umas"),
		stop:        set("o", "a", "os", "as", "um", "uma", "uns", "umas", "de", "do", "da", "dos", "das", "e", "em", "no", "na", "nos", "nas"),
		ordSuffixes: []string{"o", "a", "os", "as"},
		// "ç" always sounds as "ss" ("palhaço", "Iguaçu"), which speech recognition also writes ("palhasso"), whether it is stored precomposed or as "c" and a combining cedilla, and "3%" is said "três por cento".
		respell: strings.NewReplacer("ç", "ss", "Ç", "ss", "c\u0327", "ss", "C\u0327", "ss", "%", " por cento "),
		nums: numTable{
			units: []string{
				"zero", "", "dois", "tres", "quatro", "cinco", "seis", "sete", "oito", "nove", "dez",
				"onze", "doze", "treze", "catorze", "quinze", "dezesseis", "dezessete", "dezoito", "dezenove",
			},
			// "duas" is the feminine of "dois", "quatorze" the other spelling of 14, and the rest the European Portuguese spellings of 16, 17 and 19.
			extraUnits: map[string]int{"duas": 2, "quatorze": 14, "dezasseis": 16, "dezassete": 17, "dezanove": 19},
			tens:       []string{"vinte", "trinta", "quarenta", "cinquenta", "sessenta", "setenta", "oitenta", "noventa"},
			hundreds:   []string{"cem", "cento"},
			hundredsOf: hundredsOf,
			thousands:  []string{"mil"},
			millions:   []string{"milhao", "milhoes"},
			cont:       map[string]int{"um": 1, "uma": 1},
			ordinals: suffixed(map[string]int{
				"primeir": 1, "segund": 2, "terceir": 3, "quart": 4, "quint": 5, "sext": 6, "setim": 7, "oitav": 8,
				"non": 9, "decim": 10, "vigesim": 20, "trigesim": 30, "centesim": 100, "milesim": 1000,
			}, "o", "a", "os", "as"),
		}.build(),
		conj:     set("e"),
		parts:    set("parte", "capitulo", "temporada", "volume", "vol"), //nolint:misspell // Portuguese for "chapter".
		phonetic: phoneticKeysOf(pt.Encoder{}),
	}
}
