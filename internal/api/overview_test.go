package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/store"
)

func TestSummariseToday(t *testing.T) {
	t.Parallel()

	entry := func(ms int, outcome string, simulated bool) store.RequestLogEntry {
		return store.RequestLogEntry{Duration: time.Duration(ms) * time.Millisecond, Outcome: outcome, Simulated: simulated}
	}

	for name, tc := range map[string]struct {
		in   []store.RequestLogEntry
		want RequestsToday
	}{
		"none":            {nil, RequestsToday{}},
		"one":             {[]store.RequestLogEntry{entry(120, "played", false)}, RequestsToday{Total: 1, MedianMS: 120}},
		"odd count":       {[]store.RequestLogEntry{entry(300, "played", false), entry(100, "error", false), entry(200, "played", false)}, RequestsToday{Total: 3, Failed: 1, MedianMS: 200}},
		"even count":      {[]store.RequestLogEntry{entry(40, "played", false), entry(10, "error", false), entry(30, "error", false), entry(20, "played", false)}, RequestsToday{Total: 4, Failed: 2, MedianMS: 30}},
		"only simulator":  {[]store.RequestLogEntry{entry(500, "error", true)}, RequestsToday{}},
		"simulator mixed": {[]store.RequestLogEntry{entry(9000, "error", true), entry(80, "played", false)}, RequestsToday{Total: 1, MedianMS: 80}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			before := append([]store.RequestLogEntry(nil), tc.in...)

			assert.Equal(t, tc.want, summariseToday(tc.in))
			assert.Equal(t, before, tc.in, "the entries are left as they were")
		})
	}
}

func TestCheckEndpoint(t *testing.T) {
	t.Parallel()

	var (
		hits   atomic.Int32
		status atomic.Int32
	)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(int(status.Load()))
	}))
	t.Cleanup(srv.Close)

	var skew atomic.Int64

	start := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	a := &API{now: func() time.Time { return start.Add(time.Duration(skew.Load())) }, opts: Options{HTTPClient: srv.Client()}}
	ec := &endpointCache{}
	endpoint := srv.URL + "/api/v1/alexa"

	status.Store(http.StatusMethodNotAllowed)

	check := a.checkEndpoint(t.Context(), ec, endpoint)
	assert.True(t, check.Reachable, "Encore answers a GET with 405")
	assert.Equal(t, endpoint, check.URL)
	require.NotNil(t, check.CheckedAt)
	assert.Equal(t, start, *check.CheckedAt)
	assert.EqualValues(t, 1, hits.Load())

	status.Store(http.StatusBadGateway)
	skew.Add(int64(endpointCheckEvery - time.Second))

	assert.Equal(t, check, a.checkEndpoint(t.Context(), ec, endpoint), "a recent check is reused")
	assert.EqualValues(t, 1, hits.Load())

	skew.Add(int64(time.Second))

	check = a.checkEndpoint(t.Context(), ec, endpoint)
	assert.False(t, check.Reachable, "a proxy's 502 means Encore isn't behind it")
	assert.EqualValues(t, 2, hits.Load(), "an old check is made again")

	status.Store(http.StatusInternalServerError)

	check = a.checkEndpoint(t.Context(), ec, srv.URL+"/api/v1/alexa?broken")
	assert.False(t, check.Reachable, "a 500 is a server error, not Encore's answer")
	assert.EqualValues(t, 3, hits.Load())

	status.Store(http.StatusNotFound)

	check = a.checkEndpoint(t.Context(), ec, srv.URL+"/other")
	assert.True(t, check.Reachable, "something answered")
	assert.EqualValues(t, 4, hits.Load(), "a different URL is checked at once")

	plain := a.checkEndpoint(t.Context(), ec, "http://encore.example.net/api/v1/alexa")
	assert.Equal(t, EndpointCheck{URL: "http://encore.example.net/api/v1/alexa"}, plain, "only https endpoints are checked")
	assert.Equal(t, EndpointCheck{}, a.checkEndpoint(t.Context(), ec, ""))
	assert.EqualValues(t, 4, hits.Load())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	closed := "https://" + ln.Addr().String() + "/api/v1/alexa"
	require.NoError(t, ln.Close())

	check = a.checkEndpoint(t.Context(), ec, closed)
	assert.False(t, check.Reachable)
	assert.NotNil(t, check.CheckedAt, "a failed check is a check")

	check = a.checkEndpoint(t.Context(), ec, "https://encore.example.net/\x7f")
	assert.False(t, check.Reachable, "a URL that can't be requested isn't reachable")
}

