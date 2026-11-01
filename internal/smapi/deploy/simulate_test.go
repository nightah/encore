package deploy_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

func TestOpenPhraseLanguages(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	for locale, want := range map[string]string{
		"en-AU": "open lounge remote",
		"en-IN": "open lounge remote",
		"fr-FR": "ouvre lounge remote",
		"fr-CA": "ouvre lounge remote",
		"de-DE": "öffne lounge remote",
		"es-MX": "abre lounge remote",
		"hi-IN": "lounge remote शुरू करो",
		"de":    "",
		"":      "",
	} {
		assert.Equal(t, want, deploy.OpenPhrase(src, locale, "lounge remote"), locale)
	}
}

func TestDeploySkipsSimulationWithoutSupportedLocale(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	req := h.request(deploy.State{})
	req.Package.Locales = []string{"es-ES", "es-MX", "es-US", "it-IT"}

	res, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)
	assert.NotEmpty(t, res.State.Hash, "the deploy succeeds without a smoke test")
	assert.Contains(t, h.timeline(), "simulate:skipped")
	assert.Equal(t, 0, h.fake.Calls("POST /v2/skills/{skillId}/stages/{stage}/simulations"))
}

func TestDeploySimulationNotInvokedIsWarning(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.override("GET /v2/skills/{id}/stages/{stage}/simulations/{sim}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"id":"s","status":"SUCCESSFUL","result":{"alexaExecutionInfo":{"alexaResponses":[{"content":{"caption":"Hmm,"}},{"content":{"caption":"I don't know that."}}]}}}`)
	})

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)
	assert.NotEmpty(t, res.State.Hash)
	assert.Equal(t, []string{"en-GB: “open encore” did not reach the skill; Alexa said “Hmm, I don't know that.”"}, res.Warnings)
	require.NotNil(t, res.Locales["en-GB"].Simulation)
	assert.False(t, res.Locales["en-GB"].Simulation.Invoked)
}

func TestDeploySimulationRequestErrorIsWarning(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.override("POST /v2/skills/{id}/stages/{stage}/simulations", reject)

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err, "the skill is live for testing even when the smoke test cannot run")
	assert.NotEmpty(t, res.State.Hash)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], "en-GB: simulating “open encore” failed:")
	assert.Contains(t, res.Warnings[0], "rejected by test")
	assert.Equal(t, smapi.SimulationFailed, res.Locales["en-GB"].Simulation.Status)
	assert.Contains(t, h.timeline(), "simulate:done")
}

func TestDeploySimulationPollErrorIsWarning(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.override("GET /v2/skills/{id}/stages/{stage}/simulations/{sim}", reject)

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], "rejected by test")
}

func TestDeployCancelledDuringSimulation(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	req := h.request(deploy.State{})
	progress := req.Progress
	req.Progress = func(ev deploy.Event) {
		progress(ev)

		if ev.Step == deploy.StepSimulate && ev.State == deploy.StateRunning {
			cancel()
		}
	}

	res, err := h.deployer.Deploy(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, res.State.Hash, "a deploy cut short is not recorded as done")
	assert.NotEmpty(t, res.State.SkillID)
	assert.Contains(t, h.timeline(), "simulate:failed")
}
