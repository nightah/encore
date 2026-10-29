package skillmodel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintModelRules(t *testing.T) {
	tests := map[string]struct {
		mutate func(*Sources)
		want   string
	}{
		"slot type declared twice": {
			mutate: func(s *Sources) { s.Model.Types = append(s.Model.Types, s.Model.Types[0]) },
			want:   "error: slot type KODI_ROOM is declared twice",
		},
		"intent declared twice": {
			mutate: func(s *Sources) { s.Model.Intents = append(s.Model.Intents, Intent{Name: "ViewRoom"}) },
			want:   "error: ViewRoom: intent is declared twice",
		},
		"unknown content group": {
			mutate: func(s *Sources) { s.Model.Intents[1].Content = "podcasts" },
			want:   `error: WatchMovie: content "podcasts" is not one of movies, tv or music`,
		},
		"slot declared twice": {
			mutate: func(s *Sources) {
				s.Model.Intents[1].Slots = append(s.Model.Intents[1].Slots, Slot{Name: "Movie", Type: "AMAZON.Movie"})
			},
			want: "error: WatchMovie: slot Movie is declared twice",
		},
		"undeclared slot type": {
			mutate: func(s *Sources) { s.Model.Intents[2].Slots[0].Type = "KODI_ZONE" },
			want:   "error: ViewRoom: slot Room uses undeclared type KODI_ZONE",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			src := load(t, nil)
			tt.mutate(src)

			findings := Lint(src)
			assert.Contains(t, messages(findings, Error), tt.want)
			assert.True(t, HasErrors(findings))
		})
	}
}

func TestLintLocaleRules(t *testing.T) {
	tests := map[string]struct {
		locale string
		want   string
	}{
		"unknown language":         {locale: "language: xx\nskill_name: Encore\ninvocation: encore test\n", want: `error: en-US: unknown language "xx"`},
		"missing skill name":       {locale: "language: en\ninvocation: encore test\n", want: "error: en-US: skill_name is required"},
		"uppercase invocation":     {locale: "language: en\nskill_name: Encore\ninvocation: Encore Test\n", want: "must be lowercase letters"},
		"digits in invocation":     {locale: "language: en\nskill_name: Encore\ninvocation: encore 2\n", want: "must be lowercase letters"},
		"empty invocation":         {locale: "language: en\nskill_name: Encore\n", want: `invocation name "" must be lowercase letters`},
		"trailing period":          {locale: "language: en\nskill_name: Encore\ninvocation: encore.\n", want: "must be lowercase letters"},
		"reserved word, any place": {locale: "language: en\nskill_name: Encore\ninvocation: my echo remote\n", want: `contains the reserved word "echo"`},
		"synonyms for an unknown value": {
			locale: "language: en\nskill_name: Encore\ninvocation: encore test\ntypes:\n  KODI_ROOM:\n    attic: [loft]\n",
			want:   "error: en-US: synonyms given for value attic of slot type KODI_ROOM, which has no value with that ID",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := messages(Lint(load(t, map[string]string{"locales/en-US.yaml": tt.locale})), Error)
			assert.True(t, containsSubstring(got, tt.want), "want %q, got %q", tt.want, got)
		})
	}
}

func TestLintChecksTheKodiInvocation(t *testing.T) {
	got := messages(Lint(load(t, map[string]string{"locales/en-US.yaml": "language: en\nskill_name: Encore\ninvocation: encore test\nkodi_invocation: kodi 2\n"})), Error)
	assert.True(t, containsSubstring(got, `invocation name "kodi 2" must be lowercase letters`), "got %q", got)
}

func TestLintAcceptsValidInvocations(t *testing.T) {
	for _, inv := range []string{"encore test", "kodi's remote", "t.v. remote", "my reopen box"} {
		findings := Lint(load(t, map[string]string{"locales/en-US.yaml": "language: en\nskill_name: Encore\ninvocation: " + inv + "\n"}))
		assert.Empty(t, messages(findings, Error), inv)
		assert.False(t, containsSubstring(messages(findings, Warning), "one-word"), "%s has more than one word", inv)
	}
}

func TestLintOrdersErrorsBeforeWarningsInOnePlace(t *testing.T) {
	// The one-word warning is found before the missing skill name, so only sorting puts the error first.
	src := load(t, map[string]string{"locales/en-US.yaml": "language: en\ninvocation: encore\n"})

	var got []string

	for _, f := range Lint(src) {
		if f.Locale == "en-US" && f.Intent == "" {
			got = append(got, f.String())
		}
	}

	assert.Equal(t, []string{
		"error: en-US: skill_name is required",
		`warning: en-US: one-word invocation name "encore" is fine for a private skill but would need brand proof to certify`,
	}, got)
}

