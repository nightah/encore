package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/skillpkg"
	"github.com/nightah/encore/internal/smapi/smapitest"
	"github.com/nightah/encore/internal/store"
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

	d := waitForDeploy(t, c, acc.ID)
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
	d = waitForDeploy(t, c, acc.ID)
	require.Equal(t, "succeeded", d.State, d.Error)
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

	d := waitForDeploy(t, c, acc.ID)

	assert.Equal(t, "failed", d.State)
	assert.Contains(t, d.Error, "slot value too long")

	var state deploy.State

	require.NoError(t, h.app.DB().Settings().Get(t.Context(), deployStateKey, &state))
	assert.NotEmpty(t, state.SkillID, "the skill created before the failure is remembered")
	assert.Empty(t, state.SettingsHash, "a package that wasn't fully deployed records no settings hash, so the next deploy isn't put down to library titles")

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
	d := waitForDeploy(t, c, started.ID)
	require.Equal(t, "succeeded", d.State, d.Error)

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
	d := waitForDeploy(t, c, started.ID)
	require.Equal(t, "succeeded", d.State, d.Error)

	assert.True(t, h.api.Simulating(), "the session-ended request after the smoke test still counts as the simulator's")

	// Every log line names one component, never two such as component=api component=deploy.
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

func TestPrivacyAnswers(t *testing.T) {
	t.Parallel()

	var none Privacy

	assert.False(t, none.answered())
	assert.Equal(t, []string{"allows_purchases", "contains_ads", "is_child_directed", "is_export_compliant", "uses_personal_info"}, none.unanswered(), "sorted, so messages are stable")

	all := answeredPrivacy()
	assert.True(t, all.answered())
	assert.Empty(t, all.unanswered())

	// false is an answer; only null isn't.
	partial := Privacy{UsesPersonalInfo: new(false), IsChildDirected: new(false), IsExportCompliant: new(false), ContainsAds: new(false)}
	assert.False(t, partial.answered())
	assert.Equal(t, []string{"allows_purchases"}, partial.unanswered())

	pkg := all.toPkg()
	assert.Equal(t, all.IsExportCompliant, pkg.IsExportCompliant)
	assert.Equal(t, all.AllowsPurchases, pkg.AllowsPurchases)
	assert.Equal(t, all.UsesPersonalInfo, pkg.UsesPersonalInfo)
	assert.Equal(t, all.IsChildDirected, pkg.IsChildDirected)
	assert.Equal(t, all.ContainsAds, pkg.ContainsAds)
}

func TestInvocationChoice(t *testing.T) {
	t.Parallel()

	for stored, want := range map[string]string{"kodi": "kodi", "": "encore", "encore": "encore", "Kodi": "encore", "my kodi": "encore"} {
		assert.Equal(t, want, invocationChoice(stored), stored)
	}
}

func TestOrderContent(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"movies", "tv", "music"}, orderContent([]string{"music", "tv", "movies", "tv"}), "in the order offered, without duplicates")
	assert.Equal(t, []string{"tv"}, orderContent([]string{"podcasts", "tv", "TV"}), "unknown groups are dropped")
	assert.Equal(t, []string{}, orderContent(nil), "an empty list, not nil")
}

func TestSmallHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "en-US", firstOr(nil, "en-US"))
	assert.Equal(t, "en-AU", firstOr([]string{"en-AU", "en-GB"}, "en-US"))

	in := []string{"en-GB", "de-DE", "en-AU"}
	assert.Equal(t, []string{"de-DE", "en-AU", "en-GB"}, sortedCopy(in))
	assert.Equal(t, []string{"en-GB", "de-DE", "en-AU"}, in, "the input is left as it was")

	assert.Empty(t, (&skillService{}).storedSkillID(), "no skill ID before one is loaded")
}

func TestLWAProblem(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	status := func(err error) int {
		var se huma.StatusError

		require.ErrorAs(t, h.api.lwaProblem(t.Context(), err), &se)

		return se.GetStatus()
	}

	require.NoError(t, h.api.lwaProblem(t.Context(), nil))
	assert.Equal(t, http.StatusUnprocessableEntity, status(fmt.Errorf("exchanging: %w", lwa.ErrInvalidClient)))
	assert.Equal(t, http.StatusUnprocessableEntity, status(fmt.Errorf("refreshing: %w", lwa.ErrReauthorise)))
	assert.Equal(t, http.StatusConflict, status(errNoLWACredentials), "a problem already is passed on")
	assert.Equal(t, http.StatusUnprocessableEntity, status(errors.New("lwa: pasted URL is empty")))
	assert.Equal(t, http.StatusUnprocessableEntity, status(errors.New("this authorisation was not started here or has expired; start again")))
	assert.Equal(t, http.StatusInternalServerError, status(errors.New("disk full")))
	assert.Equal(t, http.StatusNotFound, status(store.ErrNotFound))
}

func TestSkillSettingsValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	for name, tc := range map[string]struct {
		change   func(map[string]any)
		location string
	}{
		"unknown locale": {func(s map[string]any) { s["locales_enabled"] = []string{"en-AU", "xx-XX"} }, "body.locales_enabled"},
		"no locale":      {func(s map[string]any) { s["locales_enabled"] = []string{} }, "body.locales_enabled"},
		"http endpoint": {func(s map[string]any) {
			s["endpoint"] = map[string]any{"url": "http://encore.example.net/api/v1/alexa"}
		}, "body.endpoint.url"},
		"endpoint no host": {func(s map[string]any) { s["endpoint"] = map[string]any{"url": "https:///api/v1/alexa"} }, "body.endpoint.url"},
		"bad invocation":   {func(s map[string]any) { s["invocation"] = "alexa" }, "body.invocation"},
	} {
		t.Run(name, func(t *testing.T) {
			settings := deployableSettings()
			tc.change(settings)

			p := c.do(http.MethodPut, "/api/v1/skill/settings", settings).problem(t)
			require.Equal(t, http.StatusUnprocessableEntity, p.Status)
			require.Len(t, p.Errors, 1, "%+v", p)
			assert.Equal(t, tc.location, p.Errors[0].Location)
		})
	}

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.Equal(t, info.LocalesAvailable, info.LocalesEnabled, "refused settings change nothing")

	settings := deployableSettings()
	settings["locales_enabled"] = []string{"en-GB", "en-AU", "en-GB"}
	settings["skill_name"] = "  Kodi Remote  "
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)
	assert.Equal(t, []string{"en-GB", "en-AU"}, info.LocalesEnabled, "duplicates are dropped, the order kept")
	assert.Equal(t, "Kodi Remote", info.SkillName)

	var stored skillSettings

	require.NoError(t, h.app.DB().Settings().Get(t.Context(), skillSettingsKey, &stored))
	assert.Empty(t, stored.Invocation, "Encore's own name is stored as no override, so each locale keeps its own")

	// Once locales were chosen, choosing every one is a choice too.
	settings["locales_enabled"] = info.LocalesAvailable
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)

	eff, err := h.api.prefs.Effective(t.Context())
	require.NoError(t, err)
	assert.Equal(t, info.LocalesAvailable, eff.Alexa.Locales)
}

func TestSkillLocalesFollowShippedOnes(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)

	settings := deployableSettings()
	settings["locales_enabled"] = slices.Concat([]string{info.LocalesAvailable[len(info.LocalesAvailable)-1]}, info.LocalesAvailable)
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)

	eff, err := h.api.prefs.Effective(t.Context())
	require.NoError(t, err)
	assert.Empty(t, eff.Alexa.Locales, "every locale, in any order, is no change from the default")
	assert.Equal(t, info.LocalesAvailable, info.LocalesEnabled)
	assert.Equal(t, h.api.opts.Sources.Locales[info.LocalesEnabled[0]].SkillName, info.SkillName, "the first locale's own skill name")
	assert.NotEmpty(t, info.SkillName)
}

