package skillpkg

import (
	"errors"
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
	err := writeZip(failingWriter{}, []File{{Name: "a.json", Data: []byte(strings.Repeat("x", 1<<16))}})
	require.ErrorIs(t, err, errWrite)
	assert.Contains(t, err.Error(), "skillpkg:")
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
