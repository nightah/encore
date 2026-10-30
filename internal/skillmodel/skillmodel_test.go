package skillmodel

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/media"
)

func testFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(data)}
	}

	return fsys
}

var baseFiles = map[string]string{
	"model.yaml": `
intents:
  - name: AMAZON.StopIntent
  - name: WatchMovie
    slots:
      - {name: Movie, type: AMAZON.Movie}
  - name: ViewRoom
    slots:
      - {name: Room, type: KODI_ROOM}
types:
  - name: KODI_ROOM
    values:
      - {id: lounge, value: lounge, synonyms: [living room]}
`,
	"lang/en/utterances.yaml": `
WatchMovie:
  play: "(play|watch) [the] movie {Movie}"
  put_on: "put on {Movie}"
  want: "i want to watch {Movie}"
ViewRoom:
  show: |-
    (show|open) the {Room}
    go to the {Room}
  switch: "switch to the {Room}"
`,
	"lang/en/responses.yaml": `
playback.playing: "Playing {title}."
`,
	"locales/en-AU.yaml": `
language: en
skill_name: Encore
invocation: encore test
utterances:
  WatchMovie:
    - "chuck on {Movie}"
`,
	"locales/en-US.yaml": `
language: en
skill_name: Encore
invocation: encore test
`,
}

func load(t *testing.T, overrides map[string]string) *Sources {
	t.Helper()

	files := map[string]string{}
	for k, v := range baseFiles {
		files[k] = v
	}

	for k, v := range overrides {
		files[k] = v
	}

	src, err := Load(testFS(files))
	require.NoError(t, err)

	return src
}

func TestBuild(t *testing.T) {
	src := load(t, nil)

	m, err := Build(src, "en-AU", BuildOptions{
		SlotValues: map[string][]SlotValue{
			"AMAZON.Movie": {{ID: "movie-42", Value: "Dune Part Two", Synonyms: []string{"the new dune"}}},
		},
	})
	require.NoError(t, err)

	lm := m.InteractionModel.LanguageModel
	assert.Equal(t, "encore test", lm.InvocationName)
	require.Len(t, lm.Intents, 3)

	stop, watch := lm.Intents[0], lm.Intents[1]
	assert.Equal(t, "AMAZON.StopIntent", stop.Name)
	assert.Empty(t, stop.Samples)
	assert.NotNil(t, stop.Samples, "SMAPI wants an empty array, not null")

	assert.Contains(t, watch.Samples, "play the movie {Movie}")
	assert.Contains(t, watch.Samples, "chuck on {Movie}", "locale extras are appended")
	assert.Equal(t, []ModelSlot{{Name: "Movie", Type: "AMAZON.Movie"}}, watch.Slots)

	require.Len(t, lm.Types, 2)
	assert.Equal(t, "AMAZON.Movie", lm.Types[0].Name, "extended built-in types are emitted")
	assert.Equal(t, "movie-42", lm.Types[0].Values[0].ID)
	assert.Equal(t, "KODI_ROOM", lm.Types[1].Name)

	us, err := Build(src, "en-US", BuildOptions{Invocation: "telly"})
	require.NoError(t, err)
	assert.Equal(t, "telly", us.InteractionModel.LanguageModel.InvocationName)
	assert.NotContains(t, us.InteractionModel.LanguageModel.Intents[1].Samples, "chuck on {Movie}")
	assert.Len(t, us.InteractionModel.LanguageModel.Types, 1, "unused built-ins without values are not emitted")

	b, err := json.Marshal(m)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(b), `{"interactionModel":{"languageModel":{"invocationName":"encore test"`))
}

func TestBuildRejectsEmptyCustomType(t *testing.T) {
	src := load(t, map[string]string{"model.yaml": strings.Replace(baseFiles["model.yaml"],
		"    values:\n      - {id: lounge, value: lounge, synonyms: [living room]}\n", "", 1)})

	_, err := Build(src, "en-AU", BuildOptions{})
	require.ErrorContains(t, err, "KODI_ROOM has no values")

	_, err = Build(src, "en-AU", BuildOptions{SlotValues: map[string][]SlotValue{"KODI_ROOM": {{Value: "bedroom"}}}})
	assert.NoError(t, err, "values supplied at build time satisfy the type")
}

func TestBuildUnknownLocale(t *testing.T) {
	_, err := Build(load(t, nil), "fr-FR", BuildOptions{})
	assert.Error(t, err)
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	files := map[string]string{}
	for k, v := range baseFiles {
		files[k] = v
	}

	files["locales/en-AU.yaml"] += "invokation: typo\n"

	_, err := Load(testFS(files))
	assert.ErrorContains(t, err, "invokation")
}

