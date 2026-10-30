package skillmodel

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

func files(overrides map[string]string, remove ...string) map[string]string {
	out := map[string]string{}
	for k, v := range baseFiles {
		out[k] = v
	}

	for k, v := range overrides {
		out[k] = v
	}

	for _, k := range remove {
		delete(out, k)
	}

	return out
}

func TestLoadErrors(t *testing.T) {
	tests := map[string]struct {
		files map[string]string
		want  string
	}{
		"no model":               {files: files(nil, "model.yaml"), want: "reading model.yaml"},
		"malformed model":        {files: files(map[string]string{"model.yaml": "intents: [\n"}), want: "decoding model.yaml"},
		"no utterances":          {files: files(nil, "lang/en/utterances.yaml"), want: "reading lang/en/utterances.yaml"},
		"malformed utterances":   {files: files(map[string]string{"lang/en/utterances.yaml": "WatchMovie: {\n"}), want: "decoding lang/en/utterances.yaml"},
		"utterances not a map":   {files: files(map[string]string{"lang/en/utterances.yaml": "- play\n"}), want: "expected a mapping of names to keyed entries"},
		"utterance not text":     {files: files(map[string]string{"lang/en/utterances.yaml": "WatchMovie:\n  play:\n    - one\n"}), want: "WatchMovie.play must be text"},
		"no responses":           {files: files(nil, "lang/en/responses.yaml"), want: "reading lang/en/responses.yaml"},
		"malformed types":        {files: files(map[string]string{"lang/en/types.yaml": "KODI_ROOM:\n  - lounge\n"}), want: "lang/en/types.yaml"},
		"malformed examples":     {files: files(map[string]string{"lang/en/examples.yaml": "KODI_ROOM: lounge\n"}), want: "decoding lang/en/examples.yaml"},
		"unknown example fields": {files: files(map[string]string{"lang/en/examples.yaml": "KODI_ROOM:\n  - {label: lounge}\n"}), want: "label"},
		"no locales":             {files: files(nil, "locales/en-AU.yaml", "locales/en-US.yaml"), want: "no locales found"},
		"malformed locale":       {files: files(map[string]string{"locales/en-US.yaml": "language: [en\n"}), want: "decoding locales/en-US.yaml"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			src, err := Load(testFS(tt.files))
			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, src)
		})
	}
}