func kinds(as []Attention) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Kind)
	}

	return out
}

func answeredPrivacy() Privacy {
	return Privacy{UsesPersonalInfo: new(false), IsChildDirected: new(false), IsExportCompliant: new(true), ContainsAds: new(false), AllowsPurchases: new(false)}
}

func TestAttentionOnAFreshInstall(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	att, err := h.api.attention(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"no_instances", "amazon_not_connected", "privacy_unanswered", "skill_not_deployed"}, kinds(att))

	for _, a := range att {
		assert.NotEmpty(t, a.Title, a.Kind)
		assert.NotEmpty(t, a.Detail, a.Kind)
		assert.NotEmpty(t, a.Link, a.Kind)
	}
}

func TestAttentionNamesWhatNeedsDoing(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	c := h.admin()
	ctx := t.Context()
	settings := h.app.DB().Settings()

	require.NoError(t, settings.Set(ctx, skillSettingsKey, skillSettings{Privacy: answeredPrivacy()}))
	require.NoError(t, settings.Set(ctx, reauthKey, true))
	require.NoError(t, settings.Set(ctx, lastDeployKey, deployRecord{ID: "d-1", State: "failed", Error: "slot value too long"}))

	locked := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "s3cret"})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	gone := "http://" + ln.Addr().String() + "/jsonrpc"
	require.NoError(t, ln.Close())

	var bedroom, garage, shed Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Bedroom", "url": locked.URL()}, http.StatusCreated, &bedroom)
	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Garage", "url": gone, "websocket_url": ""}, http.StatusCreated, &garage)
	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Shed", "url": gone, "websocket_url": "", "mac": "dc:a6:32:4b:19:0e"}, http.StatusCreated, &shed)

	for _, id := range []string{"amzn1.ask.device.A", "amzn1.ask.device.B", "amzn1.ask.device.C"} {
		_, err := h.app.DB().EchoDevices().Seen(ctx, id)
		require.NoError(t, err)
	}

	devs, err := h.app.DB().EchoDevices().List(ctx)
	require.NoError(t, err)

	devs[0].KodiInstanceID = bedroom.ID
	require.NoError(t, h.app.DB().EchoDevices().Update(ctx, &devs[0]))

	att, err := h.api.attention(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"amazon_reauthorise", "deploy_failed", "kodi_unauthorized", "instance_offline", "unlinked_device"}, kinds(att), "a sleeping instance that can be woken isn't a problem")

	byKind := map[string]Attention{}
	for _, a := range att {
		byKind[a.Kind] = a
	}

	assert.Equal(t, "slot value too long", byKind["deploy_failed"].Detail)
	assert.Equal(t, "Bedroom rejected Encore's credentials", byKind["kodi_unauthorized"].Title)
	assert.Equal(t, "Garage is offline", byKind["instance_offline"].Title)
	assert.Equal(t, "2 Echo devices are not linked", byKind["unlinked_device"].Title)

	devs[1].KodiInstanceID = bedroom.ID
	require.NoError(t, h.app.DB().EchoDevices().Update(ctx, &devs[1]))

	att, err = h.api.attention(ctx)
	require.NoError(t, err)
	assert.Contains(t, att, Attention{Kind: "unlinked_device", Title: "1 Echo device is not linked", Detail: "Unlinked devices control the default Kodi instance.", Link: "/echo-devices"})
}

func TestAttentionForAPendingUpdate(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.loungeID()

	ctx := t.Context()
	settings := h.app.DB().Settings()

	require.NoError(t, settings.Set(ctx, skillSettingsKey, skillSettings{Privacy: answeredPrivacy(), Endpoint: SkillEndpoint{URL: "https://encore.example.net/api/v1/alexa"}}))
	require.NoError(t, settings.Set(ctx, deployStateKey, deploy.State{SkillID: "amzn1.ask.skill.1", Hash: "an older package"}))
	require.NoError(t, settings.Set(ctx, lastDeployKey, deployRecord{ID: "d-1", State: "succeeded"}))

	att, err := h.api.attention(ctx)
	require.NoError(t, err)

	var pending []Attention

	for _, a := range att {
		if a.Kind == "needs_deploy" {
			pending = append(pending, a)
		}
	}

	require.Len(t, pending, 1, "%+v", att)
	assert.Equal(t, "Skill update pending", pending[0].Title)
	assert.NotContains(t, kinds(att), "skill_not_deployed")
	assert.NotContains(t, kinds(att), "privacy_unanswered")
	assert.NotContains(t, kinds(att), "instance_offline", "the Lounge is up")
}

