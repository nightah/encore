package match

import (
	"slices"
	"strings"
)

func (l *language) newForm(toks []string, spelled string) form {
	compact := strings.Join(toks, "")
	f := form{
		norm:        strings.Join(toks, " "),
		toks:        toks,
		keys:        l.phoneticKeys(toks),
		compact:     compact,
		compactKeys: l.phonetic(compact),
	}

	if !slices.IsSorted(toks) {
		f.sorted = strings.Join(slices.Sorted(slices.Values(toks)), " ")
	}

	if spelled != compact {
		f.spelled = spelled
	}

	for _, t := range toks {
		if isDigits(t) {
			f.digits += t
		}
	}

	return f
}

func (f *form) spelledCompact() string {
	if f.spelled == "" {
		return f.compact
	}

	return f.spelled
}

func (f *form) sortedNorm() string {
	if f.sorted == "" {
		return f.norm
	}

	return f.sorted
}

func (l *language) newEntry(d Doc) *entry {
	e := &entry{doc: d}
	seen := make(map[string]int)

	add := func(text string, derived, native bool) {
		variants := []string{text}
		if l.code == "de" {
			if v := umlautVariant(text); v != text {
				variants = append(variants, v)
			}
		}

		for _, v := range variants {
			for _, aggressive := range []bool{false, true} {
				toks, spelled := l.normalizeSpelled(v, aggressive)
				if len(toks) == 0 {
					continue
				}

				norm := strings.Join(toks, " ")
				if i, ok := seen[norm]; ok {
					// A primary name beats a derived one with the same form, and a name as written one in the language's script.
					a := &e.aliases[i]
					if a.derived && !derived && (!native || a.native) || a.native && !native && a.derived == derived {
						a.derived, a.native, a.text = derived, native, text
					}

					continue
				}

				seen[norm] = len(e.aliases)
				e.aliases = append(e.aliases, alias{form: l.newForm(toks, spelled), text: text, derived: derived, native: native})
			}
		}
	}

	// addNamed adds a name and the forms a recogniser writes it in, in the language's script.
	addNamed := func(text string, derived bool) {
		add(text, derived, false)

		if l.native != nil {
			for _, f := range l.native(text) {
				add(f, derived, true)
			}
		}
	}

	names := append([]string{d.Title}, d.Aliases...)
	for _, n := range names {
		if strings.TrimSpace(n) != "" {
			addNamed(n, false)
		}
	}

	for _, n := range names {
		for _, s := range shortenings(n) {
			addNamed(s, true)
		}
	}

	return e
}

// shortenings returns a title without its subtitle or trailing qualifier, and the subtitle on its own: "Dune: Part Two" -> "Dune", "Star Wars: Episode IV - A New Hope" -> "Star Wars: Episode IV", "Star Wars" and "A New Hope", "Avengers: Endgame" -> "Endgame", "The Office (US)" -> "The Office".
func shortenings(title string) []string {
	var out []string

	addIf := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && s != strings.TrimSpace(title) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}

	if i := strings.Index(title, "("); i > 0 {
		addIf(title[:i] + afterParen(title[i:]))
	}

	for _, sep := range []string{":", " - ", " – ", " — "} {
		if i := strings.Index(title, sep); i > 0 {
			addIf(title[:i])
		}

		if i := strings.LastIndex(title, sep); i > 0 {
			addIf(title[:i])
			addIf(title[i+len(sep):])
		}
	}

	return out
}

func afterParen(s string) string {
	if i := strings.Index(s, ")"); i >= 0 {
		return s[i+1:]
	}

	return ""
}