func messages(findings []Finding, sev Severity) []string {
	var out []string

	for _, f := range findings {
		if f.Severity == sev {
			out = append(out, f.String())
		}
	}

	return out
}

func TestLintClean(t *testing.T) {
	findings := Lint(load(t, nil))
	assert.Empty(t, messages(findings, Error))
}

func TestLintRules(t *testing.T) {
	tests := map[string]struct {
		overrides map[string]string
		want      string
		sev       Severity
	}{
		"undeclared slot": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "utterances:\n  WatchMovie:\n    - \"watch {Film}\"\n"},
			want:      "undeclared slot {Film}", sev: Error,
		},
		"digits": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "utterances:\n  WatchMovie:\n    - \"watch 2 {Movie}\"\n"},
			want:      "spell out numbers", sev: Error,
		},
		"duplicate across intents": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "utterances:\n  ViewRoom:\n    - \"put on {Movie}\"\n"},
			want:      "is also a sample of", sev: Error,
		},
		"same carrier across intents": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "utterances:\n  ViewRoom:\n    - \"put on {Room}\"\n"},
			want:      "same wording", sev: Warning,
		},
		"slot joined to other text": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "utterances:\n  WatchMovie:\n    - \"watch {Movie}'s sequel\"\n"},
			want:      "joins slot {Movie} to other text", sev: Error,
		},
		"slot name with two types": {
			overrides: map[string]string{"model.yaml": strings.Replace(baseFiles["model.yaml"], "      - {name: Room, type: KODI_ROOM}\n", "      - {name: Room, type: KODI_ROOM}\n      - {name: Movie, type: KODI_ROOM}\n", 1)},
			want:      "slot Movie has type KODI_ROOM here but AMAZON.Movie in WatchMovie", sev: Error,
		},
		"unknown intent": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "utterances:\n  Rewind:\n    - \"rewind\"\n"},
			want:      "not in model.yaml", sev: Error,
		},
		"bad pattern": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "utterances:\n  WatchMovie:\n    - \"(watch {Movie}\"\n"},
			want:      "missing", sev: Error,
		},
		"reserved invocation word": {
			overrides: map[string]string{"locales/en-US.yaml": "language: en\nskill_name: Encore\ninvocation: alexa remote\n"},
			want:      "reserved word", sev: Error,
		},
		"one word invocation": {
			overrides: map[string]string{"locales/en-US.yaml": "language: en\nskill_name: Encore\ninvocation: encore\n"},
			want:      "one-word invocation", sev: Warning,
		},
		"response placeholder mismatch": {
			overrides: map[string]string{
				"lang/de/utterances.yaml": "{}",
				"lang/de/responses.yaml":  "playback.playing: \"Spiele {titel}.\"\n",
			},
			want: "placeholders [titel], expected [title]", sev: Error,
		},
		"missing translation": {
			overrides: map[string]string{
				"lang/de/utterances.yaml": "{}",
				"lang/de/responses.yaml":  "{}",
			},
			want: "response playback.playing is not translated yet", sev: Warning,
		},
		"override of unknown response": {
			overrides: map[string]string{"locales/en-US.yaml": baseFiles["locales/en-US.yaml"] + "responses:\n  nope: \"x\"\n"},
			want:      "does not exist", sev: Error,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := messages(Lint(load(t, tt.overrides)), tt.sev)
			assert.True(t, containsSubstring(got, tt.want), "want a %s containing %q, got %q", tt.sev, tt.want, got)
		})
	}
}

func TestLintSearchQueryNeedsCarrier(t *testing.T) {
	src := load(t, nil)
	src.Model.Intents = append(src.Model.Intents, Intent{Name: "PlayMedia", Slots: []Slot{{Name: "Query", Type: "AMAZON.SearchQuery"}}})

	lang := src.Languages["en"]
	lang.Utterances["PlayMedia"] = []string{"{Query}", "play {Query}"}
	src.Languages["en"] = lang

	got := messages(Lint(src), Error)
	assert.True(t, containsSubstring(got, "carrier phrase"), "got %q", got)
}

func containsSubstring(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}

	return false
}

// TestShippedSources keeps the real i18n sources buildable and lint-clean.
func TestShippedSources(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	errs := messages(Lint(src), Error)
	assert.Empty(t, errs)

	for _, locale := range src.LocaleNames() {
		_, err := Build(src, locale, BuildOptions{})
		assert.NoError(t, err, locale)
	}
}

