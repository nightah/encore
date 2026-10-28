package docsgen

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
}