func TestLintSharedWordingNamesTheFirstIntent(t *testing.T) {
	src := load(t, map[string]string{"locales/en-US.yaml": "language: en\nskill_name: Encore\ninvocation: encore test\nutterances:\n  ViewRoom:\n    - \"put on {Room}\"\n"})
	src.Model.Intents = append(src.Model.Intents, Intent{Name: "ViewGenre", Slots: []Slot{{Name: "Genre", Type: "KODI_ROOM"}}})

	lang := src.Languages["en"]
	lang.Utterances["ViewGenre"] = []string{"put on {Genre}"}
	src.Languages["en"] = lang

	shared := map[string]string{}

	for _, f := range Lint(src) {
		if f.Locale == "en-US" && strings.Contains(f.Message, "same wording") {
			shared[f.Intent] = f.Message
		}
	}

	assert.Equal(t, map[string]string{
		"ViewRoom":  `sample "put on {Room}" has the same wording as a WatchMovie sample; Alexa will choose between them by slot value alone`,
		"ViewGenre": `sample "put on {Genre}" has the same wording as a WatchMovie sample; Alexa will choose between them by slot value alone`,
	}, shared)
}

func TestLintUnknownLanguageSkipsSampleChecks(t *testing.T) {
	src := load(t, map[string]string{"locales/en-US.yaml": "language: xx\nskill_name: Encore\ninvocation: encore test\n"})

	var us []Finding

	for _, f := range Lint(src) {
		if f.Locale == "en-US" {
			us = append(us, f)
		}
	}

	require.Len(t, us, 1, "a locale without a language has nothing else to check")
}

func TestLintResponseRules(t *testing.T) {
	tests := map[string]struct {
		overrides map[string]string
		want      string
		sev       Severity
	}{
		"response missing from the reference": {
			overrides: map[string]string{
				"lang/de/utterances.yaml": "{}",
				"lang/de/responses.yaml":  "playback.playing: \"Spiele {title}.\"\nplayback.extra: \"Extra.\"\n",
			},
			want: "warning: lang/de: response playback.extra is not in the en reference", sev: Warning,
		},
		"override placeholder mismatch": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "responses:\n  playback.playing: \"Now playing.\"\n"},
			want:      "error: en-US: response override playback.playing has placeholders [], expected [title]", sev: Error,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, messages(Lint(load(t, tt.overrides)), tt.sev), tt.want)
		})
	}
}

func TestLintOverrideKeepingPlaceholdersIsFine(t *testing.T) {
	src := load(t, map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "responses:\n  playback.playing: \"Now playing {title}, enjoy.\"\n"})
	assert.Empty(t, messages(Lint(src), Error))
}

func TestLintWithoutReferenceLanguage(t *testing.T) {
	src := load(t, nil)
	src.Languages["fr"] = src.Languages["en"]
	delete(src.Languages, "en")

	for name, loc := range src.Locales {
		loc.Language = "fr"
		src.Locales[name] = loc
	}

	assert.Contains(t, messages(Lint(src), Error), `error: reference language "en" is missing`)
}

func TestLintSampleCounts(t *testing.T) {
	src := load(t, nil)
	src.Model.Intents = append(src.Model.Intents, Intent{Name: "Rewind", Slots: []Slot{{Name: "Amount", Type: "AMAZON.DURATION"}}})

	findings := Lint(src)
	assert.Contains(t, messages(findings, Error), "error: en-AU/Rewind: custom intent has no samples")
	assert.NotContains(t, strings.Join(messages(findings, Warning), "\n"), "Rewind: slot Amount", "an intent without samples is reported once, not per slot")

	lang := src.Languages["en"]
	lang.Utterances["Rewind"] = []string{"rewind", "go back"}
	src.Languages["en"] = lang

	warnings := messages(Lint(src), Warning)
	assert.Contains(t, warnings, "warning: en-AU/Rewind: only 2 samples; aim for at least 5")
	assert.Contains(t, warnings, "warning: en-AU/Rewind: slot Amount is never used in a sample")
}

func TestLintBuiltinIntentsNeedNoSamples(t *testing.T) {
	for _, f := range Lint(load(t, nil)) {
		assert.NotEqual(t, "AMAZON.StopIntent", f.Intent, f.String())
	}
}

func TestLintFindingsAreOrdered(t *testing.T) {
	src := load(t, map[string]string{"locales/en-US.yaml": "language: en\nskill_name: Encore\ninvocation: encore\nutterances:\n  Rewind:\n    - \"rewind\"\n"})

	findings := Lint(src)
	require.NotEmpty(t, findings)

	for i := 1; i < len(findings); i++ {
		a, b := findings[i-1], findings[i]
		switch {
		case a.Locale != b.Locale:
			assert.Less(t, a.Locale, b.Locale)
		case a.Intent != b.Intent:
			assert.Less(t, a.Intent, b.Intent)
		default:
			assert.GreaterOrEqual(t, a.Severity, b.Severity, "errors come before warnings for the same place: %s, %s", a, b)
		}
	}
}

