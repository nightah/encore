package skillmodel

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/nightah/encore/internal/core/media"
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}

	return "warning"
}

func (f Finding) String() string {
	var where []string

	if f.Locale != "" {
		where = append(where, f.Locale)
	}

	if f.Intent != "" {
		where = append(where, f.Intent)
	}

	if len(where) == 0 {
		return fmt.Sprintf("%s: %s", f.Severity, f.Message)
	}

	return fmt.Sprintf("%s: %s: %s", f.Severity, strings.Join(where, "/"), f.Message)
}

var (
	slotRef        = regexp.MustCompile(`\{([^{}]*)\}`)
	placeholderRef = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	// Amazon accepts letters, apostrophes, periods (for "t.v."), hyphens and spaces in samples. Digits must be spelled out. Combining marks are letters' vowel signs in Devanagari.
	sampleChars = regexp.MustCompile(`^[\p{L}\p{M}' .\-]*$`)

	latinInvocation = regexp.MustCompile(`^\p{Ll}[\p{Ll}' .]*\p{Ll}$`)
	latinOnly       = regexp.MustCompile(`^[\p{Latin}' .]*$`)
	// latinWord is a lowercase Latin word among words of another script: an English word in Hindi, a loanword in Arabic.
	latinWord      = regexp.MustCompile(`^\p{Latin}[\p{Latin}'.]*$`)
	devanagariWord = regexp.MustCompile(`^[\p{Devanagari}\p{M}]+$`)
	arabicWord     = regexp.MustCompile(`^[\p{Arabic}\p{M}]+$`)
	// Japanese names are kana and kanji, with an acronym's Latin letters each lowercase and followed by a space.
	japaneseInvocation = regexp.MustCompile(`^(?:[a-z] |[\p{Hiragana}\p{Katakana}\p{Han}ー])*[\p{Hiragana}\p{Katakana}\p{Han}ー]$`)
	digit              = regexp.MustCompile(`\p{N}`)

	// Sample words of each script, with the apostrophes, periods and hyphens samples may hold; Latin words must also be lowercase (lowercaseLatin).
	latinSample      = regexp.MustCompile(`^[\p{Latin}\p{M}'.\-]+$`)
	devanagariSample = regexp.MustCompile(`^[\p{Devanagari}\p{M}'.\-]+$`)
	arabicSample     = regexp.MustCompile(`^[\p{Arabic}\p{M}'.\-]+$`)
	japaneseLetter   = regexp.MustCompile(`^[\p{Hiragana}\p{Katakana}\p{Han}ー]$`)

	// reservedWords are banned from invocation names in every language: Amazon's own names and English launch words.
	reservedWords = []string{"alexa", "amazon", "echo", "skill", "app", "launch", "ask", "tell", "open"}

	// languageReserved are each language's own spellings of the banned names and the launch words Amazon lists for it.
	languageReserved = map[string][]string{
		"ar": {"اليكسا", "امازون", "ايكو", "تطبيق", "افتحي", "شغلي"},
		"fr": {"appli", "application"},
		"hi": {"अलेक्सा", "एलेक्सा", "ऐलेक्सा", "ऐमेज़ोन", "ऐको", "स्किल", "ऐप"},
		"ja": {"アレクサ", "アマゾン", "エコー", "スキル", "アプリ", "起動して", "使って", "開いて", "スタート", "開始", "実行"},
		"nl": {"start", "speel", "open", "vraag"},
		"pt": {"aplicativo", "começar", "iniciar", "abrir"},
	}

	// scripts are the rules of the languages not written in Latin script, by language; every other language follows latinScript.
	scripts = map[string]scriptRules{
		"ar": {
			invocation:    wordsOf(arabicWord),
			message:       "must be Arabic words, or lowercase Latin for loanwords Arabic can't spell, separated by single spaces",
			sampleWord:    nativeOrLatin(arabicSample),
			sampleMessage: "Arabic or lowercase Latin",
		},
		"hi": {
			invocation:    wordsOf(devanagariWord),
			message:       "must be Devanagari words, or lowercase Latin for English words, separated by single spaces",
			sampleWord:    nativeOrLatin(devanagariSample),
			sampleMessage: "Devanagari or lowercase Latin",
		},
		"ja": {
			invocation:    japaneseInvocation.MatchString,
			message:       "must be Hiragana, Katakana or Kanji, with Latin letters only for acronyms, each lowercase and followed by a space",
			sampleWord:    japaneseSampleWord,
			sampleMessage: "Hiragana, Katakana, Kanji or lowercase Latin",
			unspaced:      true,
		},
	}

	latinScript = scriptRules{
		invocation:    func(name string) bool { return latinInvocation.MatchString(name) && latinOnly.MatchString(name) },
		message:       "must be lowercase letters, spaces, apostrophes or periods",
		sampleWord:    lowercaseLatin,
		sampleMessage: "lowercase Latin",
	}
)

