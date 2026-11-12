package smapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

func TestCanSimulate(t *testing.T) {
	for locale, want := range map[string]bool{
		"en-AU": true,
		"en-US": true,
		"ja-JP": true,
		"es-ES": false,
		"en-au": false,
		"en_AU": false,
		"":      false,
	} {
		assert.Equal(t, want, smapi.CanSimulate(locale), locale)
	}

	assert.ElementsMatch(t, smapitest.SimulationLocales, smapi.SimulationLocales, "the fake and the client agree on the supported locales")
}

// enabledSkill imports a package for locales into a new skill and enables it, ready to simulate.
func enabledSkill(t *testing.T, c *smapi.Client, locales ...string) string {
	t.Helper()

	id, err := c.ImportNew(t.Context(), smapitest.DefaultVendorID, upload(t, c, packageZip(t, "encore", locales...)))
	require.NoError(t, err)

	st, err := c.ImportStatus(t.Context(), id)
	require.NoError(t, err)
	require.NoError(t, c.EnableDevelopment(t.Context(), st.Skill.ID))

	return st.Skill.ID
}

func TestSimulateReachesSkill(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{Pending: 1})
	c := newClient(t, fake)
	skillID := enabledSkillAfterPending(t, c, "en-AU")

	simID, err := c.Simulate(t.Context(), skillID, "en-AU", "open encore")
	require.NoError(t, err)
	require.NotEmpty(t, simID)

	sim, err := c.Simulation(t.Context(), skillID, simID)
	require.NoError(t, err)
	assert.Equal(t, smapi.Simulation{ID: simID, Status: smapi.SimulationInProgress}, *sim)

	sim, err = c.Simulation(t.Context(), skillID, simID)
	require.NoError(t, err)
	assert.Equal(t, smapi.SimulationSuccessful, sim.Status)
	assert.True(t, sim.Invoked)
	assert.Equal(t, []string{"Welcome to encore."}, sim.Captions)
	assert.Empty(t, sim.Error)
}

// enabledSkillAfterPending is enabledSkill for a fake whose imports stay pending, polling the import until it finishes.
func enabledSkillAfterPending(t *testing.T, c *smapi.Client, locales ...string) string {
	t.Helper()

	id, err := c.ImportNew(t.Context(), smapitest.DefaultVendorID, upload(t, c, packageZip(t, "encore", locales...)))
	require.NoError(t, err)

	var skillID string

	require.NoError(t, c.Poll(t.Context(), func(ctx context.Context) (bool, error) {
		st, err := c.ImportStatus(ctx, id)
		if err != nil {
			return false, err
		}

		skillID = st.Skill.ID

		return st.Status != smapi.StatusInProgress, nil
	}))
	require.NoError(t, c.EnableDevelopment(t.Context(), skillID))

	return skillID
}

func TestSimulateNotInvoked(t *testing.T) {
	c := newClient(t, smapitest.New(t, smapitest.Options{}))
	skillID := enabledSkill(t, c, "en-US")

	simID, err := c.Simulate(t.Context(), skillID, "en-US", "open something else")
	require.NoError(t, err)

	sim, err := c.Simulation(t.Context(), skillID, simID)
	require.NoError(t, err)
	assert.Equal(t, smapi.SimulationSuccessful, sim.Status)
	assert.False(t, sim.Invoked, "Alexa answered without routing to the skill")
	assert.Equal(t, []string{"Sorry, I don't know that."}, sim.Captions)
}

func TestSimulationReportsFailure(t *testing.T) {
	c := newClient(t, smapitest.New(t, smapitest.Options{UnresolvedSimulations: 1}))
	skillID := enabledSkill(t, c, "en-GB")

	simID, err := c.Simulate(t.Context(), skillID, "en-GB", "open encore")
	require.NoError(t, err)

	sim, err := c.Simulation(t.Context(), skillID, simID)
	require.NoError(t, err)
	assert.Equal(t, smapi.SimulationFailed, sim.Status)
	assert.Equal(t, smapitest.UnresolvedMessage, sim.Error)
	assert.False(t, sim.Invoked)
	assert.Empty(t, sim.Captions)
}

func TestSimulateErrors(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	disabled := fake.AddSkill(smapitest.DefaultVendorID, nil)

	_, err := c.Simulate(t.Context(), disabled, "en-AU", "open encore")
	assert.Equal(t, http.StatusConflict, smapi.StatusCode(err), "a skill must be enabled to simulate")

	_, err = c.Simulate(t.Context(), disabled, "es-ES", "open encore")
	assert.Equal(t, http.StatusBadRequest, smapi.StatusCode(err))

	_, err = c.Simulation(t.Context(), disabled, "sim-missing")
	assert.Equal(t, http.StatusNotFound, smapi.StatusCode(err))
}

