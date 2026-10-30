package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi/deploy"
)

// autoDeployHarness is a harness whose library titles the test can add to, with the skill deployed once by hand.
type autoDeployHarness struct {
	*harness

	c      *client
	mu     sync.Mutex
	titles []skillmodel.SlotValue
	// titlesErr, when set, is what reading the library titles fails with.
	titlesErr error
}

func newAutoDeployHarness(t *testing.T) *autoDeployHarness {
	t.Helper()

	ah := &autoDeployHarness{titles: []skillmodel.SlotValue{{ID: "movie/1", Value: "Dune"}}}
	ah.harness = newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.SlotValues = func(context.Context) (map[string][]skillmodel.SlotValue, error) {
			ah.mu.Lock()
			defer ah.mu.Unlock()

			if ah.titlesErr != nil {
				return nil, ah.titlesErr
			}

			return map[string][]skillmodel.SlotValue{"AMAZON.Movie": slices.Clone(ah.titles)}, nil
		}
	}})
	ah.c = ah.admin()
	connectAmazon(t, ah.harness, ah.c)
	ah.c.json(http.MethodPut, "/api/v1/skill/settings", deployableSettings(), http.StatusOK, nil)

	return ah
}

// deployByHand deploys through the API and returns how it ended.
func (ah *autoDeployHarness) deployByHand(t *testing.T) Deploy {
	t.Helper()

	var acc Accepted

	ah.c.json(http.MethodPost, "/api/v1/skill/deploy", map[string]any{}, http.StatusAccepted, &acc)

	return waitForDeploy(t, ah.c, acc.ID)
}

func (ah *autoDeployHarness) addTitle(id, value string) {
	ah.mu.Lock()
	defer ah.mu.Unlock()

	ah.titles = append(ah.titles, skillmodel.SlotValue{ID: id, Value: value})
}

func (ah *autoDeployHarness) setAutoDeploy(t *testing.T, on bool) {
	t.Helper()

	ah.c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"features": map[string]any{"auto_deploy": on}}, http.StatusOK, nil)
}

// check runs one automatic deploy check and waits for any deploy it started.
func (ah *autoDeployHarness) check(t *testing.T) {
	t.Helper()

	ah.api.skill.autoDeploy(t.Context())
	ah.api.bg.Wait()
}

func (ah *autoDeployHarness) skill(t *testing.T) SkillInfo {
	t.Helper()

	var info SkillInfo

	ah.c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)

	return info
}

func TestAutoDeployRedeploysSettledChanges(t *testing.T) {
	ah := newAutoDeployHarness(t)

	first := ah.deployByHand(t)
	require.Equal(t, "succeeded", first.State, first.Error)

	ah.setAutoDeploy(t, true)
	ah.check(t)
	ah.check(t)
	assert.Equal(t, first.ID, ah.skill(t).LastDeploy.V.ID, "nothing changed, so nothing is deployed")

	ah.addTitle("movie/2", "Arrival")
	ah.check(t)

	info := ah.skill(t)
	assert.True(t, info.AutoDeploy)
	assert.True(t, info.NeedsDeploy)
	assert.Equal(t, first.ID, info.LastDeploy.V.ID, "a change is deployed once it has stayed the same for a whole interval")

	ah.addTitle("movie/3", "Contact")
	ah.check(t)
	assert.Equal(t, first.ID, ah.skill(t).LastDeploy.V.ID, "a library sync still adding titles isn't deployed yet")

	ah.check(t)

	info = ah.skill(t)
	require.NotNil(t, info.LastDeploy.V)
	assert.NotEqual(t, first.ID, info.LastDeploy.V.ID)
	assert.Equal(t, "succeeded", info.LastDeploy.V.State, info.LastDeploy.V.Error)
	assert.True(t, info.LastDeploy.V.Automatic)
	assert.False(t, info.NeedsDeploy)
	assert.Empty(t, info.AutoDeployPaused)
	assert.Equal(t, 1, ah.smapi.SkillCount(), "the deployed skill is updated, not a new one created")
}