// scriptOf returns the rules for a language's script.
func scriptOf(language string) scriptRules {
	if s, ok := scripts[language]; ok {
		return s
	}

	return latinScript
}

// lowercaseLatin reports whether a sample word is lowercase Latin.
func lowercaseLatin(word string) bool {
	return latinSample.MatchString(word) && word == strings.ToLower(word)
}

// japaneseSampleWord accepts a run of Japanese text, which has no spaces, when every character is kana, kanji or ー, or belongs to a lowercase Latin word written straight against them ("{Movie}をplayして").
func japaneseSampleWord(word string) bool {
	for _, r := range word {
		if c := string(r); !japaneseLetter.MatchString(c) && !lowercaseLatin(c) {
			return false
		}
	}

	return true
}

// nativeOrLatin accepts a sample word in the native script or, as Amazon allows for English words and loanwords, in lowercase Latin.
func nativeOrLatin(native *regexp.Regexp) func(string) bool {
	return func(word string) bool { return native.MatchString(word) || lowercaseLatin(word) }
}

// wordsOf accepts names of single-space-separated words, each either a native word or a lowercase Latin one, and no digits.
func wordsOf(native *regexp.Regexp) func(string) bool {
	return func(name string) bool {
		if name == "" || digit.MatchString(name) {
			return false
		}

		for w := range strings.SplitSeq(name, " ") {
			if !native.MatchString(w) && (!latinWord.MatchString(w) || w != strings.ToLower(w)) {
				return false
			}
		}

		return true
	}
}

// Lint checks the sources for problems that would fail an Alexa model build, cause intents to collide, or leave responses untranslated.
func Lint(src *Sources) []Finding {
	l := &linter{src: src}

	l.model()
	l.types()
	l.keys()

	for _, locale := range src.LocaleNames() {
		l.locale(locale)
	}

	l.responses()

	sort.SliceStable(l.findings, func(i, j int) bool {
		a, b := l.findings[i], l.findings[j]
		if a.Locale != b.Locale {
			return a.Locale < b.Locale
		}

		if a.Intent != b.Intent {
			return a.Intent < b.Intent
		}

		return a.Severity > b.Severity
	})

	return l.findings
}

// HasErrors reports whether any finding is an error.
func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == Error {
			return true
		}
	}

	return false
}

func (l *linter) add(sev Severity, locale, intent, format string, args ...any) {
	l.findings = append(l.findings, Finding{Severity: sev, Locale: locale, Intent: intent, Message: fmt.Sprintf(format, args...)})
}

