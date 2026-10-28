package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/skillpkg"
)

// deployableSettings is a skill settings body that passes every check before a deploy.
func deployableSettings() map[string]any {
	return map[string]any{
		"invocation":      "encore",
		"skill_name":      "",
		"locales_enabled": []string{"en-AU"},
		"endpoint":        map[string]any{"url": "https://encore.example.net/api/v1/alexa"},
		"privacy":         map[string]any{"uses_personal_info": false, "is_child_directed": false, "is_export_compliant": true, "contains_ads": false, "allows_purchases": false},
	}
}

func waitForDeploy(t *testing.T, c *client, id string) Deploy {
	t.Helper()

	var d Deploy

	require.Eventually(t, func() bool {
		c.json(http.MethodGet, "/api/v1/skill/deploy/"+id, nil, http.StatusOK, &d)

		return d.State != "in_progress"
	}, 20*time.Second, 50*time.Millisecond)

	return d
}

func TestDeployRecordViewsHaveNoNulls(t *testing.T) {
	t.Parallel()

	d := &deployRecord{ID: "d-1", State: "in_progress"}

	sum := d.summary()
	assert.NotNil(t, sum.Locales)
	assert.NotNil(t, sum.Warnings)

	v := d.view()
	assert.NotNil(t, v.Events)
	assert.Equal(t, Deploy{ID: "d-1", State: "in_progress", Events: []DeployEvent{}}, v)

	d.Locales = []LocaleDeploy{{Locale: "en-AU"}}
	assert.Equal(t, d.Locales, d.summary().Locales)
}

func TestDeployRecordClone(t *testing.T) {
	t.Parallel()

	d := &deployRecord{ID: "d-1", Events: make([]DeployEvent, 1, 8), Locales: []LocaleDeploy{{Locale: "en-AU"}}, Warnings: []string{"big"}}

	c := d.clone()
	c.Events = append(c.Events, DeployEvent{Step: "build"})
	c.Events[0].Step = "changed"
	c.Locales[0].Locale = "en-GB"
	c.Warnings[0] = "changed"

	assert.Equal(t, []DeployEvent{{}}, d.Events, "a running deploy's record is unaffected by its copy")
	assert.Equal(t, "en-AU", d.Locales[0].Locale)
	assert.Equal(t, "big", d.Warnings[0])
}

func TestStepState(t *testing.T) {
	t.Parallel()

	for in, want := range map[deploy.StepState]string{
		deploy.StateRunning: "started",
		deploy.StateDone:    "succeeded",
		deploy.StateSkipped: "skipped",
		deploy.StateFailed:  "failed",
		"":                  "failed",
	} {
		assert.Equal(t, want, stepState(in), in)
	}
}

func TestLocaleStatuses(t *testing.T) {
	t.Parallel()

	for in, want := range map[smapi.Status]string{
		smapi.StatusSucceeded:  "succeeded",
		smapi.StatusFailed:     "failed",
		smapi.StatusInProgress: "in_progress",
		smapi.StatusSkipped:    "in_progress",
	} {
		assert.Equal(t, want, localeStatus(in), in)
	}

	got := localeStatuses(map[string]deploy.LocaleStatus{
		"en-AU": {Status: smapi.StatusFailed, Errors: []smapi.Problem{{Code: "INVALID_SLOT", Message: "too long"}, {Message: "and another"}}},
		"en-GB": {Status: smapi.StatusSucceeded},
	})
	assert.Equal(t, map[string]LocaleStatus{
		"en-AU": {Status: "failed", Errors: []string{"INVALID_SLOT: too long", "and another"}},
		"en-GB": {Status: "succeeded", Errors: []string{}},
	}, got)

	assert.Empty(t, localeStatuses(nil))
}

