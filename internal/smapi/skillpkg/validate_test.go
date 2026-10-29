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

	for name, tc := range map[string]struct {
		model *skillmodel.InteractionModel
		err   string
	}{
		"within limits":                     {model: modelWithValue("lounge", "living room")},
		"value at the limit, in characters": {model: modelWithValue(atLimit, atLimit)},
		"value too long":                    {model: modelWithValue(overLimit), err: "slot type ROOM value"},
		"synonym too long":                  {model: modelWithValue("lounge", "den", overLimit), err: "slot type ROOM synonym"},
		"intents at the limit":              {model: maxIntents},
		"too many intents":                  {model: tooManyIntents, err: "251 intents exceeds Amazon's limit of 250"},
	} {
		t.Run(name, func(t *testing.T) {
			data, warnings, err := encodeModel("en-AU", tc.model)
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
	for _, tc := range []struct {
		size    int
		warning bool
		err     bool
	}{
		{size: WarnModelBytes},
		{size: WarnModelBytes + 1, warning: true},
		{size: MaxModelBytes, warning: true},
		{size: MaxModelBytes + 1, err: true},
	} {
		data, warnings, err := encodeModel("en-AU", modelOfSize(t, tc.size))
		if tc.err {
			require.ErrorContains(t, err, fmt.Sprintf("interaction model is %d bytes, over Amazon's %d byte limit", tc.size, MaxModelBytes))
			assert.Nil(t, data)

			continue
		}

		require.NoError(t, err, tc.size)
		assert.Len(t, data, tc.size)

		if tc.warning {
			assert.Equal(t, []string{fmt.Sprintf("locale en-AU: interaction model is %d bytes, close to Amazon's %d byte limit", tc.size, MaxModelBytes)}, warnings)
		} else {
			assert.Empty(t, warnings, tc.size)
		}
	}
}

func TestEncodeModelTruncatesLongValueInError(t *testing.T) {
	_, _, err := encodeModel("en-AU", modelWithValue(strings.Repeat("z", 500)))
	require.Error(t, err)
	assert.Less(t, len(err.Error()), 200, "the error quotes only the start of the value")
}

func TestCountry(t *testing.T) {
	for locale, want := range map[string]string{
		"en-AU":  "AU",
		"fr-ca":  "CA",
		"de":     "",
		"es-419": "",
		"":       "",
		"en-":    "",
	} {
		assert.Equal(t, want, country(locale), locale)
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
	for uri, ok := range map[string]bool{
		"https://encore.example.com/api/v1/alexa": true,
		"https://encore.example.com":              true,
		"http://encore.example.com/api/v1/alexa":  false,
		"https:///api/v1/alexa":                   false,
		"encore.example.com/api/v1/alexa":         false,
		"https://exa mple.com/":                   false,
		"":                                        false,
	} {
		err := Endpoint{URI: uri}.validate()
		assert.Equal(t, ok, err == nil, "%q: %v", uri, err)
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

	for name, tc := range map[string]struct {
		files []File
		want  string
	}{
		"while finishing":       {[]File{{Name: "a.json", Data: []byte(strings.Repeat("x", 1<<16))}}, "skillpkg: finishing zip: "},
		"while writing a file":  {[]File{{Name: "a.json", Data: noise}}, "skillpkg: writing a.json: "},
		"while adding a header": {many, "skillpkg: adding models/locale-"},
	} {
		t.Run(name, func(t *testing.T) {
			err := writeZip(failingWriter{}, tc.files)
			require.ErrorIs(t, err, errWrite)
			assert.True(t, strings.HasPrefix(err.Error(), tc.want), err.Error())
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
