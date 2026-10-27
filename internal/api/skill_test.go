package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

func connectAmazon(t *testing.T, h *harness, c *client) {
	t.Helper()

	o := h.smapi.Options()
	c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": o.ClientID, "client_secret": o.ClientSecret}, http.StatusNoContent, nil)

	var auth AuthorizeInfo

	c.json(http.MethodGet, "/api/v1/skill/amazon/authorize", nil, http.StatusOK, &auth)
	assert.Equal(t, []string{h.server.URL + "/api/v1/oauth/lwa/callback", "http://127.0.0.1:9090/cb"}, auth.ReturnURLs)

	u, err := url.Parse(auth.URL)
	require.NoError(t, err)

	q := u.Query()
	assert.Equal(t, o.ClientID, q.Get("client_id"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "http://127.0.0.1:9090/cb", q.Get("redirect_uri"), "plain HTTP uses the paste flow")
	assert.NotContains(t, auth.URL, o.ClientSecret)

	// The public callback refuses a state it did not issue.
	resp := h.client().do(http.MethodGet, "/api/v1/oauth/lwa/callback?code="+smapitest.DefaultAuthCode+"&state=forged", nil)
	require.Equal(t, http.StatusFound, resp.status)
	assert.Contains(t, resp.header.Get("Location"), "/alexa-skill?amazon_error=")

	resp = h.client().do(http.MethodGet, "/api/v1/oauth/lwa/callback?code="+smapitest.DefaultAuthCode+"&state="+url.QueryEscape(q.Get("state")), nil)
	require.Equal(t, http.StatusFound, resp.status)
	assert.Equal(t, "/alexa-skill?amazon=connected", resp.header.Get("Location"))
}

func TestAmazonConnection(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var before AuthorizeInfo

	c.json(http.MethodGet, "/api/v1/skill/amazon/authorize", nil, http.StatusOK, &before)
	assert.Empty(t, before.URL, "no consent URL before the credentials are saved")
	assert.Len(t, before.ReturnURLs, 2, "but the return URLs to register are known")

	connectAmazon(t, h, c)

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.True(t, info.Amazon.Connected)
	assert.True(t, info.Amazon.HasClientSecret)
	assert.Equal(t, smapitest.DefaultClientID, info.Amazon.ProfileClientID)
	assert.NotContains(t, string(c.do(http.MethodGet, "/api/v1/skill", nil).body), smapitest.DefaultRefreshToken)

	c.json(http.MethodDelete, "/api/v1/skill/amazon", nil, http.StatusNoContent, nil)
	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.False(t, info.Amazon.Connected)

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"refresh_token": "Atzr|bogus"}).status)
	c.json(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"refresh_token": smapitest.DefaultRefreshToken}, http.StatusNoContent, nil)
	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.True(t, info.Amazon.Connected)

	var auth AuthorizeInfo

	c.json(http.MethodGet, "/api/v1/skill/amazon/authorize", nil, http.StatusOK, &auth)
	u, _ := url.Parse(auth.URL)
	c.json(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"redirect_url": "http://127.0.0.1:9090/cb?code=" + smapitest.DefaultAuthCode + "&state=" + u.Query().Get("state")}, http.StatusNoContent, nil)
}