func TestLoadWithoutLangDirectory(t *testing.T) {
	_, err := Load(fstest.MapFS{"model.yaml": {Data: []byte("intents: []\n")}})
	require.ErrorContains(t, err, "reading lang")
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestLoadIgnoresFilesBesideLanguages(t *testing.T) {
	src, err := Load(testFS(files(map[string]string{"lang/README.md": "# languages\n", "locales/notes.txt": "ignored"})))
	require.NoError(t, err)
	assert.Equal(t, []string{"en"}, keys(src.Languages))
	assert.Equal(t, []string{"en-AU", "en-US"}, src.LocaleNames())
}

func TestLoadEmptyKeyedFile(t *testing.T) {
	src, err := Load(testFS(files(map[string]string{"lang/en/utterances.yaml": ""})))
	require.NoError(t, err)
	assert.Empty(t, src.Languages["en"].Utterances)
}

func TestTypesSynonymsAndIDs(t *testing.T) {
	src := load(t, map[string]string{"lang/en/types.yaml": "KODI_ROOM:\n  bedroom: |-\n    bedroom\n    main bedroom\n    master bedroom\n"})

	types, err := src.Types("en-AU")
	require.NoError(t, err)
	assert.Equal(t, []SlotValue{
		{ID: "lounge", Value: "lounge", Synonyms: []string{"living room"}},
		{ID: "bedroom", Value: "bedroom", Synonyms: []string{"main bedroom", "master bedroom"}},
	}, types["KODI_ROOM"], "the model's values come first, then the language's, keyed by entry")
}

func TestTypesLocaleSynonyms(t *testing.T) {
	src := load(t, map[string]string{
		"lang/en/types.yaml": "KODI_ROOM:\n  bedroom: |-\n    bedroom\n    main bedroom\n",
		"locales/en-AU.yaml": baseFiles["locales/en-AU.yaml"] + "types:\n  KODI_ROOM:\n    bedroom: [master bedroom]\n    lounge: [front room, lounge room]\n",
	})

	au, err := src.Types("en-AU")
	require.NoError(t, err)
	assert.Equal(t, []SlotValue{
		{ID: "lounge", Value: "lounge", Synonyms: []string{"living room", "front room", "lounge room"}},
		{ID: "bedroom", Value: "bedroom", Synonyms: []string{"main bedroom", "master bedroom"}},
	}, au["KODI_ROOM"], "the locale's synonyms follow the model's and the language's, on the model's values too")

	us, err := src.Types("en-US")
	require.NoError(t, err)
	assert.Equal(t, []SlotValue{
		{ID: "lounge", Value: "lounge", Synonyms: []string{"living room"}},
		{ID: "bedroom", Value: "bedroom", Synonyms: []string{"main bedroom"}},
	}, us["KODI_ROOM"], "one locale's synonyms never leak into another's")
	assert.Equal(t, []string{"living room"}, src.Model.Types[0].Values[0].Synonyms, "the model's values stay untouched")
	assert.Equal(t, []string{"main bedroom"}, src.Languages["en"].Types["KODI_ROOM"][0].Synonyms, "the language's values stay untouched")
}

func TestBuildTakesLocaleSynonyms(t *testing.T) {
	src := load(t, map[string]string{"locales/en-AU.yaml": baseFiles["locales/en-AU.yaml"] + "types:\n  KODI_ROOM:\n    lounge: [front room]\n"})

	m, err := Build(src, "en-AU", BuildOptions{})
	require.NoError(t, err)

	types := m.InteractionModel.LanguageModel.Types
	require.Len(t, types, 1)
	assert.Equal(t, []string{"living room", "front room"}, types[0].Values[0].Name.Synonyms)
}

func TestTypesLocaleSynonymsByValueWithoutID(t *testing.T) {
	src := load(t, map[string]string{
		"model.yaml":         strings.Replace(baseFiles["model.yaml"], "{id: lounge, value: lounge,", "{value: lounge,", 1),
		"locales/en-AU.yaml": baseFiles["locales/en-AU.yaml"] + "types:\n  KODI_ROOM:\n    lounge: [front room]\n",
	})

	au, err := src.Types("en-AU")
	require.NoError(t, err)
	assert.Equal(t, []string{"living room", "front room"}, au["KODI_ROOM"][0].Synonyms, "a value without an ID is its own ID")
}

func TestTypesLocaleSynonymsErrors(t *testing.T) {
	tests := map[string]struct {
		model, types, want string
	}{
		"undeclared type": {
			types: "  KODI_ZONE:\n    upstairs: [up]\n",
			want:  "locale en-AU: synonyms given for slot type KODI_ZONE, which model.yaml doesn't declare",
		},
		"dynamic type": {
			model: strings.Replace(baseFiles["model.yaml"], "  - name: KODI_ROOM\n", "  - name: KODI_ROOM\n    dynamic: true\n", 1),
			types: "  KODI_ROOM:\n    lounge: [front room]\n",
			want:  "locale en-AU: synonyms given for dynamic slot type KODI_ROOM, whose values come from Encore at build time",
		},
		"unknown value": {
			types: "  KODI_ROOM:\n    attic: [loft]\n",
			want:  "locale en-AU: synonyms given for value attic of slot type KODI_ROOM, which has no value with that ID",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			overrides := map[string]string{"locales/en-AU.yaml": baseFiles["locales/en-AU.yaml"] + "types:\n" + tt.types}
			if tt.model != "" {
				overrides["model.yaml"] = tt.model
			}

			_, err := load(t, overrides).Types("en-AU")
			require.EqualError(t, err, tt.want)
		})
	}
}

func TestUtterancesDoNotAliasLanguage(t *testing.T) {
	src := load(t, map[string]string{"locales/en-GB.yaml": "language: en\nlaunch:\n  open: open {invocation}\n  skill: open the {invocation} skill\nskill_name: Encore\ninvocation: encore test\nutterances:\n  WatchMovie:\n    - \"stick on {Movie}\"\n"})

	au, err := src.Utterances("en-AU")
	require.NoError(t, err)

	gb, err := src.Utterances("en-GB")
	require.NoError(t, err)
	assert.Contains(t, gb["WatchMovie"], "stick on {Movie}")
	assert.Contains(t, au["WatchMovie"], "chuck on {Movie}", "building another locale leaves this one intact")

	us, err := src.Utterances("en-US")
	require.NoError(t, err)

	assert.Contains(t, au["WatchMovie"], "chuck on {Movie}")
	assert.NotContains(t, us["WatchMovie"], "chuck on {Movie}", "one locale's additions never leak into another's")
	assert.NotContains(t, src.Languages["en"].Utterances["WatchMovie"], "chuck on {Movie}")
}