func TestAutoDeployOffOrNeverDeployed(t *testing.T) {
	ah := newAutoDeployHarness(t)

	ah.setAutoDeploy(t, true)
	ah.check(t)
	ah.check(t)
	assert.Nil(t, ah.skill(t).LastDeploy.V, "the first deploy is left to a person")

	first := ah.deployByHand(t)
	require.Equal(t, "succeeded", first.State, first.Error)

	ah.setAutoDeploy(t, false)
	ah.addTitle("movie/2", "Arrival")
	ah.check(t)
	ah.check(t)

	info := ah.skill(t)
	assert.False(t, info.AutoDeploy)
	assert.True(t, info.NeedsDeploy)
	assert.Equal(t, first.ID, info.LastDeploy.V.ID, "alexa.auto_deploy is off")
}

func TestAutoDeployPausesAfterAFailure(t *testing.T) {
	ah := newAutoDeployHarness(t)

	first := ah.deployByHand(t)
	require.Equal(t, "succeeded", first.State, first.Error)

	ah.setAutoDeploy(t, true)
	ah.smapi.FailLocale("en-AU", "slot value too long")
	ah.addTitle("movie/2", "Arrival")
	ah.check(t)
	ah.check(t)

	info := ah.skill(t)
	require.NotNil(t, info.LastDeploy.V)
	failed := info.LastDeploy.V
	assert.Equal(t, "failed", failed.State)
	assert.True(t, failed.Automatic)
	assert.Contains(t, info.AutoDeployPaused, "slot value too long")

	att, err := ah.api.attention(t.Context())
	require.NoError(t, err)

	byKind := map[string]Attention{}
	for _, a := range att {
		byKind[a.Kind] = a
	}

	require.Contains(t, byKind, "auto_deploy_paused", "%+v", att)
	assert.Contains(t, byKind["auto_deploy_paused"].Detail, "slot value too long")
	assert.Contains(t, byKind["auto_deploy_paused"].Detail, "Deploy the skill by hand")
	assert.NotContains(t, byKind, "deploy_failed", "the pause already names the failure")

	// Further changes wait for someone to look.
	ah.addTitle("movie/3", "Contact")
	ah.check(t)
	ah.check(t)
	assert.Equal(t, failed.ID, ah.skill(t).LastDeploy.V.ID, "paused")

	// A deploy by hand that succeeds resumes them.
	ah.smapi.PassLocale("en-AU")

	fixed := ah.deployByHand(t)
	require.Equal(t, "succeeded", fixed.State, fixed.Error)
	assert.Empty(t, ah.skill(t).AutoDeployPaused)

	ah.addTitle("movie/4", "Sunshine")
	ah.check(t)
	ah.check(t)

	info = ah.skill(t)
	assert.NotEqual(t, fixed.ID, info.LastDeploy.V.ID)
	assert.True(t, info.LastDeploy.V.Automatic)
	assert.Equal(t, "succeeded", info.LastDeploy.V.State, info.LastDeploy.V.Error)
}

func TestAutoDeployPausesWhenADeployCantStart(t *testing.T) {
	ah := newAutoDeployHarness(t)

	first := ah.deployByHand(t)
	require.Equal(t, "succeeded", first.State, first.Error)

	ah.setAutoDeploy(t, true)

	// A deploy by hand is still running: try again at the next check rather than pause.
	ah.api.skill.mu.Lock()
	ah.api.skill.running = "d-running"
	ah.api.skill.mu.Unlock()

	ah.addTitle("movie/2", "Arrival")
	ah.check(t)
	ah.check(t)
	assert.Empty(t, ah.skill(t).AutoDeployPaused, "a running deploy isn't a failure")

	ah.api.skill.mu.Lock()
	ah.api.skill.running = ""
	ah.api.skill.mu.Unlock()

	// A privacy answer withdrawn since the last deploy stops the next one before it starts.
	settings := deployableSettings()
	settings["privacy"].(map[string]any)["contains_ads"] = nil
	ah.c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, nil)
	ah.check(t)
	ah.check(t)

	info := ah.skill(t)
	assert.Equal(t, first.ID, info.LastDeploy.V.ID)
	assert.Contains(t, info.AutoDeployPaused, "privacy")

	att, err := ah.api.attention(t.Context())
	require.NoError(t, err)
	assert.Contains(t, kinds(att), "auto_deploy_paused")
}