func TestAmazonCredentials(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	o := h.smapi.Options()

	p := c.do(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": o.ClientID}).problem(t)
	require.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Equal(t, "body.client_secret", p.Errors[0].Location, "the secret is required the first time")

	connectAmazon(t, h, c)

	var info SkillInfo

	c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": " " + o.ClientID + " "}, http.StatusNoContent, nil)
	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.True(t, info.Amazon.Connected, "saving the same client ID keeps the authorisation")
	assert.True(t, info.Amazon.HasClientSecret, "and the stored secret")
	assert.Equal(t, o.ClientID, info.Amazon.ProfileClientID, "trimmed")

	c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": "amzn1.application-oa2-client.other"}, http.StatusNoContent, nil)
	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	assert.False(t, info.Amazon.Connected, "a token belongs to the client that obtained it")

	p = c.do(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"refresh_token": smapitest.DefaultRefreshToken}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Contains(t, p.Detail, "rejected the client ID or secret")
}

func TestAmazonAuthorizeChoosesTheReturnURL(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	o := h.smapi.Options()
	c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": o.ClientID, "client_secret": o.ClientSecret}, http.StatusNoContent, nil)

	callback := h.server.URL + "/api/v1/oauth/lwa/callback"

	var auth AuthorizeInfo

	c.json(http.MethodGet, "/api/v1/skill/amazon/authorize?return_url="+url.QueryEscape(callback), nil, http.StatusOK, &auth)

	u, err := url.Parse(auth.URL)
	require.NoError(t, err)
	assert.Equal(t, callback, u.Query().Get("redirect_uri"), "the flow can be chosen explicitly")

	p := c.do(http.MethodGet, "/api/v1/skill/amazon/authorize?return_url="+url.QueryEscape("https://evil.example/cb"), nil).problem(t)
	require.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Equal(t, "query.return_url", p.Errors[0].Location)
}

func TestAmazonAuthorisationExpires(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	o := h.smapi.Options()
	c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": o.ClientID, "client_secret": o.ClientSecret}, http.StatusNoContent, nil)

	state := func() string {
		var auth AuthorizeInfo

		c.json(http.MethodGet, "/api/v1/skill/amazon/authorize", nil, http.StatusOK, &auth)

		u, err := url.Parse(auth.URL)
		require.NoError(t, err)

		return u.Query().Get("state")
	}

	stale := state()

	h.skew.Add(int64(lwaPendingTTL + time.Second))

	fresh := state()

	h.api.skill.mu.Lock()
	_, kept := h.api.skill.pending[stale]
	pending := len(h.api.skill.pending)
	h.api.skill.mu.Unlock()

	assert.False(t, kept, "expired authorisations are cleared as new ones start")
	assert.Equal(t, 1, pending)

	h.skew.Add(int64(lwaPendingTTL + time.Second))

	p := c.do(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"redirect_url": localReturnURL + "?code=" + smapitest.DefaultAuthCode + "&state=" + url.QueryEscape(fresh)}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Contains(t, p.Detail, "expired")
	assert.Zero(t, h.smapi.Exchanges(), "an expired code is never redeemed")

	h.skew.Store(0)

	p = c.do(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"redirect_url": localReturnURL + "?code=" + smapitest.DefaultAuthCode + "&state=" + url.QueryEscape(fresh)}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status, "a state is used once, even if it failed")
}

func TestAmazonPasteValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	p := c.do(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{}).problem(t)
	require.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Equal(t, "body", p.Errors[0].Location)

	p = c.do(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"redirect_url": "http://127.0.0.1:9090/cb?error=access_denied"}).problem(t)
	require.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Equal(t, "body.redirect_url", p.Errors[0].Location)

	assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"refresh_token": smapitest.DefaultRefreshToken}).status, "credentials come first")
}

func TestSkillModelSize(t *testing.T) {
	h := newHarness(t, harnessOpts{mutate: func(o *Options) {
		o.SlotValues = func(context.Context) (map[string][]skillmodel.SlotValue, error) {
			return map[string][]skillmodel.SlotValue{"AMAZON.Movie": {{ID: "movie/6", Value: "Hot Fuzz"}, {ID: "movie/1", Value: "Dune"}}}, nil
		}
	}})
	c := h.admin()
	h.libraryID(t)

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	require.NotNil(t, info.ModelSize.V, "sized before the privacy questions are answered")

	ms := info.ModelSize.V
	assert.Equal(t, skillpkg.MaxModelBytes, ms.LimitBytes)
	assert.Positive(t, ms.Bytes)
	assert.Contains(t, info.LocalesEnabled, ms.Locale, "the largest model's locale")
	assert.Equal(t, 2, ms.TitlesInModel)
	assert.Equal(t, 7+4-2, ms.TitlesServerOnly, "movies and shows Encore finds without the model's help")

	in := h.api.skill.inSkillModel(t.Context())
	assert.True(t, in(0, media.Item{Kind: media.KindMovie, ID: 6}))
	assert.False(t, in(0, media.Item{Kind: media.KindMovie, ID: 7}))
	assert.False(t, in(0, media.Item{Kind: media.KindTVShow, ID: 6}), "kinds don't mix")
}

