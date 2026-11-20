package docsgen

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
)

func loadSources(t *testing.T) *skillmodel.Sources {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	return src
}

func TestVoiceCommandsOnePagePerLanguage(t *testing.T) {
	src := loadSources(t)

	pages, err := VoiceCommands(src, i18n.FS)
	require.NoError(t, err)
	require.Len(t, pages, len(src.Languages))

	for lang := range src.Languages {
		require.Contains(t, pages, lang)
		assert.Contains(t, string(pages[lang]), GeneratedMarker, lang)
	}

	assert.Contains(t, string(pages["en"]), "title: English\n")
	assert.Contains(t, string(pages["en"]), "  order: 0\n", "English comes first")
	assert.Contains(t, string(pages["de"]), "title: German (Deutsch)\n")
	assert.Contains(t, string(pages["en"]), "`en-AU`", "the English page lists the English locales")
	assert.NotContains(t, string(pages["en"]), "`de-DE`")
}

func TestVoiceCommandsShowAnUnnamedLanguageByItsCode(t *testing.T) {
	src := loadSources(t)
	src.Languages["sv"] = src.Languages["en"]

	pages, err := VoiceCommands(src, i18n.FS)
	require.NoError(t, err)

	page := string(pages["sv"])
	assert.Contains(t, page, "title: sv\n")
	assert.Contains(t, page, "editUrl: "+CrowdinProject+"/sv\n")
}

func TestVoiceDataGroupsIntentsUnderTheirSections(t *testing.T) {
	src := &skillmodel.Sources{
		Model: skillmodel.Model{
			Intents: []skillmodel.Intent{
				{Name: "AMAZON.StopIntent"},
				{Name: "WatchMovie", Slots: []skillmodel.Slot{{Name: "Movie", Type: "MovieTitle"}}},
				{Name: "ListenAlbum"},
				{Name: "Unspoken"},
			},
			Types: []skillmodel.SlotType{{Name: "MovieTitle", Dynamic: true, Values: []skillmodel.SlotValue{{Value: "Dune"}}}, {Name: "Unused"}},
		},
		Languages: map[string]skillmodel.Language{"en": {
			Utterances: map[string][]string{"WatchMovie": {"watch {Movie}"}},
			Types:      map[string][]skillmodel.SlotValue{"MovieTitle": {{Value: "Alien"}}},
		}},
		Locales: map[string]skillmodel.Locale{
			"en-AU": {Language: "en", SkillName: "Encore", Invocation: "encore", Utterances: map[string][]string{"ListenAlbum": {"chuck on {Album}"}}},
			"de-DE": {Language: "de", SkillName: "Encore", Invocation: "encore"},
		},
	}

	data := voiceData(src, "en", map[string]string{"WatchMovie": "Movies", "ListenAlbum": "Music", "Unspoken": "Nothing"})

	assert.Equal(t, "English", data["Name"])
	assert.Equal(t, []VoiceLocale{{Locale: "en-AU", SkillName: "Encore", Invocation: "encore"}}, data["Locales"])
	assert.Equal(t, []string{"AMAZON.StopIntent"}, data["Builtins"], "built-ins without patterns are listed by name")

	sections, ok := data["Sections"].([]VoiceSection)
	require.True(t, ok)
	require.Len(t, sections, 2, "sections without spoken intents are dropped")
	assert.Equal(t, "Movies", sections[0].Title)
	assert.Equal(t, []string{"watch {Movie}"}, sections[0].Intents[0].Patterns)
	assert.Equal(t, "Music", sections[1].Title)
	assert.Equal(t, []LocalePatterns{{Locale: "en-AU", Patterns: []string{"chuck on {Album}"}}}, sections[1].Intents[0].Extra)

	types, ok := data["Types"].([]VoiceType)
	require.True(t, ok)
	require.Len(t, types, 1, "only types a listed intent uses")
	assert.Equal(t, []skillmodel.SlotValue{{Value: "Dune"}, {Value: "Alien"}}, types[0].Values)
	assert.True(t, types[0].Dynamic)
	assert.Equal(t, []skillmodel.SlotValue{{Value: "Dune"}}, src.Model.Types[0].Values, "the model's values aren't changed")
}