func TestAutoDeployWaitsForAmazon(t *testing.T) {
	for name, lose := range map[string]func(*autoDeployHarness){
		"reauthorise": func(ah *autoDeployHarness) {
			require.NoError(t, ah.app.DB().Settings().Set(t.Context(), reauthKey, true))
		},
		"disconnected": func(ah *autoDeployHarness) {
			ah.c.json(http.MethodDelete, "/api/v1/skill/amazon", nil, http.StatusNoContent, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			ah := newAutoDeployHarness(t)

			first := ah.deployByHand(t)
			require.Equal(t, "succeeded", first.State, first.Error)

			ah.setAutoDeploy(t, true)
			lose(ah)
			ah.addTitle("movie/2", "Arrival")
			ah.check(t)
			ah.check(t)

			info := ah.skill(t)
			assert.Equal(t, first.ID, info.LastDeploy.V.ID, "the attention list already asks for Amazon to be reconnected")
			assert.Empty(t, info.AutoDeployPaused)
		})
	}
}

func TestAutoDeployWaitsForTheLibrary(t *testing.T) {
	ah := newAutoDeployHarness(t)

	first := ah.deployByHand(t)
	require.Equal(t, "succeeded", first.State, first.Error)

	ah.setAutoDeploy(t, true)
	ah.addTitle("movie/2", "Arrival")
	ah.mu.Lock()
	ah.titlesErr = errors.New("library index not loaded")
	ah.mu.Unlock()
	ah.check(t)
	ah.check(t)

	info := ah.skill(t)
	assert.Equal(t, first.ID, info.LastDeploy.V.ID)
	assert.Empty(t, info.AutoDeployPaused, "titles that can't be read yet aren't a failed deploy")

	ah.mu.Lock()
	ah.titlesErr = nil
	ah.mu.Unlock()
	ah.check(t)
	ah.check(t)

	info = ah.skill(t)
	assert.NotEqual(t, first.ID, info.LastDeploy.V.ID)
	assert.Equal(t, "succeeded", info.LastDeploy.V.State, info.LastDeploy.V.Error)
}

func TestEndpointWithoutARequestIsTheDeployedOne(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	ctx := t.Context()

	assert.Empty(t, h.api.skill.endpointURL(ctx, skillSettings{}), "nothing to go on")

	h.api.skill.rememberEndpoint(ctx, "https://encore.example.net/api/v1/alexa")
	assert.Equal(t, "https://encore.example.net/api/v1/alexa", h.api.skill.endpointURL(ctx, skillSettings{}))
	assert.Equal(t, "https://other.example.net/api/v1/alexa", h.api.skill.endpointURL(ctx, skillSettings{Endpoint: SkillEndpoint{URL: "https://other.example.net/api/v1/alexa"}}), "a configured endpoint wins")
}

func TestDeployRecordsItsEndpoint(t *testing.T) {
	ah := newAutoDeployHarness(t)

	d := ah.deployByHand(t)
	require.Equal(t, "succeeded", d.State, d.Error)

	var ep string

	require.NoError(t, ah.app.DB().Settings().Get(t.Context(), deployEndpointKey, &ep))
	assert.Equal(t, "https://encore.example.net/api/v1/alexa", ep)

	var state deploy.State

	require.NoError(t, ah.app.DB().Settings().Get(t.Context(), deployStateKey, &state))
	assert.NotEmpty(t, state.SkillID)
}

// forgetEndpoint leaves a deployed skill with neither a configured nor a recorded endpoint, as a skill deployed before Encore recorded it is after an upgrade.
func (ah *autoDeployHarness) forgetEndpoint(t *testing.T) {
	t.Helper()

	settings := deployableSettings()
	settings["endpoint"] = map[string]any{"url": ""}
	ah.c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, nil)
	ah.api.skill.rememberEndpoint(t.Context(), "")
}

func TestAutoDeployRecoversTheEndpointFromAmazon(t *testing.T) {
	ah := newAutoDeployHarness(t)

	first := ah.deployByHand(t)
	require.Equal(t, "succeeded", first.State, first.Error)

	ah.setAutoDeploy(t, true)
	ah.forgetEndpoint(t)
	ah.addTitle("movie/2", "Arrival")
	ah.check(t)
	ah.check(t)

	info := ah.skill(t)
	assert.Empty(t, info.AutoDeployPaused, "the deployed skill's manifest names the endpoint")
	require.NotNil(t, info.LastDeploy.V)
	assert.NotEqual(t, first.ID, info.LastDeploy.V.ID)
	assert.True(t, info.LastDeploy.V.Automatic)
	assert.Equal(t, "succeeded", info.LastDeploy.V.State, info.LastDeploy.V.Error)
	assert.Equal(t, "https://encore.example.net/api/v1/alexa", ah.api.skill.endpointURL(t.Context(), skillSettings{}), "and remembered for the next one")
}