func TestLocaleResults(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	s := &skillService{a: &API{now: func() time.Time { return now }}}

	pkg := &skillpkg.Package{Files: []skillpkg.File{
		{Name: "skill-package/skill.json", Data: make([]byte, 99)},
		{Name: skillpkg.ModelDir + "en-AU.json", Data: make([]byte, 1200)},
		{Name: skillpkg.ModelDir + "en-GB.json", Data: make([]byte, 1300)},
	}}
	opts := skillpkg.Options{
		Locales:    []string{"en-GB", "en-AU"},
		SlotValues: map[string][]skillmodel.SlotValue{"AMAZON.Movie": {{ID: "movie/1"}, {ID: "movie/2"}}, "AMAZON.TVSeries": {{ID: "tvshow/1"}}},
	}

	t.Run("built", func(t *testing.T) {
		t.Parallel()

		res := &deploy.Result{Locales: map[string]deploy.LocaleStatus{
			"en-GB": {Status: smapi.StatusFailed, Errors: []smapi.Problem{{Message: "slot value too long"}}},
			"en-AU": {Status: smapi.StatusSucceeded},
		}}

		assert.Equal(t, []LocaleDeploy{
			{Locale: "en-AU", Status: "succeeded", Errors: []string{}, Titles: 3, SizeBytes: 1200, BuiltAt: &now},
			{Locale: "en-GB", Status: "failed", Errors: []string{"slot value too long"}, Titles: 3, SizeBytes: 1300, BuiltAt: &now},
		}, s.localeResults(res, pkg, nil, opts))
	})

	t.Run("skipped", func(t *testing.T) {
		t.Parallel()

		got := s.localeResults(&deploy.Result{Skipped: true}, pkg, nil, opts)
		require.Len(t, got, 2)
		assert.Equal(t, []string{"en-AU", "en-GB"}, []string{got[0].Locale, got[1].Locale}, "the enabled locales, sorted")
		assert.Equal(t, "succeeded", got[0].Status, "an unchanged package is still deployed")
		assert.Equal(t, 1200, got[0].SizeBytes)
		assert.Equal(t, []string{"en-GB", "en-AU"}, opts.Locales, "the options are left as they were")
	})

	t.Run("package didn't build", func(t *testing.T) {
		t.Parallel()

		got := s.localeResults(&deploy.Result{}, nil, errors.New("too big"), opts)
		require.Len(t, got, 2)
		assert.Zero(t, got[0].SizeBytes, "no size without a package")
		assert.Zero(t, got[1].SizeBytes)
	})
}

func TestDeployRefusedUntilReady(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	settings := deployableSettings()
	settings["endpoint"] = map[string]any{"url": ""}
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, nil)

	p := c.do(http.MethodPost, "/api/v1/skill/deploy", nil).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status, "the endpoint derived from a plain-HTTP address can't be deployed")
	require.Len(t, p.Errors, 1)
	assert.Equal(t, "endpoint.url", p.Errors[0].Location)

	c.json(http.MethodPut, "/api/v1/skill/settings", deployableSettings(), http.StatusOK, nil)

	p = c.do(http.MethodPost, "/api/v1/skill/deploy", nil).problem(t)
	assert.Equal(t, http.StatusConflict, p.Status)
	assert.Contains(t, p.Detail, "client ID and secret first")

	o := h.smapi.Options()
	c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": o.ClientID, "client_secret": o.ClientSecret}, http.StatusNoContent, nil)

	p = c.do(http.MethodPost, "/api/v1/skill/deploy", nil).problem(t)
	assert.Equal(t, http.StatusConflict, p.Status)
	assert.Contains(t, p.Detail, "connect Encore to Amazon first")

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/v1/skill/deploy/d-unknown", nil).problem(t).Status)
	assert.Zero(t, h.smapi.SkillCount(), "nothing reached Amazon")
}