func TestDeploy(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	connectAmazon(t, h, c)

	settings := map[string]any{
		"invocation":      "encore",
		"skill_name":      "",
		"locales_enabled": []string{"en-AU", "en-GB"},
		"endpoint":        map[string]any{"url": "https://encore.example.net/api/v1/alexa"},
		"privacy":         map[string]any{"uses_personal_info": false, "is_child_directed": nil, "is_export_compliant": true, "contains_ads": false, "allows_purchases": false},
	}

	var info SkillInfo

	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)
	assert.Equal(t, []string{"en-AU", "en-GB"}, info.LocalesEnabled)
	assert.Nil(t, info.Privacy.IsChildDirected)
	assert.Equal(t, "encore", info.Invocation)
	assert.Equal(t, "encore", info.InvocationName)

	p := c.do(http.MethodPost, "/api/v1/skill/deploy", map[string]any{}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status, "refused until every privacy question is answered")
	assert.Contains(t, p.Errors[0].Message, "is_child_directed")
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, "/api/v1/skill/package", nil).status)

	settings["privacy"].(map[string]any)["is_child_directed"] = false
	settings["invocation"] = "my kodi"
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPut, "/api/v1/skill/settings", settings).status, "only encore and kodi can be chosen")

	// French uses "kodi" even for Encore, since "encore" means "again" there.
	settings["locales_enabled"] = []string{"fr-FR"}
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusUnprocessableEntity, nil)
	settings["invocation"] = "encore"
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)
	assert.Equal(t, "kodi", info.InvocationName)

	settings["locales_enabled"] = []string{"en-AU", "en-GB"}
	settings["invocation"] = "kodi"
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)
	assert.Equal(t, "kodi", info.Invocation)
	assert.Equal(t, "kodi", info.InvocationName)
	assert.True(t, info.NeedsDeploy)
	assert.Equal(t, "not_deployed", info.State)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	events, resp := c.stream(ctx, "")
	defer resp.Body.Close()

	var acc Accepted

	c.json(http.MethodPost, "/api/v1/skill/deploy", map[string]any{}, http.StatusAccepted, &acc)

	ev := next(t, events, "deploy")

	var msg deployEventMessage

	require.NoError(t, json.Unmarshal([]byte(ev.data), &msg))
	assert.Equal(t, acc.ID, msg.DeployID)
	assert.Equal(t, "package", msg.Step)
	assert.Equal(t, "started", msg.State)

	var d Deploy

	require.Eventually(t, func() bool {
		c.json(http.MethodGet, "/api/v1/skill/deploy/"+acc.ID, nil, http.StatusOK, &d)

		return d.State != "in_progress"
	}, 20*time.Second, 50*time.Millisecond)

	require.Equal(t, "succeeded", d.State, d.Error)

	steps := map[string]string{}
	for _, e := range d.Events {
		steps[e.Step] = e.State
	}

	assert.Equal(t, "succeeded", steps["enable"])
	assert.NotContains(t, steps, "certificate", "there is no certificate to upload")

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.Equal(t, "live", info.State)
	assert.False(t, info.NeedsDeploy)
	assert.NotEmpty(t, info.SkillID)
	assert.Equal(t, smapitest.DefaultVendorID, info.Amazon.VendorID)
	assert.Equal(t, info.SkillID, h.api.SkillID(), "the Alexa endpoint now enforces the deployed skill")

	require.NotNil(t, info.LastDeploy.V)
	require.Len(t, info.LastDeploy.V.Locales, 2)
	assert.Equal(t, "succeeded", info.LastDeploy.V.Locales[0].Status)
	assert.Positive(t, info.LastDeploy.V.Locales[0].SizeBytes)

	sk, ok := h.smapi.Skill(info.SkillID)
	require.True(t, ok)
	assert.True(t, sk.Enabled)

	// The state is persisted, so a second deploy updates the same skill.
	var state deploy.State

	require.NoError(t, h.app.DB().Settings().Get(t.Context(), deployStateKey, &state))
	assert.Equal(t, info.SkillID, state.SkillID)

	c.json(http.MethodPost, "/api/v1/skill/deploy", map[string]any{"force": true}, http.StatusAccepted, &acc)
	require.Eventually(t, func() bool {
		c.json(http.MethodGet, "/api/v1/skill/deploy/"+acc.ID, nil, http.StatusOK, &d)

		return d.State != "in_progress"
	}, 20*time.Second, 50*time.Millisecond)
	assert.Equal(t, 1, h.smapi.SkillCount())

	pkg := c.do(http.MethodGet, "/api/v1/skill/package", nil)
	require.Equal(t, http.StatusOK, pkg.status)
	assert.Equal(t, "application/zip", pkg.header.Get("Content-Type"))

	zr, err := zip.NewReader(bytes.NewReader(pkg.body), int64(len(pkg.body)))
	require.NoError(t, err)

	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}

	assert.True(t, names["skill-package/skill.json"], "%v", names)
}