// stubLibraries lists fixed libraries, or fails.
type stubLibraries struct {
	Libraries

	list []Library
	err  error
}

func (s stubLibraries) List(context.Context) ([]Library, error) { return s.list, s.err }

func (s stubLibraries) LibraryOf(context.Context, int64) (Library, error) {
	return Library{}, store.ErrNotFound
}

func TestOverviewSummarisesLibraries(t *testing.T) {
	older := time.Date(2026, 10, 27, 22, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.Libraries = stubLibraries{list: []Library{
			{ID: 1, Members: []LibraryMember{{InstanceID: 1}, {InstanceID: 2}}, LastSyncAt: &older},
			{ID: 2, Members: []LibraryMember{{InstanceID: 3}}, LastSyncAt: &newer},
			{ID: 3, Members: []LibraryMember{{InstanceID: 4}}},
		}}
	}})
	c := h.admin()

	repo := h.app.DB().RequestLog()
	require.NoError(t, repo.Insert(t.Context(), &store.RequestLogEntry{DeviceID: "amzn1.ask.device.A", Intent: "WatchMovie", Outcome: "played", Duration: 300 * time.Millisecond}))
	require.NoError(t, repo.Insert(t.Context(), &store.RequestLogEntry{DeviceID: "amzn1.ask.device.A", Intent: "WatchMovie", Outcome: "error", Duration: 100 * time.Millisecond}))

	var o Overview

	resp := c.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, &o)
	assert.Equal(t, 3, o.Libraries.Count)
	assert.Equal(t, 1, o.Libraries.SharedGroups)
	require.NotNil(t, o.Libraries.SyncedAt)
	assert.Equal(t, newer, *o.Libraries.SyncedAt, "the latest sync of any library")
	assert.Equal(t, RequestsToday{Total: 2, Failed: 1, MedianMS: 300}, o.RequestsToday)
	assert.Len(t, o.RecentRequests, 2)
	assert.Equal(t, "not_deployed", o.Skill.State)
	assert.Equal(t, "development", o.Skill.Stage)
	assert.NotEmpty(t, o.Skill.Locales)
	assert.Contains(t, string(resp.body), `"instances":[]`, "an empty list, not null")
	assert.Equal(t, h.server.URL+"/api/v1/alexa", o.Endpoint.URL, "derived from how the browser reached Encore")
	assert.False(t, o.Endpoint.Reachable, "plain HTTP isn't checked")
}

func TestOverviewEndpointCheck(t *testing.T) {
	srv := httptest.NewTLSServer(encoreLike())
	t.Cleanup(srv.Close)

	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.HTTPClient = srv.Client() }})
	c := h.admin()

	require.NoError(t, h.app.DB().Settings().Set(t.Context(), skillSettingsKey, skillSettings{Endpoint: SkillEndpoint{URL: srv.URL + "/api/v1/alexa"}}))

	var o Overview

	c.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, &o)
	assert.Equal(t, srv.URL+"/api/v1/alexa", o.Endpoint.URL)
	assert.True(t, o.Endpoint.Reachable)
	assert.NotNil(t, o.Endpoint.CheckedAt)
}

func TestOverviewFailsWhenLibrariesDo(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.Libraries = stubLibraries{err: errors.New("catalog unavailable")}
	}})
	c := h.admin()

	p := c.do(http.MethodGet, "/api/v1/overview", nil).problem(t)
	assert.Equal(t, http.StatusInternalServerError, p.Status)
	assert.NotContains(t, p.Detail, "catalog unavailable", "internal errors aren't shown")
	assert.Contains(t, h.logs.String(), "catalog unavailable", "but are logged")
}

func TestAttentionFailsOnUnreadableSkillSettings(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	require.NoError(t, h.app.DB().Settings().Set(t.Context(), lwaSettingsKey, "not settings"))

	_, err := h.api.attention(t.Context())
	require.Error(t, err, "settings that can't be read aren't taken as none")
}