func TestResponses(t *testing.T) {
	src := load(t, map[string]string{
		"lang/en/responses.yaml": "playback.playing: \"Playing {title}.\"\nplayback.paused: \"Paused.\"\n",
		"locales/en-AU.yaml":     baseFiles["locales/en-AU.yaml"] + "responses:\n  playback.paused: \"Righto, paused.\"\n",
	})

	au, err := src.Responses("en-AU")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"playback.playing": "Playing {title}.", "playback.paused": "Righto, paused."}, au)

	us, err := src.Responses("en-US")
	require.NoError(t, err)
	assert.Equal(t, "Paused.", us["playback.paused"], "an override applies to its own locale only")
	assert.Equal(t, "Paused.", src.Languages["en"].Responses["playback.paused"])
}

func TestResolveErrors(t *testing.T) {
	src := load(t, map[string]string{"locales/en-GB.yaml": "language: xx\nskill_name: Encore\ninvocation: encore test\n"})

	for name, call := range map[string]func(string) error{
		"utterances": func(l string) error { _, err := src.Utterances(l); return err },
		"types":      func(l string) error { _, err := src.Types(l); return err },
		"responses":  func(l string) error { _, err := src.Responses(l); return err },
	} {
		t.Run(name, func(t *testing.T) {
			require.EqualError(t, call("fr-FR"), `unknown locale "fr-FR"`)
			require.EqualError(t, call("en-GB"), `locale en-GB: unknown language "xx"`)
		})
	}

	_, err := Build(src, "en-GB", BuildOptions{})
	require.ErrorContains(t, err, "unknown language")
}

func TestModelLookups(t *testing.T) {
	src := load(t, nil)

	in, ok := src.Model.Intent("ViewRoom")
	require.True(t, ok)
	assert.Equal(t, []Slot{{Name: "Room", Type: "KODI_ROOM"}}, in.Slots)

	_, ok = src.Model.Intent("viewroom")
	assert.False(t, ok, "intent names are case-sensitive")

	ty, ok := src.Model.Type("KODI_ROOM")
	require.True(t, ok)
	assert.Equal(t, "KODI_ROOM", ty.Name)

	_, ok = src.Model.Type("AMAZON.Movie")
	assert.False(t, ok, "built-in types are not declared in the model")
}

func TestIsBuiltin(t *testing.T) {
	assert.True(t, IsBuiltin("AMAZON.StopIntent"))
	assert.True(t, IsBuiltin("AMAZON.Movie"))
	assert.False(t, IsBuiltin("KODI_ROOM"))
	assert.False(t, IsBuiltin("amazon.Movie"))
	assert.False(t, IsBuiltin("AMAZON"))
}

func TestIntentContent(t *testing.T) {
	m := Model{Intents: []Intent{
		{Name: "WatchMovie", Content: media.ContentMovies},
		{Name: "ListenToArtist", Content: media.ContentMusic},
		{Name: "AMAZON.PauseIntent"},
	}}

	assert.Equal(t, map[string]media.Content{"WatchMovie": media.ContentMovies, "ListenToArtist": media.ContentMusic}, m.IntentContent())
	assert.Empty(t, (&Model{}).IntentContent())
}

func TestBuildRejectsValuesForUndeclaredType(t *testing.T) {
	_, err := Build(load(t, nil), "en-AU", BuildOptions{SlotValues: map[string][]SlotValue{"KODI_ZONE": {{Value: "upstairs"}}}})
	require.EqualError(t, err, "locale en-AU: values supplied for undeclared slot type KODI_ZONE")
}

func TestBuildRejectsBadPattern(t *testing.T) {
	src := load(t, map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "utterances:\n  WatchMovie:\n    - \"(watch {Movie}\"\n"})

	_, err := Build(src, "en-US", BuildOptions{})
	require.ErrorContains(t, err, "locale en-US, intent WatchMovie: pattern")

	_, err = Build(src, "en-AU", BuildOptions{})
	require.NoError(t, err, "the bad pattern belongs to en-US only")
}

func TestBuildDedupesSamplesAcrossPatterns(t *testing.T) {
	src := load(t, map[string]string{"locales/en-AU.yaml": baseFiles["locales/en-AU.yaml"] + "    - \"put on {Movie}\"\n"})

	m, err := Build(src, "en-AU", BuildOptions{})
	require.NoError(t, err)

	samples := m.InteractionModel.LanguageModel.Intents[1].Samples
	count := 0

	for _, s := range samples {
		if s == "put on {Movie}" {
			count++
		}
	}

	assert.Equal(t, 1, count, "SMAPI rejects duplicate samples")
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
