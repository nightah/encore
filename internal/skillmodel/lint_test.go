package skillmodel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintModelRules(t *testing.T) {
	testCases := []struct {
		name     string
		mutate   func(*Sources)
		expected string
	}{
		{name: "ShouldRejectSlotTypeDeclaredTwice", mutate: func(s *Sources) { s.Model.Types = append(s.Model.Types, s.Model.Types[0]) }, expected: "error: slot type KODI_ROOM is declared twice"},
		{name: "ShouldRejectIntentDeclaredTwice", mutate: func(s *Sources) { s.Model.Intents = append(s.Model.Intents, Intent{Name: "ViewRoom"}) }, expected: "error: ViewRoom: intent is declared twice"},
		{name: "ShouldRejectUnknownContentGroup", mutate: func(s *Sources) { s.Model.Intents[1].Content = "podcasts" }, expected: `error: WatchMovie: content "podcasts" is not one of movies, tv, music or livetv`},
		{name: "ShouldRejectSlotDeclaredTwice", mutate: func(s *Sources) {
			s.Model.Intents[1].Slots = append(s.Model.Intents[1].Slots, Slot{Name: "Movie", Type: "AMAZON.Movie"})
		}, expected: "error: WatchMovie: slot Movie is declared twice"},
		{name: "ShouldRejectUndeclaredSlotType", mutate: func(s *Sources) { s.Model.Intents[2].Slots[0].Type = "KODI_ZONE" }, expected: "error: ViewRoom: slot Room uses undeclared type KODI_ZONE"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			src := load(t, nil)
			tc.mutate(src)

			findings := Lint(src)
			assert.Contains(t, messages(findings, Error), tc.expected)
			assert.True(t, HasErrors(findings))
		})
	}
}

func TestLintLocaleRules(t *testing.T) {
	testCases := []struct {
		name     string
		locale   string
		expected string
	}{
		{name: "ShouldRejectUnknownLanguage", locale: "language: xx\nskill_name: Encore\ninvocation: encore test\n", expected: `error: en-US: unknown language "xx"`},
		{name: "ShouldRequireSkillName", locale: "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\ninvocation: encore test\n", expected: "error: en-US: skill_name is required"},
		{name: "ShouldRejectUppercaseInvocation", locale: "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: Encore Test\n", expected: "must be lowercase letters"},
		{name: "ShouldRejectDigitsInInvocation", locale: "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore 2\n", expected: "must be lowercase letters"},
		{name: "ShouldRejectEmptyInvocation", locale: "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\n", expected: `invocation name "" must be lowercase letters`},
		{name: "ShouldRejectTrailingPeriod", locale: "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore.\n", expected: "must be lowercase letters"},
		{name: "ShouldRejectReservedWordInAnyPlace", locale: "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: my echo remote\n", expected: `contains the reserved word "echo"`},
		{name: "ShouldRequireLaunchPhrases", locale: "language: en\nskill_name: Encore\ninvocation: encore test\n", expected: "error: en-US: launch.open is required"},
		{name: "ShouldRequireSkillLaunchPhrase", locale: "language: en\nlaunch:\n  open: open {invocation}\nskill_name: Encore\ninvocation: encore test\n", expected: "error: en-US: launch.skill is required"},
		{name: "ShouldRejectLaunchWithoutTheName", locale: "language: en\nlaunch:\n  open: open {invocation}\n  also: [open it]\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore test\n", expected: `error: en-US: launch phrase "open it" must name {invocation} once`},
		{name: "ShouldRejectLaunchNamingItTwice", locale: "language: en\nlaunch:\n  open: open {invocation} {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore test\n", expected: `launch phrase "open {invocation} {invocation}" must name {invocation} once`},
		{name: "ShouldRejectSynonymsForAnUnknownValue", locale: "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore test\ntypes:\n  KODI_ROOM:\n    attic: [loft]\n", expected: "error: en-US: synonyms given for value attic of slot type KODI_ROOM, which has no value with that ID"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := messages(Lint(load(t, map[string]string{"locales/en-US.yaml": tc.locale})), Error)
			assert.True(t, containsSubstring(actual, tc.expected), "want %q, got %q", tc.expected, actual)
		})
	}
}

func TestLintChecksTheKodiInvocation(t *testing.T) {
	got := messages(Lint(load(t, map[string]string{"locales/en-US.yaml": "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore test\nkodi_invocation: kodi 2\n"})), Error)
	assert.True(t, containsSubstring(got, `invocation name "kodi 2" must be lowercase letters`), "got %q", got)
}