func TestInSkillModelWithoutTitles(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.SlotValues = func(context.Context) (map[string][]skillmodel.SlotValue, error) {
			return nil, errors.New("catalog locked")
		}
	}})

	assert.False(t, h.api.skill.inSkillModel(t.Context())(0, media.Item{Kind: media.KindMovie, ID: 1}), "nothing is in a model that can't be built")

	s := &skillService{a: &API{}}
	assert.False(t, s.inSkillModel(t.Context())(0, media.Item{Kind: media.KindMovie, ID: 1}), "nor without a title source")
}

func TestSkillStateOf(t *testing.T) {
	t.Parallel()

	s := &skillService{}

	for name, tc := range map[string]struct {
		state deploy.State
		last  *deployRecord
		want  string
	}{
		"never":                     {deploy.State{}, nil, "not_deployed"},
		"failed":                    {deploy.State{Hash: "h"}, &deployRecord{State: "failed"}, "failed"},
		"deployed":                  {deploy.State{Hash: "h"}, &deployRecord{State: "succeeded"}, "live"},
		"deployed before a restart": {deploy.State{Hash: "h"}, nil, "live"},
		"skill without a package":   {deploy.State{SkillID: "amzn1.ask.skill.1"}, nil, "not_deployed"},
		"skill with a success":      {deploy.State{SkillID: "amzn1.ask.skill.1"}, &deployRecord{State: "succeeded"}, "live"},
	} {
		assert.Equal(t, tc.want, s.stateOf(tc.state, tc.last), name)
	}

	s.running = "d-1"
	assert.Equal(t, "deploying", s.stateOf(deploy.State{Hash: "h"}, &deployRecord{State: "failed"}), "a running deploy wins")
}

func TestEndpointURL(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	assert.Equal(t, "https://encore.example.net/api/v1/alexa", h.api.skill.endpointURL(t.Context(), skillSettings{Endpoint: SkillEndpoint{URL: "https://encore.example.net/api/v1/alexa"}}))
	assert.Empty(t, h.api.skill.endpointURL(t.Context(), skillSettings{}), "outside a request there's nothing to derive it from")
}

func TestAmazonAuthorizeOverHTTPSReturnsToEncore(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	o := h.smapi.Options()
	c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": o.ClientID, "client_secret": o.ClientSecret}, http.StatusNoContent, nil)

	// As the browser reached Encore: over HTTPS, so the consent page can come back to the callback itself.
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://encore.example.net/api/v1/skill/amazon/authorize", nil)
	ctx := context.WithValue(t.Context(), ctxKey{}, &reqInfo{r: r})

	auth, err := h.api.skill.authorize(ctx, "", "setup")
	require.NoError(t, err)

	callback := "https://encore.example.net/api/v1/oauth/lwa/callback"
	assert.Equal(t, []string{callback, localReturnURL}, auth.ReturnURLs)

	u, err := url.Parse(auth.URL)
	require.NoError(t, err)
	assert.Equal(t, callback, u.Query().Get("redirect_uri"), "HTTPS uses the callback, not the paste flow")

	h.api.skill.mu.Lock()
	p := h.api.skill.pending[u.Query().Get("state")]
	h.api.skill.mu.Unlock()

	assert.Equal(t, callback, p.redirect, "the code is redeemed with the same redirect")
	assert.Equal(t, "setup", p.page)

	auth, err = h.api.skill.authorize(ctx, localReturnURL, "alexa-skill")
	require.NoError(t, err)

	u, err = url.Parse(auth.URL)
	require.NoError(t, err)
	assert.Equal(t, localReturnURL, u.Query().Get("redirect_uri"), "the paste flow can still be chosen")
}

func TestSkillSettingsRefuseANameTheSkillCantUse(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	settings := deployableSettings()
	settings["skill_name"] = "K"

	p := c.do(http.MethodPut, "/api/v1/skill/settings", settings).problem(t)
	require.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Contains(t, p.Detail, `skill name "K" is too short`)
	assert.NotContains(t, p.Detail, "skillpkg:", "the package builder's prefix means nothing to the user")

	var stored skillSettings

	require.ErrorIs(t, h.app.DB().Settings().Get(t.Context(), skillSettingsKey, &stored), store.ErrNotFound, "nothing is saved")
}