func TestAutoDeployRetriesWhenAmazonCantSayTheEndpoint(t *testing.T) {
	ah := newAutoDeployHarness(t)

	first := ah.deployByHand(t)
	require.Equal(t, "succeeded", first.State, first.Error)

	ah.setAutoDeploy(t, true)
	ah.forgetEndpoint(t)
	ah.addTitle("movie/2", "Arrival")
	ah.smapi.Inject(http.StatusBadRequest, 1)
	ah.check(t)
	assert.Equal(t, 1, ah.smapi.Injected())

	info := ah.skill(t)
	assert.Empty(t, info.AutoDeployPaused, "an Amazon hiccup isn't a failed deploy; the next check asks again")
	assert.Equal(t, first.ID, info.LastDeploy.V.ID)

	ah.check(t)
	ah.check(t)

	info = ah.skill(t)
	assert.NotEqual(t, first.ID, info.LastDeploy.V.ID)
	assert.Equal(t, "succeeded", info.LastDeploy.V.State, info.LastDeploy.V.Error)
}

func TestAutoDeploySkipsWithoutAmazonCredentials(t *testing.T) {
	ah := newAutoDeployHarness(t)

	first := ah.deployByHand(t)
	require.Equal(t, "succeeded", first.State, first.Error)

	ah.setAutoDeploy(t, true)
	ah.forgetEndpoint(t)
	require.NoError(t, ah.app.Secrets().Delete(t.Context(), lwaSecretRef))
	ah.addTitle("movie/2", "Arrival")
	ah.check(t)

	assert.Contains(t, ah.logs.String(), "reading the deployed skill's endpoint from Amazon")
	info := ah.skill(t)
	assert.Equal(t, first.ID, info.LastDeploy.V.ID)
	assert.Empty(t, info.AutoDeployPaused)
}

func TestAutoDeployDatabaseErrors(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	ctx := t.Context()
	s := h.api.skill

	require.NoError(t, h.app.DB().Close())

	require.Error(t, s.recoverEndpoint(ctx, "amzn1.ask.skill.1"))

	s.setAutoDeployPaused(ctx, "build failed")
	assert.Contains(t, h.logs.String(), "saving automatic deploy pause")

	s.rememberEndpoint(ctx, "https://encore.example.net/api/v1/alexa")
	assert.Contains(t, h.logs.String(), "saving the skill endpoint")
}

func TestAutoDeployInterval(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.InDelta(t, 5, s.Features.AutoDeployMinutes, 0)
	assert.Equal(t, 5*time.Minute, h.api.skill.autoDeployInterval(t.Context()))

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"features": map[string]any{"auto_deploy_minutes": 2.5}}, http.StatusOK, &s)
	assert.InDelta(t, 2.5, s.Features.AutoDeployMinutes, 0)
	assert.Equal(t, 150*time.Second, h.api.skill.autoDeployInterval(t.Context()))

	for _, n := range []float64{0, 0.5, 1441} {
		assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"features": map[string]any{"auto_deploy_minutes": n}}).status, n)
	}
}

func TestEvery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var interval, checks atomic.Int64

		interval.Store(int64(150 * time.Second))

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)
			every(ctx, func(context.Context) time.Duration { return time.Duration(interval.Load()) }, func(context.Context) { checks.Add(1) })
		}()

		advance := func(d time.Duration) {
			time.Sleep(d)
			synctest.Wait()
		}

		advance(149 * time.Second)
		assert.Zero(t, checks.Load(), "not before the interval")

		advance(time.Second)
		assert.EqualValues(t, 1, checks.Load(), "fractions of a minute are kept")

		advance(150 * time.Second)
		assert.EqualValues(t, 2, checks.Load())

		interval.Store(int64(10 * time.Minute))
		advance(time.Minute)
		interval.Store(int64(90 * time.Second))
		advance(time.Minute)
		assert.EqualValues(t, 3, checks.Load(), "a shorter interval applies at the next tick, not after the longer wait")

		cancel()
		<-done
	})
}
