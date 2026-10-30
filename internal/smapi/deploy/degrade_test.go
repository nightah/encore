package deploy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

func titles(n int) []skillmodel.SlotValue {
	out := make([]skillmodel.SlotValue, n)
	for i := range out {
		out[i] = skillmodel.SlotValue{ID: fmt.Sprintf("movie/%d", i), Value: fmt.Sprintf("Movie %02d", i)}
	}

	return out
}

// deployedValues lists the values of slotType in the model the fake holds for locale.
func (h *harness) deployedValues(t *testing.T, skillID, locale, slotType string) []string {
	t.Helper()

	sk, ok := h.fake.Skill(skillID)
	require.True(t, ok)

	var m skillmodel.InteractionModel

	require.NoError(t, json.Unmarshal(sk.Models[locale], &m))

	var out []string

	for _, typ := range m.InteractionModel.LanguageModel.Types {
		if typ.Name == slotType {
			for _, v := range typ.Values {
				out = append(out, v.Name.Value)
			}
		}
	}

	return out
}

func (h *harness) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	var out []string

	for _, ev := range h.events {
		if ev.Message != "" {
			out = append(out, string(ev.Step)+": "+ev.Message)
		}
	}

	return out
}

func degradeRequest(h *harness) deploy.Request {
	req := h.request(deploy.State{})
	req.Package.SlotValues = map[string][]skillmodel.SlotValue{
		"AMAZON.Movie": titles(40),
		"KODI_ROOM":    {{ID: "room/1", Value: "Lounge"}, {ID: "room/2", Value: "Bedroom"}},
	}
	req.KeepSlots = []string{"KODI_ROOM"}

	return req
}

func TestDeployThinsALocaleOthersBuiltWithout(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.LimitSlotValues("en-GB", "AMAZON.Movie", 25)

	req := degradeRequest(h)

	res, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)

	full, err := h.deployer.Build(req.Package)
	require.NoError(t, err)
	assert.Equal(t, full.Hash(), res.State.Hash, "the package asked for counts as deployed, so the next check doesn't deploy it again")

	require.Contains(t, res.Degraded, "en-GB")
	assert.NotContains(t, res.Degraded, "en-US")

	d := res.Degraded["en-GB"]
	assert.Equal(t, 42, d.Total)
	assert.Equal(t, 22, d.Kept, "every other title, and every room")
	assert.Empty(t, d.Dropped)
	assert.Positive(t, d.Bytes)
	require.Len(t, d.Reason, 1)
	assert.Contains(t, d.Reason[0].Message, "too large")

	assert.Len(t, h.deployedValues(t, res.State.SkillID, "en-US", "AMAZON.Movie"), 40)
	gb := h.deployedValues(t, res.State.SkillID, "en-GB", "AMAZON.Movie")
	assert.Len(t, gb, 20)
	assert.Equal(t, "Movie 01", gb[0])
	assert.ElementsMatch(t, []string{"Lounge", "Bedroom"}, h.deployedValues(t, res.State.SkillID, "en-GB", "KODI_ROOM"))

	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-GB"].Status)
	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-US"].Status)

	sk, _ := h.fake.Skill(res.State.SkillID)
	assert.Equal(t, 2, sk.Imports)
	assert.Contains(t, h.messages(), "build: Amazon could not build en-GB; deploying again with fewer library titles there")
	assert.Contains(t, h.timeline(), "simulate:done")
}

func TestDeployThinsOnlyOnce(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.LimitSlotValues("en-GB", "AMAZON.Movie", 5)

	res, err := h.deployer.Deploy(t.Context(), degradeRequest(h))
	require.ErrorIs(t, err, deploy.ErrBuildFailed)
	assert.False(t, deploy.Transient(err))
	assert.Empty(t, res.Degraded, "a second attempt that failed too degraded nothing")
	assert.Empty(t, res.State.Hash)

	sk, _ := h.fake.Skill(res.State.SkillID)
	assert.Equal(t, 2, sk.Imports, "one second attempt, not one per failure")
	assert.Equal(t, smapi.StatusFailed, res.Locales["en-GB"].Status)
}