func TestSavingUnchangedContentKeepsTheDefault(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	overridden := func() bool {
		var o map[string]any
		if err := h.app.DB().Settings().Get(t.Context(), overridesSetting, &o); err != nil {
			require.ErrorIs(t, err, store.ErrNotFound)

			return false
		}

		_, ok := o["alexa.content"]

		return ok
	}

	var info SkillInfo

	c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
	require.NotEmpty(t, info.Content)

	c.json(http.MethodPut, "/api/v1/skill/content", map[string]any{"content": info.Content}, http.StatusOK, &info)
	assert.False(t, overridden(), "choosing what is already played stores nothing, so the default still applies")

	settings := deployableSettings()
	settings["content"] = info.Content
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, &info)
	assert.False(t, overridden(), "nor when it comes with the other settings")

	c.json(http.MethodPut, "/api/v1/skill/content", map[string]any{"content": []string{"movies"}}, http.StatusOK, &info)
	assert.True(t, overridden(), "a change is stored")
	assert.Equal(t, []string{"movies"}, info.Content)
}

func TestAmazonAuthorisationNeedsTheClientID(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	// A secret without its client ID, as a save that failed halfway would leave.
	require.NoError(t, h.app.Secrets().Put(t.Context(), lwaSecretRef, []byte(h.smapi.Options().ClientSecret)))

	var auth AuthorizeInfo

	c.json(http.MethodGet, "/api/v1/skill/amazon/authorize", nil, http.StatusOK, &auth)
	assert.Empty(t, auth.URL, "no consent URL without a client ID")
	assert.Len(t, auth.ReturnURLs, 2)

	assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, "/api/v1/skill/amazon/paste", map[string]string{"refresh_token": smapitest.DefaultRefreshToken}).status)
}

func TestAmazonCallbackFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		code   string
		before func(t *testing.T, h *harness)
		want   string
	}{
		"Amazon refuses the code": {code: "not-the-code", want: "invalid_grant"},
		"the credentials were removed meanwhile": {
			code:   smapitest.DefaultAuthCode,
			before: func(t *testing.T, h *harness) { require.NoError(t, h.app.Secrets().Delete(t.Context(), lwaSecretRef)) },
			want:   errNoLWACredentials.Error(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{noKodi: true})
			c := h.admin()
			o := h.smapi.Options()
			c.json(http.MethodPut, "/api/v1/skill/amazon/credentials", map[string]string{"client_id": o.ClientID, "client_secret": o.ClientSecret}, http.StatusNoContent, nil)

			var auth AuthorizeInfo

			c.json(http.MethodGet, "/api/v1/skill/amazon/authorize?then=setup", nil, http.StatusOK, &auth)

			u, err := url.Parse(auth.URL)
			require.NoError(t, err)

			if tc.before != nil {
				tc.before(t, h)
			}

			resp := h.client().do(http.MethodGet, "/api/v1/oauth/lwa/callback?code="+tc.code+"&state="+url.QueryEscape(u.Query().Get("state")), nil)
			require.Equal(t, http.StatusFound, resp.status)

			loc, err := url.Parse(resp.header.Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, "/setup", loc.Path, "back to the page that started it")
			assert.Contains(t, loc.Query().Get("amazon_error"), tc.want)

			var info SkillInfo

			c.json(http.MethodGet, "/api/v1/skill", nil, http.StatusOK, &info)
			assert.False(t, info.Amazon.Connected)
			assert.Contains(t, h.logs.String(), "Login with Amazon failed")
		})
	}
}

func TestSkillPackageNeedsAnHTTPSEndpoint(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	// No endpoint is set, and the one derived from how the browser reached Encore is plain HTTP.
	settings := deployableSettings()
	settings["endpoint"] = map[string]any{"url": ""}
	c.json(http.MethodPut, "/api/v1/skill/settings", settings, http.StatusOK, nil)

	p := c.do(http.MethodGet, "/api/v1/skill/package", nil).problem(t)
	require.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Contains(t, p.Detail, "must be an https:// URL")
	assert.Contains(t, p.Detail, h.server.URL+"/api/v1/alexa")
	assert.NotContains(t, p.Detail, "skillpkg:")
}

func TestDeployRefusesStoredSettingsThatNoLongerBuild(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	// Saved settings are checked as they are saved; these could only come from an older release or a restored backup.
	require.NoError(t, h.app.DB().Settings().Set(t.Context(), skillSettingsKey, skillSettings{
		SkillName: "K", Endpoint: SkillEndpoint{URL: "https://encore.example.net/api/v1/alexa"}, Privacy: answeredPrivacy(),
	}))

	p := c.do(http.MethodPost, "/api/v1/skill/deploy", map[string]any{}).problem(t)
	require.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Contains(t, p.Detail, "too short")
	assert.NotContains(t, p.Detail, "skillpkg:")
	assert.Zero(t, h.smapi.Exchanges(), "nothing reaches Amazon")
}