func TestDeployFailurePersistsState(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	connectAmazon(t, h, c)

	h.smapi.FailLocale("en-AU", "slot value too long")

	c.json(http.MethodPut, "/api/v1/skill/settings", map[string]any{
		"invocation": "encore", "skill_name": "", "locales_enabled": []string{"en-AU"},
		"endpoint": map[string]any{"url": "https://encore.example.net/api/v1/alexa"},
		"privacy":  map[string]any{"uses_personal_info": false, "is_child_directed": false, "is_export_compliant": true, "contains_ads": false, "allows_purchases": false},
	}, http.StatusOK, nil)

	var acc Accepted

	c.json(http.MethodPost, "/api/v1/skill/deploy", nil, http.StatusAccepted, &acc)
	assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, "/api/v1/skill/deploy", nil).status, "one deploy at a time")

	var d Deploy

	require.Eventually(t, func() bool {
		c.json(http.MethodGet, fmt.Sprintf("/api/v1/skill/deploy/%s", acc.ID), nil, http.StatusOK, &d)

		return d.State != "in_progress"
	}, 20*time.Second, 50*time.Millisecond)

	assert.Equal(t, "failed", d.State)
	assert.Contains(t, d.Error, "slot value too long")

	var state deploy.State

	require.NoError(t, h.app.DB().Settings().Get(t.Context(), deployStateKey, &state))
	assert.NotEmpty(t, state.SkillID, "the skill created before the failure is remembered")

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.Equal(t, "failed", info.State)
	assert.Equal(t, "failed", info.LastDeploy.V.Locales[0].Status)
}

func TestAmazonCallbackReturnsToSetup(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	o := h.smapi.Options()
	c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": o.ClientID, "client_secret": o.ClientSecret}, http.StatusNoContent, nil)

	state := func() string {
		var auth AuthorizeInfo

		c.json(http.MethodGet, "/api/v1/skill/amazon/authorize?then=setup", nil, http.StatusOK, &auth)

		u, err := url.Parse(auth.URL)
		require.NoError(t, err)

		return url.QueryEscape(u.Query().Get("state"))
	}

	// Amazon reporting an error still goes back to the wizard, with the reason.
	resp := h.client().do(http.MethodGet, "/api/v1/oauth/lwa/callback?error=access_denied&state="+state(), nil)
	require.Equal(t, http.StatusFound, resp.status)
	assert.True(t, strings.HasPrefix(resp.header.Get("Location"), "/setup?amazon_error=access_denied"), resp.header.Get("Location"))

	resp = h.client().do(http.MethodGet, "/api/v1/oauth/lwa/callback?code="+smapitest.DefaultAuthCode+"&state="+state(), nil)
	require.Equal(t, http.StatusFound, resp.status)
	assert.Equal(t, "/setup?amazon=connected", resp.header.Get("Location"))

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, "/api/v1/skill/amazon/authorize?then=https://evil.example", nil).status, "only encore's own pages")
}