func TestSimulationIgnoresEmptyCaptions(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"s","status":"SUCCESSFUL","result":{"skillExecutionInfo":{"invocations":[]},"alexaExecutionInfo":{"alexaResponses":[{"content":null},{"content":{"caption":""}},{"content":{"caption":"Hi"}}]}}}`))
	})

	sim, err := c.Simulation(t.Context(), "skill", "s")
	require.NoError(t, err)
	assert.Equal(t, []string{"Hi"}, sim.Captions)
	assert.False(t, sim.Invoked, "an empty invocations list means the skill was not reached")
}

func TestSayReadsIntentSlotsAndReply(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{Pending: 1})
	c := newClient(t, fake)
	skillID := enabledSkill(t, c, "en-AU")

	ended := true
	fake.Answer("en-AU", "ask encore what's on abc news", smapitest.Answer{
		Intent:     "WhatsOn",
		Slots:      map[string]string{"Channel": "ABC news"},
		Resolved:   map[string]string{"Channel": "ABC NEWS"},
		Speech:     "On ABC NEWS now: ABC News Mornings, until 12:00 pm.",
		Reprompt:   "Anything else?",
		EndSession: &ended,
		Directives: []string{"Dialog.Delegate"},
	})

	sim, err := c.Say(t.Context(), skillID, "en-AU", "ask encore what's on abc news", false)
	require.NoError(t, err)

	assert.Equal(t, smapi.SimulationSuccessful, sim.Status)
	assert.True(t, sim.Invoked)
	assert.Equal(t, "WhatsOn", sim.Intent)
	assert.Equal(t, map[string]smapi.SimulationSlot{"Channel": {Value: "ABC news", Resolved: "ABC NEWS"}}, sim.Slots, "an empty slot is left out, and the value comes from the authority that matched")
	assert.Equal(t, []smapi.SkillResponse{{
		Request:    "IntentRequest",
		Speech:     "On ABC NEWS now: ABC News Mornings, until 12:00 pm.",
		Reprompt:   "<speak>Anything else?</speak>",
		EndSession: &ended,
		Directives: []string{"Dialog.Delegate"},
	}}, sim.Responses)
	assert.Equal(t, []string{"On ABC NEWS now: ABC News Mornings, until 12:00 pm."}, sim.Captions)
}

func TestSayReportsTheIntentWeighedWhenUnresolved(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	skillID := enabledSkill(t, c, "en-AU")

	fake.Answer("en-AU", "ask encore to switch to nine go", smapitest.Answer{Intent: "WatchChannel", Slots: map[string]string{"Channel": "9 go"}, Unresolved: true})

	sim, err := c.Say(t.Context(), skillID, "en-AU", "ask encore to switch to nine go", false)
	require.NoError(t, err)

	assert.Equal(t, smapi.SimulationFailed, sim.Status)
	assert.Equal(t, smapitest.UnresolvedMessage, sim.Error)
	assert.Equal(t, "WatchChannel", sim.Intent)
	assert.Equal(t, map[string]smapi.SimulationSlot{"Channel": {Value: "9 go"}}, sim.Slots)
	assert.False(t, sim.Invoked)
	assert.Empty(t, sim.Responses)
}

func TestSaySessionModes(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	skillID := enabledSkill(t, c, "en-AU")

	for _, inSession := range []bool{false, true} {
		_, err := c.Say(t.Context(), skillID, "en-AU", "open encore", inSession)
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"FORCE_NEW_SESSION", "DEFAULT"}, fake.SessionModes())
}

func TestSayWaitsWhileTheSimulatorIsBusy(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{BusySimulations: 2})
	c := newClient(t, fake)
	skillID := enabledSkill(t, c, "en-AU")

	sim, err := c.Say(t.Context(), skillID, "en-AU", "open encore", false)
	require.NoError(t, err)
	assert.True(t, sim.Invoked)
	assert.Equal(t, 3, fake.Calls("POST /v2/skills/{skillId}/stages/{stage}/simulations"))
}

func TestSayGivesUpWhenTheSimulatorStaysBusy(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{BusySimulations: 100})
	c := newClient(t, fake)
	skillID := enabledSkill(t, c, "en-AU")

	_, err := c.Say(t.Context(), skillID, "en-AU", "open encore", false)
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, smapi.StatusCode(err))
	assert.Equal(t, 10, fake.Calls("POST /v2/skills/{skillId}/stages/{stage}/simulations"))
}

func TestSayErrors(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	skillID := enabledSkill(t, c, "en-AU")

	_, err := c.Say(t.Context(), skillID, "es-ES", "abre encore", false)
	assert.Equal(t, http.StatusBadRequest, smapi.StatusCode(err), "a refusal isn't retried")

	_, err = c.Say(t.Context(), skillID+"-gone", "en-AU", "open encore", false)
	assert.Equal(t, http.StatusNotFound, smapi.StatusCode(err))

	fake.InjectRoute("GET /v2/skills/{skillId}/stages/{stage}/simulations/{simId}", http.StatusForbidden)

	_, err = c.Say(t.Context(), skillID, "en-AU", "open encore", false)
	assert.Equal(t, http.StatusForbidden, smapi.StatusCode(err))
}

func TestSayStopsWithTheContext(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{BusySimulations: 100})
	c := newClient(t, fake)
	skillID := enabledSkill(t, c, "en-AU")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := c.Say(ctx, skillID, "en-AU", "open encore", false)
	require.ErrorIs(t, err, context.Canceled)
}
