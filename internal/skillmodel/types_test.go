package skillmodel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const roomValues = "    values:\n      - {id: lounge, value: lounge, synonyms: [living room]}\n"

func TestLanguageTypesAndDynamicTypes(t *testing.T) {
	src := load(t, map[string]string{
		"model.yaml":            strings.Replace(baseFiles["model.yaml"], roomValues, "    dynamic: true\n  - name: NAV_ACTION\n", 1),
		"lang/en/types.yaml":    "NAV_ACTION:\n  up: |-\n    up\n    go up\n",
		"lang/en/examples.yaml": "KODI_ROOM:\n  - {value: lounge}\n  - {value: bedroom}\n",
	})

	types, err := src.Types("en-AU")
	require.NoError(t, err)
	assert.Equal(t, "up", types["NAV_ACTION"][0].ID, "custom type values come from the language")

	byName := func(m *InteractionModel) map[string]ModelType {
		out := map[string]ModelType{}
		for _, mt := range m.InteractionModel.LanguageModel.Types {
			out[mt.Name] = mt
		}

		return out
	}

	m, err := Build(src, "en-AU", BuildOptions{})
	require.NoError(t, err)
	assert.Len(t, byName(m)["KODI_ROOM"].Values, 2, "a dynamic type uses the language's examples when nothing is supplied")
	assert.Equal(t, []string{"go up"}, byName(m)["NAV_ACTION"].Values[0].Name.Synonyms)

	m, err = Build(src, "en-AU", BuildOptions{SlotValues: map[string][]SlotValue{"KODI_ROOM": {{ID: "7", Value: "rumpus room"}}}})
	require.NoError(t, err)

	rooms := byName(m)["KODI_ROOM"].Values
	require.Len(t, rooms, 1, "supplied values replace the examples")
	assert.Equal(t, "rumpus room", rooms[0].Name.Value)

	assert.Empty(t, messages(Lint(src), Error))
}

func TestLintTypeRules(t *testing.T) {
	noValues := strings.Replace(baseFiles["model.yaml"], roomValues, "", 1)
	dynamic := strings.Replace(noValues, "  - name: KODI_ROOM\n", "  - name: KODI_ROOM\n    dynamic: true\n", 1)

	testCases := []struct {
		name      string
		overrides map[string]string
		expected  string
	}{
		{name: "ShouldRejectEmptyType", overrides: map[string]string{"model.yaml": noValues}, expected: "slot type KODI_ROOM has no values"},
		{name: "ShouldRejectDynamicTypeWithoutExamples", overrides: map[string]string{"model.yaml": dynamic}, expected: "needs example values"},
		{name: "ShouldRejectUndeclaredType", overrides: map[string]string{"lang/en/types.yaml": "COLOUR:\n  red: red\n"}, expected: "doesn't declare"},
		{name: "ShouldRejectDynamicValuesInTypesYAML", overrides: map[string]string{"model.yaml": dynamic, "lang/en/types.yaml": "KODI_ROOM:\n  lounge: lounge\n"}, expected: "belong in examples.yaml"},
		{name: "ShouldRejectStaticValuesInExamplesYAML", overrides: map[string]string{"model.yaml": noValues, "lang/en/examples.yaml": "KODI_ROOM:\n  - {value: lounge}\n"}, expected: "belong in types.yaml"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := messages(Lint(load(t, tc.overrides)), Error)
			assert.True(t, containsSubstring(actual, tc.expected), "want an error containing %q, got %q", tc.expected, actual)
		})
	}
}

func TestLintSearchQueryAlone(t *testing.T) {
	src := load(t, nil)
	src.Model.Intents = append(src.Model.Intents, Intent{Name: "PlayMedia", Slots: []Slot{{Name: "Query", Type: "AMAZON.SearchQuery"}, {Name: "Room", Type: "KODI_ROOM"}}})

	lang := src.Languages["en"]
	lang.Utterances["PlayMedia"] = []string{"play {Query} in the {Room}"}
	src.Languages["en"] = lang

	got := messages(Lint(src), Error)
	assert.True(t, containsSubstring(got, "combines an AMAZON.SearchQuery slot"), "got %q", got)
}

func TestKeyedFiles(t *testing.T) {
	src := load(t, nil)
	assert.Equal(t, []string{"(show|open) the {Room}", "go to the {Room}", "switch to the {Room}"}, src.Languages["en"].Utterances["ViewRoom"], "every line of every entry, in file order")
	assert.Equal(t, []string{"show", "switch"}, src.Languages["en"].UtteranceKeys["ViewRoom"])

	testCases := []struct {
		name string
		have string
		err  string
	}{
		{name: "ShouldRejectDuplicateKey", have: "WatchMovie:\n  play: \"watch {Movie}\"\n  play: \"play {Movie}\"\n", err: "defined twice"},
		{name: "ShouldRejectEmptyEntry", have: "WatchMovie:\n  play: \"  \"\n", err: "is empty"},
		{name: "ShouldRejectList", have: "WatchMovie:\n  - \"watch {Movie}\"\n", err: "must map keys to text"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			for k, v := range baseFiles {
				files[k] = v
			}

			files["lang/en/utterances.yaml"] = tc.have
			_, err := Load(testFS(files))
			assert.ErrorContains(t, err, tc.err)
		})
	}
}

func TestLintKeysMustBeInReference(t *testing.T) {
	src := load(t, map[string]string{
		"lang/de/utterances.yaml": "WatchMovie:\n  play: \"spiel den film {Movie}\"\n  extra: \"zeig mir {Movie}\"\n",
		"lang/de/responses.yaml":  "playback.playing: \"Spiele {title}.\"\n",
		"locales/de-DE.yaml":      "language: de\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore test\n",
	})

	got := messages(Lint(src), Error)
	assert.True(t, containsSubstring(got, "WatchMovie.extra isn't in lang/en"), "got %q", got)
	assert.False(t, containsSubstring(got, "WatchMovie.play"), "keys the reference has are fine")
}
