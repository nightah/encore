package deploy_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

// deployed deploys locales and returns the skill's ID.
func (h *harness) deployed(t *testing.T, invocation string, locales ...string) string {
	t.Helper()

	req := h.request(deploy.State{})
	req.Package.Locales = locales
	req.Package.Invocation = invocation
	req.Simulate = false

	res, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)

	return res.State.SkillID
}

func TestCheckOpenTriesEachLocale(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	skillID := h.deployed(t, "", "en-GB", "de-DE", "fr-FR", "es-ES")

	// German reaches the skill only on its second phrase; French never does.
	h.fake.Reach("de-DE", "starte encore")

	checks, err := h.deployer.CheckOpen(t.Context(), skillID, []string{"en-GB", "de-DE", "fr-FR", "es-ES"}, "", slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.Len(t, checks, 4)

	phrases := func(c deploy.OpenCheck) []string {
		var out []string
		for _, tr := range c.Tries {
			out = append(out, tr.Phrase)
		}

		return out
	}

	assert.Equal(t, "en-GB", checks[0].Locale)
	assert.Equal(t, []string{"open encore"}, phrases(checks[0]))
	assert.True(t, checks[0].Reached())
	assert.False(t, checks[0].Refused())

	assert.Equal(t, []string{"öffne encore", "starte encore"}, phrases(checks[1]), "phrases are tried until one reaches the skill")
	assert.True(t, checks[1].Reached())

	assert.Equal(t, []string{"ouvre kodi", "lance kodi", "démarre kodi", "ouvre la skill kodi"}, phrases(checks[2]), "every phrase is tried, ending with the skill phrase")
	assert.False(t, checks[2].Reached())
	assert.False(t, checks[2].Refused())
	assert.Equal(t, smapi.SimulationSuccessful, checks[2].Tries[0].Simulation.Status)

	assert.Equal(t, []string{"abre encore"}, phrases(checks[3]), "a locale the simulator refuses gets one try")
	assert.True(t, checks[3].Refused())
	assert.False(t, checks[3].Reached())
	assert.Equal(t, smapi.SimulationFailed, checks[3].Tries[0].Simulation.Status)
	assert.Contains(t, checks[3].Tries[0].Simulation.Error, "not supported for simulation")
}

func TestCheckOpenUsesTheInvocationChoice(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	skillID := h.deployed(t, skillmodel.InvocationKodi, "en-AU")

	checks, err := h.deployer.CheckOpen(t.Context(), skillID, []string{"en-AU"}, skillmodel.InvocationKodi, nil)
	require.NoError(t, err)
	require.Len(t, checks, 1)
	assert.Equal(t, "open kodi", checks[0].Tries[0].Phrase)
	assert.True(t, checks[0].Reached())
}

func TestCheckOpenStopsWhenCancelled(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	skillID := h.deployed(t, "", "en-GB")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	checks, err := h.deployer.CheckOpen(ctx, skillID, []string{"en-GB", "en-US"}, "", nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, checks)
}