func TestVoiceDataShowsLocaleSynonyms(t *testing.T) {
	src := &skillmodel.Sources{
		Model: skillmodel.Model{
			Intents: []skillmodel.Intent{{Name: "ViewRoom", Slots: []skillmodel.Slot{{Name: "Room", Type: "ROOM"}}}},
			Types:   []skillmodel.SlotType{{Name: "ROOM", Values: []skillmodel.SlotValue{{Value: "lounge"}}}},
		},
		Languages: map[string]skillmodel.Language{"en": {
			Utterances: map[string][]string{"ViewRoom": {"show the {Room}"}},
			Types:      map[string][]skillmodel.SlotValue{"ROOM": {{ID: "bed", Value: "bedroom"}, {ID: "den", Value: "den"}}},
		}},
		Locales: map[string]skillmodel.Locale{
			"en-AU": {Language: "en", Types: map[string]map[string][]string{"ROOM": {"den": {"rumpus room"}, "lounge": {"lounge room"}}}},
			"en-US": {Language: "en"},
		},
	}

	types, ok := voiceData(src, "en", nil)["Types"].([]VoiceType)
	require.True(t, ok)
	require.Len(t, types, 1)
	assert.Equal(t, []LocaleValues{{Locale: "en-AU", Values: []skillmodel.SlotValue{
		{Value: "lounge", Synonyms: []string{"lounge room"}},
		{ID: "den", Value: "den", Synonyms: []string{"rumpus room"}},
	}}}, types[0].Extra, "each value the locale adds to, in the type's order, with only its synonyms")

	pages, err := VoiceCommands(loadSources(t), i18n.FS)
	require.NoError(t, err)
	assert.Contains(t, string(pages["fr"]), "Also in `fr-CA`:\n\n- clip (vidéoclips)\n- chanson (toune, tounes)\n")
}

func TestSectionTitles(t *testing.T) {
	fsys := fstest.MapFS{"model.yaml": {Data: []byte(`# File header.

# Playback.
intents:
  - name: AMAZON.StopIntent
  # Movies and
  # collections.
  - name: WatchMovie
  - name: WatchTrailer
  # An old note.

  # Music.
  - name: ListenAlbum
types: []
`)}}

	titles, err := sectionTitles(fsys)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"WatchMovie": "Movies and collections", "ListenAlbum": "Music"}, titles)
}

func TestSectionTitlesErrors(t *testing.T) {
	testCases := []struct {
		name    string
		have    string
		missing bool
	}{
		{name: "ShouldRejectNotYAML", have: "intents: ["},
		{name: "ShouldRejectNotAMapping", have: "- one\n- two\n"},
		{name: "ShouldRejectEmpty"},
		{name: "ShouldRejectNoIntents", have: "types: []\n"},
		{name: "ShouldRejectUndecodableItem", have: "intents:\n  # Movies.\n  - name: [nested]\n"},
		{name: "ShouldRejectMissingModelYAML", missing: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			if !tc.missing {
				fsys["model.yaml"] = &fstest.MapFile{Data: []byte(tc.have)}
			}

			_, err := sectionTitles(fsys)
			require.Error(t, err)
		})
	}

	_, err := VoiceCommands(loadSources(t), fstest.MapFS{})
	require.Error(t, err)
}

func TestSectionTitle(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldDropTrailingPeriod", have: "# Movies.", expected: "Movies"},
		{name: "ShouldReadTitleWithoutPeriod", have: "# Movies", expected: "Movies"},
		{name: "ShouldTrimSpaces", have: "#   Movies.  ", expected: "Movies"},
		{name: "ShouldJoinLines", have: "# Line one\n# line two.", expected: "Line one line two"},
		{name: "ShouldKeepOnlyTheLastParagraph", have: "# Ignored.\n\n# Kept.", expected: "Kept"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, sectionTitle(tc.have))
		})
	}
}