func TestLintAcceptsValidInvocations(t *testing.T) {
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldAcceptTwoWords", have: "encore test"},
		{name: "ShouldAcceptApostrophe", have: "kodi's remote"},
		{name: "ShouldAcceptPeriods", have: "t.v. remote"},
		{name: "ShouldAcceptReservedWordInsideAWord", have: "my reopen box"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Lint(load(t, map[string]string{"locales/en-US.yaml": "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: " + tc.have + "\n"}))
			assert.Empty(t, messages(findings, Error))
			assert.False(t, containsSubstring(messages(findings, Warning), "one-word"), "%s has more than one word", tc.have)
		})
	}
}

func TestLintOrdersErrorsBeforeWarningsInOnePlace(t *testing.T) {
	// The one-word warning is found before the missing skill name, so only sorting puts the error first.
	src := load(t, map[string]string{"locales/en-US.yaml": "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\ninvocation: encore\n"})

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
	src := load(t, map[string]string{"locales/en-US.yaml": "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore test\nutterances:\n  ViewRoom:\n    - \"put on {Room}\"\n"})
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
	testCases := []struct {
		name      string
		overrides map[string]string
		expected  string
		sev       Severity
	}{
		{name: "ShouldWarnResponseMissingFromTheReference", overrides: map[string]string{"lang/de/utterances.yaml": "{}", "lang/de/responses.yaml": "playback.playing: \"Spiele {title}.\"\nplayback.extra: \"Extra.\"\n"}, expected: "warning: lang/de: response playback.extra is not in the en reference", sev: Warning},
		{name: "ShouldRejectOverridePlaceholderMismatch", overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "responses:\n  playback.playing: \"Now playing.\"\n"}, expected: "error: en-US: response override playback.playing has placeholders [], expected [title]", sev: Error},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, messages(Lint(load(t, tc.overrides)), tc.sev), tc.expected)
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
	src := load(t, map[string]string{"locales/en-US.yaml": "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore\nutterances:\n  Rewind:\n    - \"rewind\"\n"})

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
	testCases := []struct {
		name     string
		have     Finding
		expected string
	}{
		{name: "ShouldFormatMessageOnly", have: Finding{Severity: Error, Message: "slot type X is declared twice"}, expected: "error: slot type X is declared twice"},
		{name: "ShouldFormatLocale", have: Finding{Severity: Warning, Locale: "en-AU", Message: "one-word"}, expected: "warning: en-AU: one-word"},
		{name: "ShouldFormatIntent", have: Finding{Severity: Error, Intent: "Watch", Message: "bad"}, expected: "error: Watch: bad"},
		{name: "ShouldFormatLocaleAndIntent", have: Finding{Severity: Error, Locale: "en-AU", Intent: "Watch", Message: "bad"}, expected: "error: en-AU/Watch: bad"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.String())
		})
	}
}

func TestHasErrors(t *testing.T) {
	testCases := []struct {
		name     string
		have     []Finding
		expected bool
	}{
		{name: "ShouldHaveNoErrorsWithoutFindings"},
		{name: "ShouldHaveNoErrorsWithOnlyWarnings", have: []Finding{{Severity: Warning}, {Severity: Warning}}},
		{name: "ShouldHaveErrorsWithAnError", have: []Finding{{Severity: Warning}, {Severity: Error}}, expected: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, HasErrors(tc.have))
		})
	}
}