func TestDeployDoesNotThinWhenEveryLocaleFailed(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.LimitSlotValues("en-GB", "AMAZON.Movie", 25)
	h.fake.LimitSlotValues("en-US", "AMAZON.Movie", 25)

	res, err := h.deployer.Deploy(t.Context(), degradeRequest(h))
	require.ErrorIs(t, err, deploy.ErrBuildFailed)

	sk, _ := h.fake.Skill(res.State.SkillID)
	assert.Equal(t, 1, sk.Imports, "with nothing built, fewer titles everywhere is no better than the failure")
}

func TestDeployLeavesOutANamedValue(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.RejectSlotValue("Movie 07")

	req := degradeRequest(h)
	req.Package.Locales = []string{"en-AU"}

	res, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err, "a value Amazon names is left out even when every locale failed")

	require.Contains(t, res.Degraded, "en-AU")
	d := res.Degraded["en-AU"]
	assert.Equal(t, []string{"Movie 07"}, d.Dropped)
	assert.Equal(t, 41, d.Kept)
	assert.Equal(t, 42, d.Total)

	values := h.deployedValues(t, res.State.SkillID, "en-AU", "AMAZON.Movie")
	assert.Len(t, values, 39)
	assert.NotContains(t, values, "Movie 07")
}

func TestDeployDoesNotThinAnUnexplainedFailure(t *testing.T) {
	h := newHarness(t, smapitest.Options{ImportWithoutModels: true})
	h.override("GET /v1/skills/{id}/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"interactionModel":{"en-US":{"lastUpdateRequest":{"status":"SUCCEEDED"}},"en-GB":{"lastUpdateRequest":{"status":"FAILED"}}}}`)
	})

	res, err := h.deployer.Deploy(t.Context(), degradeRequest(h))
	require.ErrorIs(t, err, deploy.ErrBuildFailed)
	assert.Empty(t, res.Degraded)
	assert.True(t, deploy.Transient(err), "a failure Amazon gives no reason for is tried again later as it is: %v", err)

	sk, _ := h.fake.Skill(res.State.SkillID)
	assert.Equal(t, 1, sk.Imports)
}

// A deploy skipped because the package is unchanged reports the locales the deploy it stands for built with fewer titles, and one that changed the package forgets them.
func TestDeploySkippedKeepsTheDegradation(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.LimitSlotValues("en-GB", "AMAZON.Movie", 25)

	req := degradeRequest(h)

	first, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)
	require.Contains(t, first.Degraded, "en-GB")

	req.State = first.State

	again, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)
	require.True(t, again.Skipped)
	assert.Equal(t, first.Degraded, again.Degraded, "the skipped deploy still has Amazon's model without the titles it dropped")

	req.State = again.State
	req.Package.SlotValues["AMAZON.Movie"] = titles(10)

	changed, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)
	require.False(t, changed.Skipped)
	assert.Empty(t, changed.Degraded, "a package that built in full is deployed in full")
	assert.Empty(t, changed.State.Degraded)
}

// When the package with fewer titles doesn't build, the deploy fails with Amazon's reason rather than try it.
func TestDeployKeepsTheFailureWhenThinningDoesNotBuild(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.LimitSlotValues("en-GB", "AMAZON.Movie", 25)

	req := degradeRequest(h)
	progress := req.Progress
	req.Progress = func(ev deploy.Event) {
		progress(ev)

		// The sources change once Amazon fails the import, between the two builds, so only the second fails.
		if ev.Step == deploy.StepImport && ev.State == deploy.StateFailed {
			loc := h.src.Locales["en-GB"]
			loc.SkillName = "x"
			h.src.Locales["en-GB"] = loc
		}
	}

	res, err := h.deployer.Deploy(t.Context(), req)
	require.ErrorIs(t, err, deploy.ErrBuildFailed, "Amazon's failure stands")
	assert.Empty(t, res.Degraded)

	sk, _ := h.fake.Skill(res.State.SkillID)
	assert.Equal(t, 1, sk.Imports, "nothing more was imported")
}