func TestDeployRefusedWhenTitlesCantBeRead(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.SlotValues = func(context.Context) (map[string][]skillmodel.SlotValue, error) {
			return nil, errors.New("catalog locked")
		}
	}})
	c := h.admin()
	connectAmazon(t, h, c)
	c.json(http.MethodPut, "/api/v1/skill/settings", deployableSettings(), http.StatusOK, nil)

	assert.Equal(t, http.StatusInternalServerError, c.do(http.MethodPost, "/api/v1/skill/deploy", nil).problem(t).Status)
	assert.Contains(t, h.logs.String(), "collecting library titles for the skill model: catalog locked")
	assert.Equal(t, http.StatusInternalServerError, c.do(http.MethodGet, "/api/v1/skill/package", nil).problem(t).Status)

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.True(t, info.NeedsDeploy, "a package that can't be built counts as a pending settings change")
	assert.Equal(t, "settings", info.UpdateReason)
	assert.Nil(t, info.ModelSize.V, "and has no size")
}

func TestDeployNeedingReauthorisation(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	connectAmazon(t, h, c)
	c.json(http.MethodPut, "/api/v1/skill/settings", deployableSettings(), http.StatusOK, nil)

	// The access token has run out and Amazon no longer accepts the refresh token.
	tokens := h.api.skill.tokens()
	tok, err := tokens.Load(t.Context())
	require.NoError(t, err)

	tok.Expiry = time.Now().Add(-time.Minute)
	require.NoError(t, tokens.Save(t.Context(), tok))
	h.smapi.RevokeRefreshToken()

	var acc Accepted

	c.json(http.MethodPost, "/api/v1/skill/deploy", nil, http.StatusAccepted, &acc)
	d := waitForDeploy(t, c, acc.ID)
	assert.Equal(t, "failed", d.State)
	assert.Contains(t, d.Error, "authorise Encore with Amazon again")

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.True(t, info.Amazon.NeedsReauthorise)
	assert.False(t, info.Amazon.Connected, "a revoked authorisation isn't a connection")
	assert.Equal(t, "failed", info.State)

	att, err := h.api.attention(t.Context())
	require.NoError(t, err)
	assert.Contains(t, kinds(att), "amazon_reauthorise")
	assert.NotContains(t, kinds(att), "amazon_not_connected", "reconnecting is the one thing to do")

	// The revoked token can't be pasted back in; disconnecting clears the flag.
	c.json(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"refresh_token": h.smapi.Options().RefreshToken}, http.StatusUnprocessableEntity, nil)
	c.json(http.MethodDelete, "/api/v1/skill/amazon", nil, http.StatusNoContent, nil)
	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.False(t, info.Amazon.NeedsReauthorise, "disconnecting forgets the reauthorisation flag")
}

func TestDeployHistory(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	connectAmazon(t, h, c)
	c.json(http.MethodPut, "/api/v1/skill/settings", deployableSettings(), http.StatusOK, nil)

	s := h.api.skill

	// As if keepDeploys deploys had already run.
	s.mu.Lock()
	for range keepDeploys {
		id := jobID("old")
		s.deploys[id] = &deployRecord{ID: id, State: "succeeded"}
		s.order = append(s.order, id)
	}

	oldest, second := s.order[0], s.order[1]
	s.mu.Unlock()

	var acc Accepted

	c.json(http.MethodPost, "/api/v1/skill/deploy", map[string]any{"force": true}, http.StatusAccepted, &acc)
	d := waitForDeploy(t, c, acc.ID)
	require.Equal(t, "succeeded", d.State, d.Error)

	s.mu.Lock()
	assert.Len(t, s.order, keepDeploys)
	assert.Len(t, s.deploys, keepDeploys)
	s.mu.Unlock()

	assert.Nil(t, s.deploy(t.Context(), oldest), "the oldest is forgotten")
	assert.NotNil(t, s.deploy(t.Context(), second))

	// After a restart only the last deploy is known, from settings.
	restarted := newSkillService(h.api)

	last := restarted.deploy(t.Context(), acc.ID)
	require.NotNil(t, last)
	assert.Equal(t, "succeeded", last.State)
	assert.True(t, slices.ContainsFunc(last.Events, func(e DeployEvent) bool { return e.Step == "enable" }), "with its events")
	assert.Nil(t, restarted.deploy(t.Context(), second))
	assert.Equal(t, h.api.SkillID(), restarted.storedSkillID(), "and the skill ID")
	assert.NotEmpty(t, restarted.storedSkillID())
}