func TestSkillContent(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.Equal(t, []string{"movies", "tv", "music"}, info.Content, "everything by default")

	settings := map[string]any{
		"invocation":      "encore",
		"skill_name":      "",
		"locales_enabled": []string{"en-AU"},
		"content":         []string{"tv", "movies"},
		"endpoint":        map[string]any{"url": "https://encore.example.net/api/v1/alexa"},
		"privacy":         map[string]any{"uses_personal_info": false, "is_child_directed": false, "is_export_compliant": true, "contains_ads": false, "allows_purchases": false},
	}

	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)
	assert.Equal(t, []string{"movies", "tv"}, info.Content, "kept in the order they are offered")

	pkg := c.do(http.MethodGet, "/api/v1/skill/package", nil)
	require.Equal(t, http.StatusOK, pkg.status)

	zr, err := zip.NewReader(bytes.NewReader(pkg.body), int64(len(pkg.body)))
	require.NoError(t, err)

	f, err := zr.Open("skill-package/interactionModels/custom/en-AU.json")
	require.NoError(t, err)
	model, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Contains(t, string(model), `"WatchMovie"`)
	assert.NotContains(t, string(model), `"ListenToArtist"`, "music intents are left out")

	// Leaving content out keeps the choice.
	delete(settings, "content")
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)
	assert.Equal(t, []string{"movies", "tv"}, info.Content)

	settings["content"] = []string{}
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPut, "/api/v1/skill/settings", settings).status, "at least one group")

	settings["content"] = []string{"podcasts"}
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPut, "/api/v1/skill/settings", settings).status)
}

func TestUpdateReason(t *testing.T) {
	var (
		mu     sync.Mutex
		titles = []skillmodel.SlotValue{{ID: "movie/1", Value: "Dune"}}
	)

	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.SlotValues = func(context.Context) (map[string][]skillmodel.SlotValue, error) {
			mu.Lock()
			defer mu.Unlock()

			return map[string][]skillmodel.SlotValue{"AMAZON.Movie": slices.Clone(titles)}, nil
		}
	}})
	c := h.admin()
	connectAmazon(t, h, c)

	settings := map[string]any{
		"invocation":      "encore",
		"skill_name":      "",
		"locales_enabled": []string{"en-AU"},
		"endpoint":        map[string]any{"url": "https://encore.example.net/api/v1/alexa"},
		"privacy":         map[string]any{"uses_personal_info": false, "is_child_directed": false, "is_export_compliant": true, "contains_ads": false, "allows_purchases": false},
	}
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, nil)

	var started Accepted

	c.json(http.MethodPost, "/api/v1/skill/deploy", map[string]any{}, http.StatusAccepted, &started)
	require.Eventually(t, func() bool {
		var d Deploy

		c.json(http.MethodGet, "/api/v1/skill/deploy/"+started.ID, nil, http.StatusOK, &d)

		return d.State == "succeeded"
	}, 20*time.Second, 50*time.Millisecond)

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.False(t, info.NeedsDeploy)
	assert.Equal(t, "none", info.UpdateReason)

	// A new title in the library: the model would change, the settings haven't.
	mu.Lock()
	titles = append(titles, skillmodel.SlotValue{ID: "movie/2", Value: "Arrival"})
	mu.Unlock()

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.True(t, info.NeedsDeploy)
	assert.Equal(t, "library", info.UpdateReason)

	var ov Overview

	c.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, &ov)
	assert.Equal(t, "library", ov.Skill.UpdateReason)
	require.NotEmpty(t, ov.Attention)
	assert.True(t, slices.ContainsFunc(ov.Attention, func(a Attention) bool { return a.Title == "New library titles for the skill" }), "%+v", ov.Attention)

	// A settings change wins over the library one.
	settings["skill_name"] = "Kodi Remote"
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)
	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.Equal(t, "settings", info.UpdateReason)
}

func TestDeployMarksSimulatorWindow(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	connectAmazon(t, h, c)

	assert.False(t, h.api.Simulating(), "no smoke test yet")

	settings := map[string]any{
		"invocation":      "encore",
		"skill_name":      "",
		"locales_enabled": []string{"en-AU"},
		"endpoint":        map[string]any{"url": "https://encore.example.net/api/v1/alexa"},
		"privacy":         map[string]any{"uses_personal_info": false, "is_child_directed": false, "is_export_compliant": true, "contains_ads": false, "allows_purchases": false},
	}
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, nil)

	var started Accepted

	c.json(http.MethodPost, "/api/v1/skill/deploy", map[string]any{}, http.StatusAccepted, &started)
	require.Eventually(t, func() bool {
		var d Deploy

		c.json(http.MethodGet, "/api/v1/skill/deploy/"+started.ID, nil, http.StatusOK, &d)

		return d.State == "succeeded"
	}, 20*time.Second, 50*time.Millisecond)

	assert.True(t, h.api.Simulating(), "the session-ended request after the smoke test still counts as the simulator's")

	// Every log line names one component; the deploy's used to say component=api component=deploy.
	logs := h.logs.String()
	assert.Contains(t, logs, `msg="skill deploy step" component=deploy deploy=`)

	for line := range strings.SplitSeq(logs, "\n") {
		assert.LessOrEqual(t, strings.Count(line, "component="), 1, line)
	}

	h.skew.Add(int64(simulateGrace + time.Second))
	assert.False(t, h.api.Simulating(), "then requests are an Echo's again")
}

