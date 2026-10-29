package docsgen

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

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
	src.Languages["nl"] = src.Languages["en"]

	pages, err := VoiceCommands(src, i18n.FS)
	require.NoError(t, err)

	page := string(pages["nl"])
	assert.Contains(t, page, "title: nl\n")
	assert.Contains(t, page, "editUrl: "+CrowdinProject+"/nl\n")
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

func TestVoiceDataShowsFallbacksWhereEveryLocaleLacksTheBuiltin(t *testing.T) {
	src := &skillmodel.Sources{
		Model: skillmodel.Model{
			Intents: []skillmodel.Intent{{Name: "PlayMedia", Slots: []skillmodel.Slot{{Name: "Query", Type: "AMAZON.SearchQuery", Fallback: "KODI_TITLE"}}}},
			Types:   []skillmodel.SlotType{{Name: "KODI_TITLE", Dynamic: true}},
		},
		Languages: map[string]skillmodel.Language{
			"en": {Utterances: map[string][]string{"PlayMedia": {"play {Query}"}}},
			"nl": {Utterances: map[string][]string{"PlayMedia": {"speel {Query}"}}, Types: map[string][]skillmodel.SlotValue{"KODI_TITLE": {{Value: "bluey"}}}},
		},
		Locales: map[string]skillmodel.Locale{
			"en-AU": {Language: "en"},
			"nl-NL": {Language: "nl"},
		},
	}

	for lang, want := range map[string]string{"en": "AMAZON.SearchQuery", "nl": "KODI_TITLE"} {
		data := voiceData(src, lang, nil)

		sections, ok := data["Sections"].([]VoiceSection)
		require.True(t, ok)
		assert.Equal(t, []skillmodel.Slot{{Name: "Query", Type: want, Fallback: "KODI_TITLE"}}, sections[0].Intents[0].Slots, lang)

		types, ok := data["Types"].([]VoiceType)
		require.True(t, ok)
		assert.Equal(t, want == "KODI_TITLE", len(types) == 1, "%s lists the fallback's values only where it is used", lang)
	}

	assert.Equal(t, "AMAZON.SearchQuery", src.Model.Intents[0].Slots[0].Type, "the model isn't changed")
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
	for name, model := range map[string]string{
		"not YAML":         "intents: [",
		"not a mapping":    "- one\n- two\n",
		"empty":            "",
		"no intents":       "types: []\n",
		"undecodable item": "intents:\n  # Movies.\n  - name: [nested]\n",
	} {
		_, err := sectionTitles(fstest.MapFS{"model.yaml": {Data: []byte(model)}})
		require.Error(t, err, name)
	}

	_, err := sectionTitles(fstest.MapFS{})
	require.Error(t, err, "no model.yaml")

	_, err = VoiceCommands(loadSources(t), fstest.MapFS{})
	require.Error(t, err)
}

func TestSectionTitle(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"# Movies.":               "Movies",
		"# Movies":                "Movies",
		"#   Movies.  ":           "Movies",
		"# Line one\n# line two.": "Line one line two",
		"# Ignored.\n\n# Kept.":   "Kept",
	} {
		assert.Equal(t, want, sectionTitle(in), "%q", in)
	}
}

func TestEditURL(t *testing.T) {
	t.Parallel()

	assert.Equal(t, SourceEditURL, editURL("en"))
	assert.Equal(t, CrowdinProject+"/de", editURL("de"))
	assert.Equal(t, CrowdinProject+"/es-ES", editURL("es"), "Crowdin names Spanish by its regional variant")
	assert.Equal(t, CrowdinProject+"/fr", editURL("fr"))
	assert.Equal(t, CrowdinProject+"/pt-BR", editURL("pt"), "Crowdin names Portuguese by its regional variant")
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