func TestPlaceholders(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldListNoneForEmptyText", expected: "[]"},
		{name: "ShouldListPlaceholder", have: "Playing {title}.", expected: "[title]"},
		{name: "ShouldSortPlaceholders", have: "{b} then {a}", expected: "[a, b]"},
		{name: "ShouldKeepRepeatedPlaceholders", have: "{a} and {a}", expected: "[a, a]"},
		{name: "ShouldIgnoreInvalidNames", have: "{1abc} {} { spaced } {_ok9}", expected: "[_ok9]"},
		{name: "ShouldListPlaceholdersInAccentedText", have: "Lecture de {titre} à {salle}", expected: "[salle, titre]"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, placeholders(tc.have))
		})
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
	testCases := []struct {
		name     string
		language string
		have     string
		// err is part of the error the name must raise; empty means it must raise none.
		err string
	}{
		// Latin script: lowercase letters with their accents, spaces, apostrophes and periods.
		{name: "ShouldAcceptEnglishInvocation", language: "en", have: "encore test"},
		{name: "ShouldAcceptEnglishInvocationWithPeriods", language: "en", have: "t.v. remote"},
		{name: "ShouldRejectUppercaseEnglishInvocation", language: "en", have: "Encore Test", err: "must be lowercase letters, spaces, apostrophes or periods"},
		{name: "ShouldRejectDigitsInEnglishInvocation", language: "en", have: "encore 2", err: "must be lowercase letters, spaces, apostrophes or periods"},
		{name: "ShouldRejectTrailingPeriodInEnglishInvocation", language: "en", have: "encore.", err: "must be lowercase letters, spaces, apostrophes or periods"},
		{name: "ShouldRejectEmptyEnglishInvocation", language: "en", err: `invocation name "" must be lowercase letters`},
		{name: "ShouldAcceptGermanUmlaut", language: "de", have: "fernbedienung für kodi"},
		{name: "ShouldAcceptGermanEszett", language: "de", have: "große leinwand"},
		{name: "ShouldAcceptPortugueseTilde", language: "pt", have: "sessão de pipoca"},
		{name: "ShouldAcceptPortugueseAcute", language: "pt", have: "pipoca já"},
		{name: "ShouldRejectUppercasePortugueseInvocation", language: "pt", have: "Pipoca Já", err: "must be lowercase letters"},
		{name: "ShouldAcceptFrenchAccents", language: "fr", have: "télé kodi"},
		{name: "ShouldRejectCyrillicInFrenchInvocation", language: "fr", have: "кино пульт", err: "must be lowercase letters"},
		{name: "ShouldAcceptDutchInitials", language: "nl", have: "n. o. s. kijken"},
		// Japanese: kana and kanji, ー, and acronyms as lowercase letters each followed by a space.
		{name: "ShouldAcceptJapaneseKatakana", language: "ja", have: "アンコール"},
		{name: "ShouldAcceptJapaneseKanjiAndKana", language: "ja", have: "映画のリモコン"},
		{name: "ShouldAcceptJapaneseHiraganaWithLongVowelMark", language: "ja", have: "えんこーる"},
		{name: "ShouldAcceptJapaneseSpacedAcronym", language: "ja", have: "a b c ニュース"},
		{name: "ShouldRejectLatinWordInJapaneseInvocation", language: "ja", have: "encore", err: "must be Hiragana, Katakana or Kanji"},
		{name: "ShouldRejectUnspacedAcronymInJapaneseInvocation", language: "ja", have: "ab ニュース", err: "must be Hiragana, Katakana or Kanji"},
		{name: "ShouldRejectTrailingLetterInJapaneseInvocation", language: "ja", have: "アンコール a", err: "must be Hiragana, Katakana or Kanji"},
		{name: "ShouldRejectSpaceBetweenJapaneseWords", language: "ja", have: "アンコール 映画", err: "must be Hiragana, Katakana or Kanji"},
		{name: "ShouldRejectDigitInJapaneseInvocation", language: "ja", have: "アンコール1", err: "must be Hiragana, Katakana or Kanji"},
		// Hindi: Devanagari words with their vowel signs, and lowercase Latin for English words.
		{name: "ShouldAcceptHindiDevanagari", language: "hi", have: "एनकोर"},
		{name: "ShouldAcceptHindiWordsWithVowelSigns", language: "hi", have: "मेरा टीवी"},
		{name: "ShouldAcceptHindiWithLowercaseEnglishWord", language: "hi", have: "कोडी remote"},
		{name: "ShouldAcceptLowercaseLatinInHindiInvocation", language: "hi", have: "encore test"},
		{name: "ShouldRejectUppercaseLatinInHindiInvocation", language: "hi", have: "एनकोर Remote", err: "must be Devanagari words"},
		{name: "ShouldRejectDevanagariDigitInHindiInvocation", language: "hi", have: "एनकोर २", err: "must be Devanagari words"},
		{name: "ShouldRejectDoubleSpaceInHindiInvocation", language: "hi", have: "एनकोर  रिमोट", err: "must be Devanagari words"},
		// Arabic: Arabic words, and lowercase Latin for loanwords.
		{name: "ShouldAcceptArabicInvocation", language: "ar", have: "انكور"},
		{name: "ShouldAcceptArabicWithLowercaseLoanword", language: "ar", have: "kodi ريموت"},
		{name: "ShouldRejectArabicDigitInArabicInvocation", language: "ar", have: "انكور ٢", err: "must be Arabic words"},
		{name: "ShouldRejectUppercaseLatinInArabicInvocation", language: "ar", have: "Kodi", err: "must be Arabic words"},
		{name: "ShouldRejectTrailingSpaceInArabicInvocation", language: "ar", have: "ريموت ", err: "must be Arabic words"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs, _ := lintLocale(t, tc.language, tc.have, "{}")
			if tc.err == "" {
				assert.False(t, containsSubstring(errs, "invocation name"), "got %q", errs)
			} else {
				assert.True(t, containsSubstring(errs, tc.err), "want %q, got %q", tc.err, errs)
			}
		})
	}
}

