package deploy

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
)

func TestWithoutNamed(t *testing.T) {
	t.Parallel()

	values := map[string][]skillmodel.SlotValue{
		"AMAZON.Movie": {{Value: "Ocean's Eleven"}, {Value: "Heat", Synonyms: []string{"heat 1995"}}, {Value: "Dune"}, {Value: ""}},
		"KODI_ROOM":    {{Value: "Lounge"}},
	}

	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldDropValueInDoubleQuotes", have: `Slot value "ocean's eleven" is invalid`, expected: []string{"Ocean's Eleven"}},
		{name: "ShouldDropValueInSingleQuotes", have: `Invalid value 'Dune' in slot type 'AMAZON.Movie'`, expected: []string{"Dune"}},
		{name: "ShouldDropValueInCurlyQuotes", have: "Invalid value “Lounge”", expected: []string{"Lounge"}},
		{name: "ShouldDropValueOfQuotedSynonym", have: "Synonym «heat 1995» is invalid", expected: []string{"Heat"}},
		{name: "ShouldDropValueInJapaneseQuotes", have: "スロット値「Dune」は無効です", expected: []string{"Dune"}},
		{name: "ShouldIgnoreUnquotedMention", have: "Dune is too long"},
		{name: "ShouldDropNothingWhenNothingIsNamed", have: "The interaction model is too large"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, dropped := withoutNamed(values, nil, []smapi.Problem{{Code: "INVALID", Message: tc.have}})
			assert.Equal(t, tc.expected, dropped)
			assert.Equal(t, countValues(values)-len(tc.expected), countValues(out))
		})
	}
}

func TestThinned(t *testing.T) {
	t.Parallel()

	values := map[string][]skillmodel.SlotValue{
		"AMAZON.Movie":    {{Value: "A"}, {Value: "B"}, {Value: "C"}, {Value: "D"}, {Value: "E"}},
		"AMAZON.TVSeries": {{Value: "Only"}},
		"KODI_ROOM":       {{Value: "Lounge"}, {Value: "Bedroom"}},
	}

	out := thinned(values, []string{"KODI_ROOM"})
	assert.Equal(t, []skillmodel.SlotValue{{Value: "B"}, {Value: "D"}}, out["AMAZON.Movie"])
	assert.Empty(t, out["AMAZON.TVSeries"])
	assert.Equal(t, values["KODI_ROOM"], out["KODI_ROOM"])
	assert.Len(t, values["AMAZON.Movie"], 5, "the values asked for are left alone")
}

func TestDegradeOnlyOnce(t *testing.T) {
	t.Parallel()

	r := &run{retried: true}
	assert.False(t, r.degrade(&Error{Locales: map[string][]smapi.Problem{"en-US": {{Message: "x"}}}}))
	assert.False(t, (&run{}).degrade(&Error{Step: StepImport, Problems: []smapi.Problem{{Message: "x"}}}))
}