func (l *linter) model() {
	intents := map[string]bool{}
	types := map[string]bool{}

	for _, t := range l.src.Model.Types {
		if types[t.Name] {
			l.add(Error, "", "", "slot type %s is declared twice", t.Name)
		}

		types[t.Name] = true
	}

	// Amazon requires a slot name to have one type across the model: "Slot X in intent Y cannot reference more than one slot type".
	slotTypes := map[string]string{}
	slotFirst := map[string]string{}

	for _, in := range l.src.Model.Intents {
		if intents[in.Name] {
			l.add(Error, "", in.Name, "intent is declared twice")
		}

		intents[in.Name] = true

		if in.Content != "" && !slices.Contains(media.Contents, in.Content) {
			l.add(Error, "", in.Name, "content %q is not one of movies, tv or music", in.Content)
		}

		slots := map[string]bool{}

		for _, s := range in.Slots {
			if slots[s.Name] {
				l.add(Error, "", in.Name, "slot %s is declared twice", s.Name)
			}

			slots[s.Name] = true

			if !IsBuiltin(s.Type) && !types[s.Type] {
				l.add(Error, "", in.Name, "slot %s uses undeclared type %s", s.Name, s.Type)
			}

			if prev, ok := slotTypes[s.Name]; ok && prev != s.Type {
				l.add(Error, "", in.Name, "slot %s has type %s here but %s in %s; Amazon requires one type per slot name, so rename one", s.Name, s.Type, prev, slotFirst[s.Name])
			} else if !ok {
				slotTypes[s.Name], slotFirst[s.Name] = s.Type, in.Name
			}
		}
	}
}

// types checks every language gives each custom slot type values (examples, for a dynamic type) and gives none for undeclared types, with static values in types.yaml and dynamic examples in examples.yaml.
func (l *linter) types() {
	for name, lang := range l.src.Languages {
		where := "lang/" + name

		for typ := range lang.TypeKeys {
			if t, ok := l.src.Model.Type(typ); ok && t.Dynamic {
				l.add(Error, where, "", "types.yaml gives values for dynamic slot type %s; its examples belong in examples.yaml", typ)
			}
		}

		for typ := range lang.Examples {
			if t, ok := l.src.Model.Type(typ); ok && !t.Dynamic {
				l.add(Error, where, "", "examples.yaml gives values for static slot type %s; they belong in types.yaml", typ)
			}
		}

		for typ := range lang.Types {
			if _, ok := l.src.Model.Type(typ); !ok {
				l.add(Error, where, "", "values given for slot type %s, which model.yaml doesn't declare", typ)
			}
		}

		for _, t := range l.src.Model.Types {
			if len(t.Values)+len(lang.Types[t.Name]) > 0 {
				continue
			}

			if t.Dynamic {
				l.add(Error, where, "", "dynamic slot type %s needs example values so the model builds before any are configured", t.Name)
			} else {
				l.add(Error, where, "", "slot type %s has no values; Amazon rejects empty custom types", t.Name)
			}
		}
	}
}

// keys checks each language uses only the reference language's utterance and slot value keys: Crowdin writes translations under the reference keys, so any other key would be lost at the next sync.
func (l *linter) keys() {
	ref, ok := l.src.Languages[ReferenceLanguage]
	if !ok {
		return
	}

	for name, lang := range l.src.Languages {
		if name == ReferenceLanguage {
			continue
		}

		where := "lang/" + name

		for _, c := range []struct {
			file      string
			got, want map[string][]string
		}{{"utterances.yaml", lang.UtteranceKeys, ref.UtteranceKeys}, {"types.yaml", lang.TypeKeys, ref.TypeKeys}} {
			for section, keys := range c.got {
				for _, key := range keys {
					if !slices.Contains(c.want[section], key) {
						l.add(Error, where, "", "%s: %s.%s isn't in lang/%s; add it there first, or Crowdin will drop it", c.file, section, key, ReferenceLanguage)
					}
				}
			}
		}
	}
}