func TestLintReservedWordsPerLanguage(t *testing.T) {
	testCases := []struct {
		name     string
		language string
		have     string
		expected bool
	}{
		{name: "ShouldRejectEchoInEnglish", language: "en", have: "my echo remote", expected: true},
		{name: "ShouldRejectAlexaInGerman", language: "de", have: "alexa kino", expected: true},
		{name: "ShouldRejectAppliInFrench", language: "fr", have: "appli kodi", expected: true},
		{name: "ShouldRejectApplicationInFrench", language: "fr", have: "application télé", expected: true},
		{name: "ShouldRejectAbrirInPortuguese", language: "pt", have: "abrir filmes", expected: true},
		{name: "ShouldRejectAplicativoInPortuguese", language: "pt", have: "aplicativo kodi", expected: true},
		{name: "ShouldRejectComecarInPortuguese", language: "pt", have: "começar cinema", expected: true},
		{name: "ShouldRejectSpeelInDutch", language: "nl", have: "speel films", expected: true},
		{name: "ShouldRejectSkillInDutch", language: "nl", have: "kodi skill", expected: true},
		{name: "ShouldRejectAlexaInJapanese", language: "ja", have: "アレクサリモコン", expected: true},
		{name: "ShouldRejectStartInJapanese", language: "ja", have: "映画を開始", expected: true},
		{name: "ShouldRejectAmazonInJapanese", language: "ja", have: "アマゾン映画", expected: true},
		{name: "ShouldRejectAlexaInHindi", language: "hi", have: "अलेक्सा रिमोट", expected: true},
		{name: "ShouldRejectAppInHindi", language: "hi", have: "कोडी app", expected: true},
		// Hindi's nukta is reserved whether written as one code point or two.
		{name: "ShouldRejectAmazonWithOneCodePointNuktaInHindi", language: "hi", have: "ऐमेज़ोन टीवी", expected: true},
		{name: "ShouldRejectAmazonWithTwoCodePointNuktaInHindi", language: "hi", have: "ऐमेज़ोन टीवी", expected: true},
		{name: "ShouldRejectOpenInArabic", language: "ar", have: "افتحي انكور", expected: true},
		{name: "ShouldRejectAmazonInArabic", language: "ar", have: "امازون ريموت", expected: true},
		{name: "ShouldAllowDutchReservedWordInEnglish", language: "en", have: "speel films"},
		{name: "ShouldAllowPortugueseReservedWordInGerman", language: "de", have: "abrir filmes"},
		{name: "ShouldAllowEncoreInJapanese", language: "ja", have: "アンコール"},
		{name: "ShouldAllowKodiInFrench", language: "fr", have: "kodi"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs, _ := lintLocale(t, tc.language, tc.have, "{}")
			if tc.expected {
				assert.True(t, containsSubstring(errs, "contains the reserved word"), "got %q", errs)
			} else {
				assert.False(t, containsSubstring(errs, "reserved word"), "reserved only in other languages: %q", errs)
			}
		})
	}
}

func TestLintOneWordInvocationWarning(t *testing.T) {
	testCases := []struct {
		name     string
		language string
		have     string
		expected bool
	}{
		{name: "ShouldWarnOneWordEnglishInvocation", language: "en", have: "encore", expected: true},
		{name: "ShouldNotWarnTwoWordEnglishInvocation", language: "en", have: "encore test"},
		{name: "ShouldWarnOneWordHindiInvocation", language: "hi", have: "एनकोर", expected: true},
		{name: "ShouldWarnOneWordArabicInvocation", language: "ar", have: "انكور", expected: true},
		{name: "ShouldNotWarnJapaneseInvocation", language: "ja", have: "アンコール"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, warnings := lintLocale(t, tc.language, tc.have, "{}")
			assert.Equal(t, tc.expected, containsSubstring(warnings, "one-word invocation"), "%q", warnings)
		})
	}
}