// TestShippedFreeTextSlotsInEveryLocale keeps the free-text slots on AMAZON.SearchQuery in every locale, Dutch included: Amazon builds nl-NL models that use it and fills it, though its slot type reference leaves nl-NL off the type's list.
func TestShippedFreeTextSlotsInEveryLocale(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	for _, locale := range src.LocaleNames() {
		m, err := Build(src, locale, BuildOptions{})
		require.NoError(t, err, locale)

		got := map[string]string{}

		for _, in := range m.InteractionModel.LanguageModel.Intents {
			for _, s := range in.Slots {
				if s.Type == "AMAZON.SearchQuery" {
					got[in.Name] = s.Name
				}
			}
		}

		assert.Equal(t, map[string]string{"PlayMedia": "Query", "GlobalSearch": "Query", "TypeText": "Text", "OpenFavourite": "Favourite", "LaunchAddon": "Addon"}, got, locale)
	}
}

func TestShippedIdentifyEchoPhrasings(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	m, err := Build(src, "en-AU", BuildOptions{})
	require.NoError(t, err)

	i := slices.IndexFunc(m.InteractionModel.LanguageModel.Intents, func(in ModelIntent) bool { return in.Name == "IdentifyEcho" })
	require.GreaterOrEqual(t, i, 0)

	// The devices page suggests "which device this is"; the question form must work too.
	samples := m.InteractionModel.LanguageModel.Intents[i].Samples
	for _, s := range []string{"which device this is", "which device is this", "what echo is this"} {
		assert.Contains(t, samples, s)
	}
}

func TestBuildWithoutContentGroups(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	names := func(m *InteractionModel) (intents, types []string) {
		for _, in := range m.InteractionModel.LanguageModel.Intents {
			intents = append(intents, in.Name)
		}

		for _, ty := range m.InteractionModel.LanguageModel.Types {
			types = append(types, ty.Name)
		}

		return intents, types
	}

	all, err := Build(src, "en-AU", BuildOptions{SlotValues: map[string][]SlotValue{"AMAZON.Musician": {{Value: "Queen"}}}})
	require.NoError(t, err)
	intents, types := names(all)
	assert.Contains(t, intents, "ListenToArtist", "every group by default")
	assert.Contains(t, types, "AMAZON.Musician")

	noMusic, err := Build(src, "en-AU", BuildOptions{
		Content:    []media.Content{media.ContentMovies, media.ContentTV},
		SlotValues: map[string][]SlotValue{"AMAZON.Musician": {{Value: "Queen"}}, "KODI_MUSIC_VIDEO": {{Value: "Thriller"}}},
	})
	require.NoError(t, err)

	intents, types = names(noMusic)
	assert.Contains(t, intents, "WatchMovie")
	assert.Contains(t, intents, "PlayMedia", "untagged intents stay")
	assert.Contains(t, intents, "AMAZON.PauseIntent")
	assert.NotContains(t, intents, "ListenToArtist")
	assert.NotContains(t, intents, "WatchMusicVideo", "music takes music videos along")
	assert.NotContains(t, types, "AMAZON.Musician", "titles only music used are left out")
	assert.NotContains(t, types, "KODI_MUSIC_VIDEO")
	assert.Contains(t, types, "MEDIA_GENRE", "types still used elsewhere stay")

	moviesOnly, err := Build(src, "en-AU", BuildOptions{Content: []media.Content{media.ContentMovies}})
	require.NoError(t, err)

	intents, _ = names(moviesOnly)
	assert.NotContains(t, intents, "WatchEpisode")
	assert.Contains(t, intents, "WatchRandomMovie")
}

func TestBuildKodiInvocationUsesTheLocaleSpelling(t *testing.T) {
	src := load(t, map[string]string{
		"locales/en-GB.yaml": "language: en\nskill_name: Encore\ninvocation: encore test\nkodi_invocation: kodi player\n",
	})

	for _, tt := range []struct {
		locale, choice, want string
	}{
		{"en-GB", InvocationKodi, "kodi player"},
		{"en-GB", "", "encore test"},
		{"en-GB", "telly", "telly"},
		{"en-US", InvocationKodi, "kodi"},
	} {
		m, err := Build(src, tt.locale, BuildOptions{Invocation: tt.choice})
		require.NoError(t, err)
		assert.Equal(t, tt.want, m.InteractionModel.LanguageModel.InvocationName, "%s with %q", tt.locale, tt.choice)
	}
}