func (l *linter) locale(locale string) {
	loc := l.src.Locales[locale]

	if _, ok := l.src.Languages[loc.Language]; !ok {
		l.add(Error, locale, "", "unknown language %q", loc.Language)

		return
	}

	l.script = scriptOf(loc.Language)
	l.invocation(locale, loc.Language, loc.Invocation)

	if loc.KodiInvocation != "" {
		l.invocation(locale, loc.Language, loc.KodiInvocation)
	}

	if loc.SkillName == "" {
		l.add(Error, locale, "", "skill_name is required")
	}

	if _, err := l.src.Types(locale); err != nil {
		l.add(Error, locale, "", "%v", strings.TrimPrefix(err.Error(), "locale "+locale+": "))
	}

	utterances, _ := l.src.Utterances(locale)

	for intent := range utterances {
		if _, ok := l.src.Model.Intent(intent); !ok {
			l.add(Error, locale, intent, "utterances given for an intent not in model.yaml")
		}
	}

	exact := map[string]sampleOwner{}
	carriers := map[string]sampleOwner{}

	for _, in := range l.src.Model.Intents {
		var samples []string

		for _, pattern := range utterances[in.Name] {
			expanded, err := Expand(pattern)
			if err != nil {
				l.add(Error, locale, in.Name, "%v", err)

				continue
			}

			for _, s := range expanded {
				samples = append(samples, s)
				l.sample(locale, in, pattern, s, exact, carriers)
			}
		}

		l.counts(locale, in, samples)
	}
}

func (l *linter) invocation(locale, language, name string) {
	script := l.script
	name = norm.NFC.String(name)

	if !script.invocation(name) {
		l.add(Error, locale, "", "invocation name %q %s", name, script.message)

		return
	}

	words := strings.Fields(name)

	for _, r := range append(slices.Clone(reservedWords), languageReserved[language]...) {
		r = norm.NFC.String(r)

		if slices.Contains(words, r) || script.unspaced && strings.Contains(name, r) {
			l.add(Error, locale, "", "invocation name %q contains the reserved word %q", name, r)
		}
	}

	if !script.unspaced && len(words) == 1 {
		l.add(Warning, locale, "", "one-word invocation name %q is fine for a private skill but would need brand proof to certify", name)
	}
}

func (l *linter) sample(locale string, in Intent, pattern, sample string, exact, carriers map[string]sampleOwner) {
	declared := map[string]Slot{}
	for _, s := range in.Slots {
		declared[s.Name] = s
	}

	refs := slotRef.FindAllStringSubmatch(sample, -1)

	// Amazon fails to parse a slot joined to other text, such as the possessive "{Artist}'s". Japanese writes no spaces, and its particles follow the slot directly ("{Movie}を再生して").
	for _, loc := range slotRef.FindAllStringIndex(sample, -1) {
		if !l.script.unspaced && ((loc[0] > 0 && sample[loc[0]-1] != ' ') || (loc[1] < len(sample) && sample[loc[1]] != ' ')) {
			l.add(Error, locale, in.Name, "sample %q joins slot %s to other text; Amazon needs a space on each side (rephrase possessives such as \"{Artist}'s\")", sample, sample[loc[0]:loc[1]])
		}
	}

	for _, ref := range refs {
		slot, ok := declared[ref[1]]
		if !ok {
			l.add(Error, locale, in.Name, "sample %q references undeclared slot {%s}", sample, ref[1])

			continue
		}

		if slot.Type == "AMAZON.SearchQuery" && strings.TrimSpace(slotRef.ReplaceAllString(sample, "")) == "" {
			l.add(Error, locale, in.Name, "sample %q needs a carrier phrase around the AMAZON.SearchQuery slot", sample)
		}

		if slot.Type == "AMAZON.SearchQuery" && len(refs) > 1 {
			l.add(Error, locale, in.Name, "sample %q combines an AMAZON.SearchQuery slot with other slots, which Amazon rejects", sample)
		}
	}

	if text := slotRef.ReplaceAllString(sample, ""); !sampleChars.MatchString(text) {
		l.add(Error, locale, in.Name, "sample %q may only contain letters, spaces, apostrophes, periods and hyphens (spell out numbers)", sample)
	} else {
		for w := range strings.FieldsSeq(text) {
			if !l.script.sampleWord(w) {
				l.add(Error, locale, in.Name, "sample %q has the word %q, which isn't %s", sample, w, l.script.sampleMessage)

				break
			}
		}
	}

	key := strings.ToLower(sample)
	if prev, ok := exact[key]; ok {
		if prev.intent != in.Name {
			l.add(Error, locale, in.Name, "sample %q is also a sample of %s", sample, prev.intent)
		}
	} else {
		exact[key] = sampleOwner{intent: in.Name, pattern: pattern}
	}

	if len(refs) == 0 {
		return
	}

	carrier := slotRef.ReplaceAllString(key, "{}")
	if prev, ok := carriers[carrier]; ok && prev.intent != in.Name {
		l.add(Warning, locale, in.Name, "sample %q has the same wording as a %s sample; Alexa will choose between them by slot value alone", sample, prev.intent)
	} else if !ok {
		carriers[carrier] = sampleOwner{intent: in.Name, pattern: pattern}
	}
}