func TestLintSamplesPerScript(t *testing.T) {
	testCases := []struct {
		name     string
		language string
		have     string
		// err is part of the error the sample must raise; empty means it must raise none.
		err string
	}{
		{name: "ShouldAcceptHindiSample", language: "hi", have: "{Movie} चलाओ"},
		{name: "ShouldAcceptHindiSampleWithNukta", language: "hi", have: "फ़िल्म {Movie} दिखाओ"},
		{name: "ShouldAcceptHindiSampleWithEnglishWord", language: "hi", have: "play करो {Movie}"},
		{name: "ShouldRejectDevanagariDigitInHindiSample", language: "hi", have: "{Movie} चलाओ २", err: "spell out numbers"},
		{name: "ShouldRejectSlotJoinedToTextInHindiSample", language: "hi", have: "{Movie}को चलाओ", err: "joins slot {Movie} to other text"},
		{name: "ShouldAcceptJapaneseSample", language: "ja", have: "{Movie}を再生して"},
		{name: "ShouldAcceptJapaneseSampleWithTextBeforeSlot", language: "ja", have: "映画の{Movie}を見せて"},
		{name: "ShouldRejectDigitInJapaneseSample", language: "ja", have: "{Movie}を2回再生", err: "spell out numbers"},
		{name: "ShouldRejectSlotJoinedToTextInGermanSample", language: "de", have: "spiele {Movie}s", err: "joins slot {Movie} to other text"},
		{name: "ShouldAcceptPortugueseSample", language: "pt", have: "assistir {Movie}"},
		{name: "ShouldAcceptArabicSample", language: "ar", have: "شغل {Movie}"},
		{name: "ShouldRejectSlotJoinedToTextInArabicSample", language: "ar", have: "شغل{Movie}", err: "joins slot {Movie} to other text"},
		// Each word is in the language's script, or lowercase Latin where Amazon allows English words.
		{name: "ShouldRejectUppercaseLatinInHindiSample", language: "hi", have: "{Movie} F चलाओ", err: `has the word "F", which isn't Devanagari or lowercase Latin`},
		{name: "ShouldRejectArabicScriptInHindiSample", language: "hi", have: "{Movie} شغل", err: "which isn't Devanagari or lowercase Latin"},
		{name: "ShouldAcceptLowercaseLatinInArabicSample", language: "ar", have: "play {Movie}"},
		{name: "ShouldRejectDevanagariInArabicSample", language: "ar", have: "शुरू {Movie}", err: "which isn't Arabic or lowercase Latin"},
		{name: "ShouldAcceptLowercaseLatinInJapaneseSample", language: "ja", have: "{Movie}をplayして"},
		{name: "ShouldRejectUppercaseLatinInJapaneseSample", language: "ja", have: "{Movie}をPlayして", err: "which isn't Hiragana, Katakana, Kanji or lowercase Latin"},
		{name: "ShouldRejectHangulInJapaneseSample", language: "ja", have: "{Movie}를재생", err: "which isn't Hiragana, Katakana, Kanji or lowercase Latin"},
		{name: "ShouldAcceptGermanSample", language: "de", have: "spiele den film {Movie} ab"},
		{name: "ShouldRejectCapitalisedWordInGermanSample", language: "de", have: "spiele den Film {Movie} ab", err: `has the word "Film", which isn't lowercase Latin`},
		{name: "ShouldAcceptPortugueseSampleWithAccent", language: "pt", have: "toca o filme {Movie} já"},
		{name: "ShouldRejectCyrillicInPortugueseSample", language: "pt", have: "смотреть {Movie}", err: "which isn't lowercase Latin"},
		{name: "ShouldAcceptDutchSampleWithApostrophe", language: "nl", have: "zet 't journal op"},
		{name: "ShouldAcceptDutchSampleWithInitials", language: "nl", have: "speel r. en b."},
		{name: "ShouldAcceptFrenchSampleWithElision", language: "fr", have: "mets l'émission {Movie}"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs, _ := lintLocale(t, tc.language, "encore test", "WatchMovie:\n  play: \""+tc.have+"\"\n")

			var samples []string

			for _, e := range errs {
				if strings.HasPrefix(e, "sample ") {
					samples = append(samples, e)
				}
			}

			if tc.err == "" {
				assert.Empty(t, samples)
			} else {
				assert.True(t, containsSubstring(samples, tc.err), "want %q, got %q", tc.err, samples)
			}
		})
	}
}