func TestContentLimitsLibrarySync(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()

	songs := func() int {
		var libs []Library

		c.json(http.MethodGet, "/api/v1/libraries", nil, http.StatusOK, &libs)
		if len(libs) == 0 {
			return -1
		}

		return libs[0].Counts.Songs
	}

	require.Eventually(t, func() bool { return songs() > 0 }, 10*time.Second, 50*time.Millisecond, "music is synced by default")

	settings := map[string]any{
		"invocation":      "encore",
		"skill_name":      "",
		"locales_enabled": []string{"en-AU"},
		"content":         []string{"movies", "tv"},
		"endpoint":        map[string]any{"url": "https://encore.example.net/api/v1/alexa"},
		"privacy":         map[string]any{"uses_personal_info": false, "is_child_directed": false, "is_export_compliant": true, "contains_ads": false, "allows_purchases": false},
	}
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, nil)
	assert.Equal(t, 0, songs(), "turning music off drops its titles straight away")

	settings["content"] = []string{"movies", "tv", "music"}
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, nil)
	require.Eventually(t, func() bool { return songs() > 0 }, 10*time.Second, 50*time.Millisecond, "and turning it back on fetches them again")
}

func TestSavedContentAppliesFromStartup(t *testing.T) {
	// As after a restart: music was turned off in the web interface earlier.
	h := newHarness(t, harnessOpts{mutate: func(o *Options) {
		require.NoError(t, o.App.DB().Settings().Set(context.Background(), overridesSetting, map[string]any{"alexa.content": []string{"movies", "tv"}}))
	}})
	c := h.admin()

	var libs []Library

	require.Eventually(t, func() bool {
		c.json(http.MethodGet, "/api/v1/libraries", nil, http.StatusOK, &libs)

		return len(libs) > 0 && libs[0].Counts.Movies > 0
	}, 10*time.Second, 50*time.Millisecond)

	assert.Zero(t, libs[0].Counts.Songs, "the first sync leaves music out")

	calls := 0
	for _, call := range h.kodi.Calls() {
		if strings.HasPrefix(call.Method, "AudioLibrary.") {
			calls++
		}
	}

	assert.Zero(t, calls, "Kodi isn't asked for music at all")
}

func TestSaveContentAlone(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()

	songs := func() int {
		var libs []Library

		c.json(http.MethodGet, "/api/v1/libraries", nil, http.StatusOK, &libs)
		if len(libs) == 0 {
			return -1
		}

		return libs[0].Counts.Songs
	}

	require.Eventually(t, func() bool { return songs() > 0 }, 10*time.Second, 50*time.Millisecond)

	var info SkillInfo

	c.json(http.MethodPut, "/api/v1/skill/content", map[string]any{"content": []string{"tv", "movies"}}, http.StatusOK, &info)
	assert.Equal(t, []string{"movies", "tv"}, info.Content, "in the order offered")
	assert.Zero(t, songs(), "music leaves the library at once, with nothing else of the skill's settings needed")

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPut, "/api/v1/skill/content", map[string]any{"content": []string{}}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPut, "/api/v1/skill/content", map[string]any{"content": []string{"podcasts"}}).status)
}
