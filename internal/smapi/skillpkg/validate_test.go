package skillpkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/skillmodel"
)

func modelWithValue(value string, synonyms ...string) *skillmodel.InteractionModel {
	var v skillmodel.ModelTypeValue
	v.Name.Value = value
	v.Name.Synonyms = synonyms

	m := &skillmodel.InteractionModel{}
	m.InteractionModel.LanguageModel = skillmodel.LanguageModel{
		InvocationName: "encore",
		Intents:        []skillmodel.ModelIntent{{Name: "PlayIntent", Samples: []string{"play"}}},
		Types:          []skillmodel.ModelType{{Name: "ROOM", Values: []skillmodel.ModelTypeValue{v}}},
	}

	return m
}

func TestEncodeModelLimits(t *testing.T) {
	atLimit := strings.Repeat("é", MaxSlotValueLength)
	overLimit := strings.Repeat("a", MaxSlotValueLength+1)

	tooManyIntents := modelWithValue("lounge")
	tooManyIntents.InteractionModel.LanguageModel.Intents = make([]skillmodel.ModelIntent, MaxIntents+1)

	maxIntents := modelWithValue("lounge")
	maxIntents.InteractionModel.LanguageModel.Intents = make([]skillmodel.ModelIntent, MaxIntents)

	testCases := []struct {
		name string
		have *skillmodel.InteractionModel
		err  string
	}{
		{name: "ShouldEncodeWithinLimits", have: modelWithValue("lounge", "living room")},
		{name: "ShouldEncodeValueAtTheLimitInCharacters", have: modelWithValue(atLimit, atLimit)},
		{name: "ShouldRejectValueTooLong", have: modelWithValue(overLimit), err: "slot type ROOM value"},
		{name: "ShouldRejectSynonymTooLong", have: modelWithValue("lounge", "den", overLimit), err: "slot type ROOM synonym"},
		{name: "ShouldEncodeIntentsAtTheLimit", have: maxIntents},
		{name: "ShouldRejectTooManyIntents", have: tooManyIntents, err: "251 intents exceeds Amazon's limit of 250"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, warnings, err := encodeModel("en-AU", tc.have)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				assert.Contains(t, err.Error(), "locale en-AU")
				assert.Nil(t, data)

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, data)
			assert.Empty(t, warnings)
		})
	}
}

// modelOfSize returns a model whose encoding is exactly size bytes, padded with one long sample.
func modelOfSize(t *testing.T, size int) *skillmodel.InteractionModel {
	t.Helper()

	m := modelWithValue("lounge")
	base, err := json.Marshal(m)
	require.NoError(t, err)

	m.InteractionModel.LanguageModel.Intents[0].Samples = []string{strings.Repeat("a", size-len(base)+len(`"play"`)-len(`""`))}

	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.Len(t, data, size)

	return m
}

func TestEncodeModelSizeBoundaries(t *testing.T) {
	testCases := []struct {
		name    string
		size    int
		warning bool
		err     bool
	}{
		{name: "ShouldNotWarnAtTheWarningSize", size: WarnModelBytes},
		{name: "ShouldWarnJustOverTheWarningSize", size: WarnModelBytes + 1, warning: true},
		{name: "ShouldWarnAtTheLimit", size: MaxModelBytes, warning: true},
		{name: "ShouldRejectJustOverTheLimit", size: MaxModelBytes + 1, err: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, warnings, err := encodeModel("en-AU", modelOfSize(t, tc.size))
			if tc.err {
				require.ErrorContains(t, err, fmt.Sprintf("interaction model is %d bytes, over Amazon's %d byte limit", tc.size, MaxModelBytes))
				assert.Nil(t, data)

				return
			}

			require.NoError(t, err)
			assert.Len(t, data, tc.size)

			if tc.warning {
				assert.Equal(t, []string{fmt.Sprintf("locale en-AU: interaction model is %d bytes, close to Amazon's %d byte limit", tc.size, MaxModelBytes)}, warnings)
			} else {
				assert.Empty(t, warnings)
			}
		})
	}
}