// A database that stops answering must surface as errors, never as empty results that look valid.
func TestDatabaseErrorsArePassedOn(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	ctx := t.Context()
	s := h.api.skill

	require.NoError(t, h.app.DB().Close())

	for name, call := range map[string]func() error{
		"overview":       func() error { _, err := h.api.overview(ctx, &endpointCache{}); return err },
		"skill overview": func() error { _, err := s.overview(ctx); return err },
		"skill info":     func() error { _, err := s.info(ctx); return err },
		"attention":      func() error { _, err := h.api.attention(ctx); return err },
		"instances":      func() error { _, err := h.api.instances(ctx); return err },
		"instance":       func() error { _, err := h.api.instance(ctx, 1); return err },
		"discover":       func() error { _, err := h.api.discover(ctx); return err },
		"create": func() error {
			_, err := h.api.createInstance(ctx, InstanceInput{Name: new("Den"), URL: new("http://192.0.2.1:8080/jsonrpc")})
			return err
		},
		"update":       func() error { _, err := h.api.updateInstance(ctx, 1, InstanceInput{}); return err },
		"delete":       func() error { return h.api.deleteInstance(ctx, 1) },
		"test":         func() error { _, err := h.api.testInstance(ctx, 1); return err },
		"start deploy": func() error { _, err := s.startDeploy(ctx, false, false); return err },
		"export":       func() error { _, err := s.exportPackage(ctx); return err },
		"credentials":  func() error { return s.saveCredentials(ctx, "client", "secret") },
		"settings":     func() error { return s.saveSettings(ctx, SkillSettingsInput{LocalesEnabled: []string{"en-AU"}}) },
	} {
		require.ErrorContains(t, call(), "database is closed", name)
	}

	assert.Nil(t, s.lastDeploy(ctx), "no last deploy can be read")
	assert.Nil(t, s.deploy(ctx, "d-1"))
}

func TestAttentionForRejectedRequests(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.loungeID()

	rejected := h.app.Rejections()

	for range 3 {
		rejected.Record(alexa.RejectTimestamp)
	}

	rejected.Record(alexa.RejectSignature)
	rejected.Record(alexa.RejectSkillID)
	rejected.Record(alexa.RejectUnsigned)
	rejected.Record(alexa.RejectBody)

	att, err := h.api.attention(t.Context())
	require.NoError(t, err)

	var got []Attention

	for _, a := range att {
		if a.Kind == "requests_rejected" {
			got = append(got, a)
		}
	}

	assert.Equal(t, []Attention{
		{
			Kind: "requests_rejected", Stage: "timestamp", Count: 3,
			Title:  "3 requests rejected for a stale timestamp",
			Detail: "Alexa stamps each request with the time it was sent, and Encore refuses one more than 150 seconds from its own clock. Check that this host's clock is synchronised.",
		},
		{
			Kind: "requests_rejected", Stage: "signature", Count: 1,
			Title:  "1 request rejected for a bad signature",
			Detail: "Encore couldn't confirm that Alexa sent them. If they came from your Echo, check that Encore can reach s3.amazonaws.com for Amazon's certificate and that your proxy passes requests on unchanged.",
		},
		{
			Kind: "requests_rejected", Stage: "skill_id", Count: 1, Link: "/alexa-skill",
			Title:  "1 request for another skill rejected",
			Detail: "Alexa signed them for a skill ID other than the one Encore expects. Check alexa.skill_id, or deploy the skill again.",
		},
	}, got, "unsigned requests and unreadable bodies come from scanners as often as from a misconfiguration, so they raise nothing")

	var o Overview

	h.admin().json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, &o)

	stages := map[string]int{}
	for _, r := range o.RejectedRequests {
		stages[r.Stage] = r.Count
		assert.False(t, r.LastAt.IsZero(), r.Stage)
	}

	assert.Equal(t, map[string]int{"body": 1, "unsigned": 1, "timestamp": 3, "signature": 1, "skill_id": 1}, stages, "the overview counts every stage")

	h.skew.Add(int64(alexa.RecentRejections + time.Hour))

	att, err = h.api.attention(t.Context())
	require.NoError(t, err)
	assert.NotContains(t, kinds(att), "requests_rejected", "a day without rejections clears the item")
}

func TestOverviewWithoutRejections(t *testing.T) {
	h := newHarness(t, harnessOpts{})

	var raw map[string]any

	h.admin().json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, &raw)
	assert.Equal(t, []any{}, raw["rejected_requests"], "no rejections is an empty list, not null")
}