func TestFindingString(t *testing.T) {
	for want, f := range map[string]Finding{
		"error: slot type X is declared twice": {Severity: Error, Message: "slot type X is declared twice"},
		"warning: en-AU: one-word":             {Severity: Warning, Locale: "en-AU", Message: "one-word"},
		"error: Watch: bad":                    {Severity: Error, Intent: "Watch", Message: "bad"},
		"error: en-AU/Watch: bad":              {Severity: Error, Locale: "en-AU", Intent: "Watch", Message: "bad"},
	} {
		assert.Equal(t, want, f.String())
	}
}

func TestHasErrors(t *testing.T) {
	assert.False(t, HasErrors(nil))
	assert.False(t, HasErrors([]Finding{{Severity: Warning}, {Severity: Warning}}))
	assert.True(t, HasErrors([]Finding{{Severity: Warning}, {Severity: Error}}))
}

func TestPlaceholders(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "[]",
		"Playing {title}.":             "[title]",
		"{b} then {a}":                 "[a, b]",
		"{a} and {a}":                  "[a, a]",
		"{1abc} {} { spaced } {_ok9}":  "[_ok9]",
		"Lecture de {titre} à {salle}": "[salle, titre]",
	} {
		assert.Equal(t, want, placeholders(in), in)
	}
}

// lintLocale lints a test locale, xx-XX, of language with the given invocation name and, for a language other than English, utterances, returning the locale's findings.
func lintLocale(t *testing.T, language, invocation, utterances string) (errs, warnings []string) {
	t.Helper()

	files := map[string]string{"locales/xx-XX.yaml": "language: " + language + "\nskill_name: Encore\ninvocation: \"" + invocation + "\"\n"}
	if language != ReferenceLanguage {
		files["lang/"+language+"/utterances.yaml"] = utterances
		files["lang/"+language+"/responses.yaml"] = "{}"
	}

	for _, f := range Lint(load(t, files)) {
		switch {
		case f.Locale != "xx-XX":
		case f.Severity == Error:
			errs = append(errs, f.Message)
		default:
			warnings = append(warnings, f.Message)
		}
	}

	return errs, warnings
}

func TestLintInvocationPerScript(t *testing.T) {
	tests := []struct {
		language, invocation string
		// want is part of the error the name must raise; empty means it must raise none.
		want string
	}{
		// Latin script: lowercase letters with their accents, spaces, apostrophes and periods.
		{"en", "encore test", ""},
		{"en", "t.v. remote", ""},
		{"en", "Encore Test", "must be lowercase letters, spaces, apostrophes or periods"},
		{"en", "encore 2", "must be lowercase letters, spaces, apostrophes or periods"},
		{"en", "encore.", "must be lowercase letters, spaces, apostrophes or periods"},
		{"en", "", `invocation name "" must be lowercase letters`},
		{"de", "fernbedienung für kodi", ""},
		{"de", "große leinwand", ""},
		{"pt", "sessão de pipoca", ""},
		{"pt", "pipoca já", ""},
		{"pt", "Pipoca Já", "must be lowercase letters"},
		{"fr", "télé kodi", ""},
		{"fr", "кино пульт", "must be lowercase letters"},
		{"nl", "n. o. s. kijken", ""},
		// Japanese: kana and kanji, ー, and acronyms as lowercase letters each followed by a space.
		{"ja", "アンコール", ""},
		{"ja", "映画のリモコン", ""},
		{"ja", "えんこーる", ""},
		{"ja", "a b c ニュース", ""},
		{"ja", "encore", "must be Hiragana, Katakana or Kanji"},
		{"ja", "ab ニュース", "must be Hiragana, Katakana or Kanji"},
		{"ja", "アンコール a", "must be Hiragana, Katakana or Kanji"},
		{"ja", "アンコール 映画", "must be Hiragana, Katakana or Kanji"},
		{"ja", "アンコール1", "must be Hiragana, Katakana or Kanji"},
		// Hindi: Devanagari words with their vowel signs, and lowercase Latin for English words.
		{"hi", "एनकोर", ""},
		{"hi", "मेरा टीवी", ""},
		{"hi", "कोडी remote", ""},
		{"hi", "encore test", ""},
		{"hi", "एनकोर Remote", "must be Devanagari words"},
		{"hi", "एनकोर २", "must be Devanagari words"},
		{"hi", "एनकोर  रिमोट", "must be Devanagari words"},
		// Arabic: Arabic words, and lowercase Latin for loanwords.
		{"ar", "انكور", ""},
		{"ar", "kodi ريموت", ""},
		{"ar", "انكور ٢", "must be Arabic words"},
		{"ar", "Kodi", "must be Arabic words"},
		{"ar", "ريموت ", "must be Arabic words"},
	}

	for _, tt := range tests {
		t.Run(tt.language+"/"+tt.invocation, func(t *testing.T) {
			errs, _ := lintLocale(t, tt.language, tt.invocation, "{}")
			if tt.want == "" {
				assert.False(t, containsSubstring(errs, "invocation name"), "got %q", errs)
			} else {
				assert.True(t, containsSubstring(errs, tt.want), "want %q, got %q", tt.want, errs)
			}
		})
	}
}