func TestEditURL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldEditEnglishInTheRepository", have: "en", expected: SourceEditURL},
		{name: "ShouldEditGermanOnCrowdin", have: "de", expected: CrowdinProject + "/de"},
		{name: "ShouldNameSpanishByItsRegionalVariant", have: "es", expected: CrowdinProject + "/es-ES"},
		{name: "ShouldEditFrenchOnCrowdin", have: "fr", expected: CrowdinProject + "/fr"},
		{name: "ShouldNamePortugueseByItsRegionalVariant", have: "pt", expected: CrowdinProject + "/pt-BR"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, editURL(tc.have))
		})
	}
}

// TestCrowdinLanguageMatchesCrowdinConfig keeps crowdinLanguage the reverse of every file's languages_mapping in .crowdin.yml.
func TestCrowdinLanguageMatchesCrowdinConfig(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile("../../.crowdin.yml")
	require.NoError(t, err)

	var cfg struct {
		Files []struct {
			Source  string `yaml:"source"`
			Mapping struct {
				TwoLetters map[string]string `yaml:"two_letters_code"`
			} `yaml:"languages_mapping"`
		} `yaml:"files"`
	}
	require.NoError(t, yaml.Unmarshal(b, &cfg))
	require.NotEmpty(t, cfg.Files)

	for _, f := range cfg.Files {
		require.NotEmpty(t, f.Mapping.TwoLetters, "%s maps its languages", f.Source)

		for crowdin, dir := range f.Mapping.TwoLetters {
			assert.Equal(t, crowdin, crowdinLanguage(dir), "%s: %s", f.Source, dir)
		}

		for dir, crowdin := range crowdinRegional {
			assert.Equal(t, dir, f.Mapping.TwoLetters[crowdin], "%s maps %s", f.Source, crowdin)
		}
	}
}

func TestVoiceCommandsShowLaunchPhrases(t *testing.T) {
	pages, err := VoiceCommands(loadSources(t), i18n.FS)
	require.NoError(t, err)

	assert.Contains(t, string(pages["en"]), "| `en-GB` | \"Alexa, open encore\" | \"start encore\", \"launch encore\" | \"Alexa, open the encore skill\" |\n")
	assert.Contains(t, string(pages["ja"]), "| `ja-JP` | \"Alexa, アンコールを開いて\" |")
	assert.Contains(t, string(pages["fr"]), "| `fr-CA` | \"Alexa, ouvre kodi\" | \"lance kodi\", \"démarre kodi\" | \"Alexa, ouvre la compétence kodi\" |\n")

	testCases := []struct {
		name   string
		lang   string
		locale string
	}{
		{name: "ShouldCautionInDutch", lang: "nl", locale: "nl-NL"},
		{name: "ShouldCautionInPortuguese", lang: "pt", locale: "pt-BR"},
		{name: "ShouldCautionInHindi", lang: "hi", locale: "hi-IN"},
		{name: "ShouldCautionInArabic", lang: "ar", locale: "ar-SA"},
		{name: "ShouldNotCautionInEnglish", lang: "en"},
		{name: "ShouldNotCautionInGerman", lang: "de"},
		{name: "ShouldNotCautionInSpanish", lang: "es"},
		{name: "ShouldNotCautionInFrench", lang: "fr"},
		{name: "ShouldNotCautionInItalian", lang: "it"},
		{name: "ShouldNotCautionInJapanese", lang: "ja"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.locale != "" {
				assert.Contains(t, string(pages[tc.lang]), ":::caution[A plain open phrase may not start the skill]\nIn `"+tc.locale+"`, ")
			} else {
				assert.NotContains(t, string(pages[tc.lang]), "A plain open phrase may not start the skill")
			}
		})
	}
}