func TestEncodeModelTruncatesLongValueInError(t *testing.T) {
	_, _, err := encodeModel("en-AU", modelWithValue(strings.Repeat("z", 500)))
	require.Error(t, err)
	assert.Less(t, len(err.Error()), 200, "the error quotes only the start of the value")
}

func TestCountry(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldReadCountry", have: "en-AU", expected: "AU"},
		{name: "ShouldUpperCaseCountry", have: "fr-ca", expected: "CA"},
		{name: "ShouldHaveNoCountryForBareLanguage", have: "de"},
		{name: "ShouldHaveNoCountryForNumericRegion", have: "es-419"},
		{name: "ShouldHaveNoCountryForEmptyLocale"},
		{name: "ShouldHaveNoCountryForEmptyRegion", have: "en-"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, country(tc.have))
		})
	}
}

func TestFilterSlotValuesNil(t *testing.T) {
	out, warnings := filterSlotValues(nil)
	assert.Nil(t, out, "no build-time values stays distinct from an empty set")
	assert.Nil(t, warnings)

	out, warnings = filterSlotValues(map[string][]skillmodel.SlotValue{"ROOM": {}})
	assert.Equal(t, map[string][]skillmodel.SlotValue{"ROOM": {}}, out)
	assert.Nil(t, warnings)
}

func TestPrivacyValidateListsEveryMissingAnswer(t *testing.T) {
	yes := true

	err := Privacy{AllowsPurchases: &yes, IsChildDirected: &yes}.validate()
	require.EqualError(t, err, "privacy and compliance questions unanswered: containsAds, isExportCompliant, usesPersonalInfo")
}

func TestEndpointValidate(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected bool
	}{
		{name: "ShouldAcceptHTTPSWithPath", have: "https://encore.example.com/api/v1/alexa", expected: true},
		{name: "ShouldAcceptHTTPSWithoutPath", have: "https://encore.example.com", expected: true},
		{name: "ShouldRejectHTTP", have: "http://encore.example.com/api/v1/alexa"},
		{name: "ShouldRejectMissingHost", have: "https:///api/v1/alexa"},
		{name: "ShouldRejectMissingScheme", have: "encore.example.com/api/v1/alexa"},
		{name: "ShouldRejectInvalidHost", have: "https://exa mple.com/"},
		{name: "ShouldRejectEmpty"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Endpoint{URI: tc.have}.validate()
			assert.Equal(t, tc.expected, err == nil, "%v", err)
		})
	}
}

type failingWriter struct{}

var errWrite = errors.New("disk full")

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestWriteZipReportsWriterErrors(t *testing.T) {
	// Incompressible data reaches the writer as it is written, rather than when the zip is finished.
	noise := make([]byte, 1<<20)
	_, _ = rand.NewChaCha8([32]byte{}).Read(noise)

	// Each entry's header goes to the writer once enough of them are buffered.
	many := make([]File, 200)
	for i := range many {
		many[i] = File{Name: fmt.Sprintf("models/locale-%03d-with-a-long-enough-name.json", i), Data: []byte("{}")}
	}

	testCases := []struct {
		name     string
		have     []File
		expected string
	}{
		{name: "ShouldReportErrorWhileFinishing", have: []File{{Name: "a.json", Data: []byte(strings.Repeat("x", 1<<16))}}, expected: "skillpkg: finishing zip: "},
		{name: "ShouldReportErrorWhileWritingAFile", have: []File{{Name: "a.json", Data: noise}}, expected: "skillpkg: writing a.json: "},
		{name: "ShouldReportErrorWhileAddingAHeader", have: many, expected: "skillpkg: adding models/locale-"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := writeZip(failingWriter{}, tc.have)
			require.ErrorIs(t, err, errWrite)
			assert.True(t, strings.HasPrefix(err.Error(), tc.expected), err.Error())
		})
	}
}

