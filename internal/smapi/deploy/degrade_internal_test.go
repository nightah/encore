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

	for name, tc := range map[string]struct {
		message string
		dropped []string
	}{
		"double quotes":    {message: `Slot value "ocean's eleven" is invalid`, dropped: []string{"Ocean's Eleven"}},
		"single quotes":    {message: `Invalid value 'Dune' in slot type 'AMAZON.Movie'`, dropped: []string{"Dune"}},
		"curly quotes":     {message: "Invalid value “Lounge”", dropped: []string{"Lounge"}},
		"synonym":          {message: "Synonym «heat 1995» is invalid", dropped: []string{"Heat"}},
		"japanese":         {message: "スロット値「Dune」は無効です", dropped: []string{"Dune"}},
		"unquoted mention": {message: "Dune is too long"},
		"nothing named":    {message: "The interaction model is too large"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, dropped := withoutNamed(values, nil, []smapi.Problem{{Code: "INVALID", Message: tc.message}})
			assert.Equal(t, tc.dropped, dropped)
			assert.Equal(t, countValues(values)-len(tc.dropped), countValues(out))
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