func TestLintReservedWordsPerLanguage(t *testing.T) {
	tests := []struct {
		language, invocation string
	}{
		{"en", "my echo remote"},
		{"de", "alexa kino"},
		{"fr", "appli kodi"},
		{"fr", "application télé"},
		{"pt", "abrir filmes"},
		{"pt", "aplicativo kodi"},
		{"pt", "começar cinema"},
		{"nl", "speel films"},
		{"nl", "kodi skill"},
		{"ja", "アレクサリモコン"},
		{"ja", "映画を開始"},
		{"ja", "アマゾン映画"},
		{"hi", "अलेक्सा रिमोट"},
		{"hi", "कोडी app"},
		// Hindi's nukta is reserved whether written as one code point or two.
		{"hi", "ऐमेज़ोन टीवी"},
		{"hi", "ऐमेज़ोन टीवी"},
		{"ar", "افتحي انكور"},
		{"ar", "امازون ريموت"},
	}

	for _, tt := range tests {
		t.Run(tt.language+"/"+tt.invocation, func(t *testing.T) {
			errs, _ := lintLocale(t, tt.language, tt.invocation, "{}")
			assert.True(t, containsSubstring(errs, "contains the reserved word"), "got %q", errs)
		})
	}

	for _, tt := range []struct{ language, invocation string }{{"en", "speel films"}, {"de", "abrir filmes"}, {"ja", "アンコール"}, {"fr", "kodi"}} {
		errs, _ := lintLocale(t, tt.language, tt.invocation, "{}")
		assert.False(t, containsSubstring(errs, "reserved word"), "%s %q is reserved only in other languages: %q", tt.language, tt.invocation, errs)
	}
}

func TestLintOneWordInvocationWarning(t *testing.T) {
	for _, tt := range []struct {
		language, invocation string
		warn                 bool
	}{
		{"en", "encore", true},
		{"en", "encore test", false},
		{"hi", "एनकोर", true},
		{"ar", "انكور", true},
		{"ja", "アンコール", false},
	} {
		_, warnings := lintLocale(t, tt.language, tt.invocation, "{}")
		assert.Equal(t, tt.warn, containsSubstring(warnings, "one-word invocation"), "%s %q: %q", tt.language, tt.invocation, warnings)
	}
}

func TestLintSamplesPerScript(t *testing.T) {
	tests := []struct {
		language, sample string
		// want is part of the error the sample must raise; empty means it must raise none.
		want string
	}{
		{"hi", "{Movie} चलाओ", ""},
		{"hi", "फ़िल्म {Movie} दिखाओ", ""},
		{"hi", "play करो {Movie}", ""},
		{"hi", "{Movie} चलाओ २", "spell out numbers"},
		{"hi", "{Movie}को चलाओ", "joins slot {Movie} to other text"},
		{"ja", "{Movie}を再生して", ""},
		{"ja", "映画の{Movie}を見せて", ""},
		{"ja", "{Movie}を2回再生", "spell out numbers"},
		{"de", "spiele {Movie}s", "joins slot {Movie} to other text"},
		{"pt", "assistir {Movie}", ""},
		{"ar", "شغل {Movie}", ""},
		{"ar", "شغل{Movie}", "joins slot {Movie} to other text"},
	}

	for _, tt := range tests {
		t.Run(tt.language+"/"+tt.sample, func(t *testing.T) {
			errs, _ := lintLocale(t, tt.language, "encore test", "WatchMovie:\n  play: \""+tt.sample+"\"\n")

			var samples []string

			for _, e := range errs {
				if strings.HasPrefix(e, "sample ") {
					samples = append(samples, e)
				}
			}

			if tt.want == "" {
				assert.Empty(t, samples)
			} else {
				assert.True(t, containsSubstring(samples, tt.want), "want %q, got %q", tt.want, samples)
			}
		})
	}
}