func TestHashFilesSeparatesNamesFromData(t *testing.T) {
	a := hashFiles([]File{{Name: "ab", Data: []byte("c")}})
	b := hashFiles([]File{{Name: "a", Data: []byte("bc")}})
	assert.NotEqual(t, a, b, "moving bytes between a name and its data changes the hash")

	c := hashFiles([]File{{Name: "a", Data: []byte("b")}, {Name: "c", Data: nil}})
	d := hashFiles([]File{{Name: "a", Data: []byte("bc")}})
	assert.NotEqual(t, c, d)

	assert.Equal(t, hashFiles(nil), hashFiles([]File{}))
	assert.Len(t, hashFiles(nil), 64)
}

func TestCleanSlotText(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldKeepPlainText", have: "Heat", expected: "Heat"},
		{name: "ShouldDropLeadingAsterisk", have: "*NSYNC", expected: "NSYNC"},
		{name: "ShouldDropInnerAsterisks", have: "M*A*S*H", expected: "MASH"},
		{name: "ShouldDropBoldTags", have: "[B]Dune[/B]", expected: "Dune"},
		{name: "ShouldDropColourAndItalicTags", have: "[COLOR red]Alien[/COLOR] [I]Director's Cut[/i]", expected: "Alien Director's Cut"},
		{name: "ShouldDropLowerCaseColourTags", have: "[color=FFFF0000]Up[/color]", expected: "Up"},
		{name: "ShouldReplaceLineBreakTagWithSpace", have: "Line one[CR]Line two", expected: "Line one Line two"},
		{name: "ShouldDropFormattingTags", have: "[UPPERCASE]loud[/UPPERCASE] [LIGHT]x[/LIGHT] [TABS]y [CAPITALIZE]z[/CAPITALIZE] [LOWERCASE]w[/LOWERCASE]", expected: "loud x y z w"},
		{name: "ShouldReplaceWhitespaceControlsWithSpaces", have: "Toy\tStory\n2", expected: "Toy Story 2"},
		{name: "ShouldDropInvisibleCharacters", have: "Bell\x07e\u200b", expected: "Belle"},
		{name: "ShouldTrimAndCollapseSpaces", have: "  Up   ", expected: "Up"},
		{name: "ShouldDropEmoji", have: "🎄 Christmas 🎅🏽 Hits", expected: "Christmas Hits"},
		{name: "ShouldDropSymbols", have: "まどか☆マギカ", expected: "まどかマギカ"},
		{name: "ShouldCleanToEmpty", have: "[B]***[/B] 🎵"},
		{name: "ShouldKeepUnknownTags", have: "[Rec]", expected: "[Rec]"},
		{name: "ShouldKeepAccents", have: "Amélie", expected: "Amélie"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, CleanSlotText(tc.have))
		})
	}
}

func TestFilterSlotValuesCleans(t *testing.T) {
	out, warnings := filterSlotValues(map[string][]skillmodel.SlotValue{"AMAZON.Movie": {
		{ID: "movie/1", Value: "[B]Dune[/B]", Synonyms: []string{"dune", "*", "Dune  Part   One", "dune part one"}},
		{ID: "movie/2", Value: "DUNE", Synonyms: []string{"arrakis"}},
		{ID: "movie/3", Value: "🎬"},
		{ID: "movie/4", Value: "*NSYNC", Synonyms: []string{"[I]nsync[/I]", "n sync"}},
	}})

	assert.Equal(t, map[string][]skillmodel.SlotValue{"AMAZON.Movie": {
		{ID: "movie/1", Value: "Dune", Synonyms: []string{"Dune Part One", "arrakis"}},
		{ID: "movie/4", Value: "NSYNC", Synonyms: []string{"n sync"}},
	}}, out, "values that clean to the same text merge into the first, and values and synonyms left empty are dropped")
	assert.Equal(t, []string{"slot type AMAZON.Movie: dropped 2 values or synonyms with nothing to say once cleaned"}, warnings)
}