func (l *linter) counts(locale string, in Intent, samples []string) {
	if IsBuiltin(in.Name) {
		return
	}

	switch {
	case len(samples) < minSamples:
		l.add(Error, locale, in.Name, "custom intent has no samples")
	case len(samples) < recommendedSamples:
		l.add(Warning, locale, in.Name, "only %d samples; aim for at least %d", len(samples), recommendedSamples)
	}

	for _, slot := range in.Slots {
		ref := "{" + slot.Name + "}"
		found := false

		for _, s := range samples {
			if strings.Contains(s, ref) {
				found = true

				break
			}
		}

		if !found && len(samples) > 0 {
			l.add(Warning, locale, in.Name, "slot %s is never used in a sample", slot.Name)
		}
	}
}

// responses checks each language's messages have the reference language's placeholders, and each locale override targets a real ID. A message a language lacks is only a warning: translations arrive from Crowdin over time, and the reference language is spoken until then.
func (l *linter) responses() {
	ref, ok := l.src.Languages[ReferenceLanguage]
	if !ok {
		l.add(Error, "", "", "reference language %q is missing", ReferenceLanguage)

		return
	}

	for name, lang := range l.src.Languages {
		if name == ReferenceLanguage {
			continue
		}

		for id, text := range ref.Responses {
			other, ok := lang.Responses[id]
			if !ok {
				l.add(Warning, "lang/"+name, "", "response %s is not translated yet; %s is spoken instead", id, ReferenceLanguage)

				continue
			}

			if a, b := placeholders(text), placeholders(other); a != b {
				l.add(Error, "lang/"+name, "", "response %s has placeholders %s, expected %s", id, b, a)
			}
		}

		for id := range lang.Responses {
			if _, ok := ref.Responses[id]; !ok {
				l.add(Warning, "lang/"+name, "", "response %s is not in the %s reference", id, ReferenceLanguage)
			}
		}
	}

	for _, locale := range l.src.LocaleNames() {
		loc := l.src.Locales[locale]
		base := l.src.Languages[loc.Language].Responses

		for id, text := range loc.Responses {
			orig, ok := base[id]
			if !ok {
				l.add(Error, locale, "", "response override %s does not exist in lang/%s", id, loc.Language)

				continue
			}

			if a, b := placeholders(orig), placeholders(text); a != b {
				l.add(Error, locale, "", "response override %s has placeholders %s, expected %s", id, b, a)
			}
		}
	}
}

func placeholders(s string) string {
	matches := placeholderRef.FindAllStringSubmatch(s, -1)
	names := make([]string, 0, len(matches))

	for _, m := range matches {
		names = append(names, m[1])
	}

	sort.Strings(names)

	return "[" + strings.Join(names, ", ") + "]"
}
