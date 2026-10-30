package smapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

func TestProfileNLU(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	skillID := fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"nl-NL": "Encore"})

	fake.Profile("nl-NL", "speel de film dune in de woonkamer", smapitest.NLUAnswer{
		Intent:     "WatchMovie",
		Slots:      map[string]string{"Movie": "dune", "Room": "woonkamer"},
		Resolved:   map[string]string{"Room": "living_room"},
		Considered: []string{"PlayMedia", "WatchMusicVideo"},
	})

	p, err := c.ProfileNLU(t.Context(), skillID, "nl-NL", "speel de film dune in de woonkamer")
	require.NoError(t, err)
	assert.Equal(t, "WatchMovie", p.Intent())
	assert.False(t, p.SessionEnded)
	assert.Equal(t, "dune", p.Selected.Slots["Movie"].Value)
	assert.Empty(t, p.Selected.Slots["Movie"].Resolved())
	assert.Equal(t, []string{"woonkamer", "living_room"}, p.Selected.Slots["Room"].Resolved())
	require.Len(t, p.Considered, 2)
	assert.Equal(t, "PlayMedia", p.Considered[0].Name)
}

func TestProfileNLUNoIntent(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	skillID := fake.AddSkill(smapitest.DefaultVendorID, nil)

	p, err := c.ProfileNLU(t.Context(), skillID, "en-GB", "bananas")
	require.NoError(t, err)
	assert.Empty(t, p.Intent())
	assert.Nil(t, p.Selected)

	var none *smapi.NLUProfile
	assert.Empty(t, none.Intent())
}

func TestProfileNLURetriesServerErrors(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	skillID := fake.AddSkill(smapitest.DefaultVendorID, nil)
	fake.Profile("en-GB", "volume up", smapitest.NLUAnswer{Intent: "VolumeUp"})

	fake.Inject(http.StatusInternalServerError, 2)

	p, err := c.ProfileNLU(t.Context(), skillID, "en-GB", "volume up")
	require.NoError(t, err, "Amazon's occasional 500s are retried, as the request body can be sent again")
	assert.Equal(t, "VolumeUp", p.Intent())
	assert.Equal(t, 2, fake.Injected())
	assert.Equal(t, 1, fake.Profiled("en-GB", "volume up"))
}

func TestProfileNLUErrors(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	_, err := c.ProfileNLU(t.Context(), "amzn1.ask.skill.missing", "en-GB", "volume up")
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, smapi.StatusCode(err))
	assert.Contains(t, err.Error(), "profile utterance")

	skillID := fake.AddSkill(smapitest.DefaultVendorID, nil)

	_, err = c.ProfileNLU(t.Context(), skillID, "en-GB", "")
	assert.Equal(t, http.StatusBadRequest, smapi.StatusCode(err))

	fake.Profile("en-GB", "mute", smapitest.NLUAnswer{Status: http.StatusForbidden})

	_, err = c.ProfileNLU(t.Context(), skillID, "en-GB", "mute")
	assert.Equal(t, http.StatusForbidden, smapi.StatusCode(err))
}

// TestNLUSlotResolvedFlat reads resolved values laid out as Amazon's spec documents them, name and id straight on the item.
func TestNLUSlotResolvedFlat(t *testing.T) {
	var s smapi.NLUSlot
	require.NoError(t, json.Unmarshal([]byte(`{"name":"SeekDirection","value":"vooruit","resolutions":{"resolutionsPerAuthority":[{"authority":"a","status":{"code":"ER_SUCCESS_MATCH"},"values":[{"name":"forward","id":"forward"}]}]}}`), &s))
	assert.Equal(t, []string{"forward", "forward"}, s.Resolved())
}
