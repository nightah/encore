package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

func TestKodiInstances(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lounge := h.loungeID()

	var list []Instance

	require.Eventually(t, func() bool {
		c.json(http.MethodGet, "/api/v1/kodi/instances", nil, http.StatusOK, &list)

		return len(list) == 1 && list[0].Status.State == "connected" && list[0].Status.KodiVersion != ""
	}, 10*time.Second, 50*time.Millisecond)

	inst := list[0]
	assert.Equal(t, lounge, inst.ID)
	assert.Equal(t, "Lounge", inst.Name)
	assert.True(t, inst.ManagedByConfig)
	assert.True(t, inst.DefaultForUnlinked)
	assert.Equal(t, lounge, inst.Library.GroupID)
	assert.True(t, inst.Library.IsSource)
	assert.False(t, inst.Library.Shared)
	assert.Equal(t, 7, inst.Library.Movies)
	assert.Equal(t, 4, inst.Library.Shows)
	assert.Equal(t, 4, inst.Library.Episodes, "the episode total comes from Kodi")
	require.NotNil(t, inst.Library.NewestTitle.V)
	assert.Equal(t, "Conclave", inst.Library.NewestTitle.V.Title)
	assert.True(t, inst.Library.NewestTitle.V.Findable)
	assert.Nil(t, inst.NowPlaying.V)
	assert.NotNil(t, inst.Status.LastSyncAt)

	t.Run("config-managed instances are read-only", func(t *testing.T) {
		assert.Equal(t, http.StatusConflict, c.do(http.MethodPatch, fmt.Sprintf("/api/v1/kodi/instances/%d", lounge), map[string]string{"name": "Den"}).problem(t).Status)
		assert.Equal(t, http.StatusConflict, c.do(http.MethodDelete, fmt.Sprintf("/api/v1/kodi/instances/%d", lounge), nil).status)
	})

	t.Run("test checks", func(t *testing.T) {
		var res struct {
			Checks []Check `json:"checks"`
		}

		c.json(http.MethodPost, fmt.Sprintf("/api/v1/kodi/instances/%d/test", lounge), nil, http.StatusOK, &res)
		require.Len(t, res.Checks, 4)

		for _, ch := range res.Checks {
			assert.True(t, ch.OK, "%s: %s", ch.Name, ch.Detail)
		}

		assert.Equal(t, "7 movies", res.Checks[3].Detail)
	})

	t.Run("sync and wake are accepted", func(t *testing.T) {
		var acc Accepted

		c.json(http.MethodPost, fmt.Sprintf("/api/v1/kodi/instances/%d/sync", lounge), nil, http.StatusAccepted, &acc)
		assert.NotEmpty(t, acc.ID)

		assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, fmt.Sprintf("/api/v1/kodi/instances/%d/wake", lounge), nil).status, "no MAC address known yet")
		assert.Equal(t, http.StatusNotFound, c.do(http.MethodPost, "/api/v1/kodi/instances/999/sync", nil).status)
	})
}

func TestKodiInstanceLifecycle(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	bedroom := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "s3cret"})

	p := c.do(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Bedroom", "url": "ftp://nope"}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Equal(t, "body.url", p.Errors[0].Location)

	var created Instance

	// Credentials in the URL are moved to their own fields and never shown back.
	withCreds := "http://kodi:s3cret@" + bedroom.URL()[len("http://"):]
	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Bedroom", "url": withCreds, "websocket_url": bedroom.WebSocketURL(), "mac": "dc:a6:32:4b:19:0e"}, http.StatusCreated, &created)
	assert.Equal(t, "Bedroom", created.Name)
	assert.Equal(t, "kodi", created.Username)
	assert.True(t, created.HasPassword)
	assert.NotContains(t, created.URL, "s3cret")
	assert.Equal(t, "DC:A6:32:4B:19:0E", created.MAC)
	assert.True(t, created.DefaultForUnlinked, "the first instance answers unlinked Echos")
	assert.False(t, created.ManagedByConfig)

	path := fmt.Sprintf("/api/v1/kodi/instances/%d", created.ID)

	// The runtime starts without a restart.
	require.Eventually(t, func() bool {
		var inst Instance

		c.json(http.MethodGet, path, nil, http.StatusOK, &inst)

		return inst.Library.Movies == 7
	}, 10*time.Second, 50*time.Millisecond)

	var res struct {
		Checks []Check `json:"checks"`
	}

	c.json(http.MethodPost, path+"/test", nil, http.StatusOK, &res)
	assert.True(t, res.Checks[2].OK, "the stored password authenticates: %+v", res.Checks)

	var renamed Instance

	c.json(http.MethodPatch, path, map[string]string{"name": "Master bedroom"}, http.StatusOK, &renamed)
	assert.Equal(t, "Master bedroom", renamed.Name)
	assert.True(t, renamed.HasPassword, "the password follows a rename")

	c.json(http.MethodPost, path+"/test", nil, http.StatusOK, &res)
	assert.True(t, res.Checks[2].OK)

	assert.Equal(t, "en", renamed.Language)
	assert.Empty(t, renamed.Timezone, "learnt from Kodi's clock")
	assert.Equal(t, "inherit", renamed.Shutdown)

	var tuned Instance

	c.json(http.MethodPatch, path, map[string]string{"language": "de", "timezone": "Australia/Melbourne", "shutdown": "quit"}, http.StatusOK, &tuned)
	assert.Equal(t, "de", tuned.Language)
	assert.Equal(t, "Australia/Melbourne", tuned.Timezone)
	assert.Equal(t, "quit", tuned.Shutdown)

	rec, err := h.app.DB().KodiInstances().Get(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, new(true), rec.ShutdownQuits)

	for field, value := range map[string]string{"language": "xx", "timezone": "Mars/Olympus"} {
		p := c.do(http.MethodPatch, path, map[string]string{field: value}).problem(t)
		assert.Equal(t, http.StatusUnprocessableEntity, p.Status, field)
		assert.Equal(t, "body."+field, p.Errors[0].Location)
	}

	c.json(http.MethodPatch, path, map[string]string{"timezone": "", "shutdown": "inherit"}, http.StatusOK, &tuned)
	assert.Empty(t, tuned.Timezone)
	assert.Equal(t, "inherit", tuned.Shutdown)
	assert.Equal(t, "de", tuned.Language, "fields left out are kept")

	c.json(http.MethodPatch, path, map[string]string{"password": "wrong"}, http.StatusOK, nil)
	c.json(http.MethodPost, path+"/test", nil, http.StatusOK, &res)
	assert.False(t, res.Checks[2].OK)
	assert.Contains(t, res.Checks[2].Detail, "rejected")

	var acc Accepted

	c.json(http.MethodPost, path+"/wake", nil, http.StatusAccepted, &acc)
	assert.NotEmpty(t, acc.ID)

	c.json(http.MethodPost, path+"/default", nil, http.StatusNoContent, nil)
	c.json(http.MethodDelete, path, nil, http.StatusNoContent, nil)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodGet, path, nil).status)
	assert.Empty(t, h.app.InstanceIDs())
}

func TestDiscoveredExcludesConfigured(t *testing.T) {
	var h *harness

	h = newHarness(t, harnessOpts{discover: func(context.Context) ([]kodi.Instance, error) {
		host, port, _ := net.SplitHostPort(h.kodi.URL()[len("http://") : len(h.kodi.URL())-len("/jsonrpc")])

		var p int

		_, _ = fmt.Sscan(port, &p)

		return []kodi.Instance{
			{Name: "Kodi (lounge)", Host: host, Addrs: []net.IP{net.ParseIP(host)}, Port: p, WebSocketPort: 9090},
			{Name: "Kodi (garage)", Host: "garage.local.", Addrs: []net.IP{net.ParseIP("192.0.2.77")}, Port: 8080, WebSocketPort: 9090},
		}, nil
	}})
	c := h.admin()

	var found []DiscoveredInstance

	c.json(http.MethodGet, "/api/v1/kodi/discovered", nil, http.StatusOK, &found)
	require.Len(t, found, 1)
	assert.Equal(t, DiscoveredInstance{Name: "Kodi (garage)", Host: "192.0.2.77", HTTPPort: 8080, WSPort: 9090, Version: "21.3"}, found[0])
}

func TestLibraries(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lounge := h.loungeID()

	var libs []Library

	require.Eventually(t, func() bool {
		c.json(http.MethodGet, "/api/v1/libraries", nil, http.StatusOK, &libs)

		return len(libs) == 1 && libs[0].Counts.Movies == 7
	}, 5*time.Second, 50*time.Millisecond)

	libID := libs[0].ID
	assert.Equal(t, "solo", libs[0].State)
	assert.Equal(t, []LibraryMember{{InstanceID: lounge, Name: "Lounge"}}, libs[0].Members)
	assert.Equal(t, 7, libs[0].Counts.Movies)

	var recent []RecentTitle

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/libraries/%d/recent?limit=3", libID), nil, http.StatusOK, &recent)
	require.Len(t, recent, 3)
	assert.Equal(t, "Conclave", recent[0].Title)
	assert.Equal(t, "Lounge", recent[0].Instance)
	assert.True(t, recent[0].Findable)
	assert.True(t, recent[0].InSkillModel, "library titles go into the skill model")

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPost, "/api/v1/libraries/group", map[string][]int64{"instance_ids": {lounge, 99}}).problem(t).Status)
	c.json(http.MethodPatch, fmt.Sprintf("/api/v1/libraries/%d", libID), map[string]int64{"source_instance_id": lounge}, http.StatusOK, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, fmt.Sprintf("/api/v1/libraries/%d", libID), map[string]int64{"source_instance_id": 99}).status)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/v1/libraries/999/recent", nil).status)

	// A second instance, grouped by hand, shares the library and can be split off again.
	bedroom := fakekodi.New(t, fakekodi.Options{})

	var created Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Bedroom", "url": bedroom.URL()}, http.StatusCreated, &created)

	var shared Library

	c.json(http.MethodPost, "/api/v1/libraries/group", map[string][]int64{"instance_ids": {lounge, created.ID}}, http.StatusOK, &shared)
	assert.Equal(t, "manual", shared.State)
	assert.Equal(t, lounge, shared.SourceInstanceID)
	assert.Equal(t, []LibraryMember{{InstanceID: lounge, Name: "Lounge"}, {InstanceID: created.ID, Name: "Bedroom"}}, shared.Members)

	var inst Instance

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/kodi/instances/%d", created.ID), nil, http.StatusOK, &inst)
	assert.Equal(t, shared.ID, inst.Library.GroupID)
	assert.True(t, inst.Library.Shared)
	assert.False(t, inst.Library.IsSource)

	c.json(http.MethodPost, fmt.Sprintf("/api/v1/libraries/%d/split", shared.ID), map[string]int64{"instance_id": created.ID}, http.StatusNoContent, nil)
	c.json(http.MethodGet, "/api/v1/libraries", nil, http.StatusOK, &libs)
	assert.Len(t, libs, 2)
}

func TestMatchAndAliases(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lib := h.libraryID(t)

	var res MatchResult

	c.json(http.MethodPost, "/api/v1/match", map[string]any{"library_id": lib, "query": "hot fuzz", "kinds": []string{"movie"}}, http.StatusOK, &res)
	assert.Equal(t, "play", res.Decision)
	require.NotEmpty(t, res.Candidates)
	assert.Equal(t, "Hot Fuzz", res.Candidates[0].Title)
	assert.Equal(t, "hot fuzz", res.Normalized)
	assert.Positive(t, res.Candidates[0].Breakdown.TokenSet)

	c.json(http.MethodPost, "/api/v1/match", map[string]any{"library_id": lib, "query": "dune"}, http.StatusOK, &res)
	assert.Equal(t, "ask", res.Decision)

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/match", map[string]any{"library_id": lib, "query": "x", "kinds": []string{"podcast"}}).status)

	var alias TitleAlias

	c.json(http.MethodPost, "/api/v1/aliases", map[string]any{"library_id": lib, "kind": "movie", "media_id": 6, "alias": "the cornetto one"}, http.StatusCreated, &alias)
	assert.Equal(t, "Hot Fuzz", alias.Title)
	assert.Equal(t, lib, alias.LibraryID)

	assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, "/api/v1/aliases", map[string]any{"library_id": lib, "kind": "movie", "media_id": 6, "alias": "The Cornetto One"}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/aliases", map[string]any{"library_id": lib, "kind": "movie", "media_id": 999, "alias": "x"}).status)

	var aliases []TitleAlias

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/aliases?library_id=%d", lib), nil, http.StatusOK, &aliases)
	require.Len(t, aliases, 1)

	// The matcher finds the title by its alias straight away.
	c.json(http.MethodPost, "/api/v1/match", map[string]any{"library_id": lib, "query": "the cornetto one"}, http.StatusOK, &res)
	require.NotEmpty(t, res.Candidates)
	assert.Equal(t, "Hot Fuzz", res.Candidates[0].Title)
	assert.Equal(t, "play", res.Decision)

	c.json(http.MethodDelete, fmt.Sprintf("/api/v1/aliases/%d", alias.ID), nil, http.StatusNoContent, nil)

	c.json(http.MethodPost, "/api/v1/match", map[string]any{"library_id": lib, "query": "the cornetto one"}, http.StatusOK, &res)
	assert.NotEqual(t, "play", res.Decision, "a removed alias no longer matches")
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodDelete, fmt.Sprintf("/api/v1/aliases/%d", alias.ID), nil).status)
}

// fieldLocation returns where a validation error points and the value it reports, or fails when err isn't one.
func fieldLocation(t *testing.T, err error) (string, any) {
	t.Helper()

	var em *huma.ErrorModel

	require.ErrorAs(t, err, &em)
	require.Equal(t, http.StatusUnprocessableEntity, em.Status, em.Detail)
	require.Len(t, em.Errors, 1)

	return em.Errors[0].Location, em.Errors[0].Value
}

func TestApplyInstanceInputRejects(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in       InstanceInput
		creating bool
		location string
	}{
		"blank name":                  {in: InstanceInput{Name: new("   ")}, location: "body.name"},
		"create without url":          {in: InstanceInput{Name: new("Den")}, creating: true, location: "body"},
		"create without name":         {in: InstanceInput{URL: new("http://192.0.2.1:8080/jsonrpc")}, creating: true, location: "body"},
		"ftp url":                     {in: InstanceInput{URL: new("ftp://192.0.2.1/jsonrpc")}, location: "body.url"},
		"url without host":            {in: InstanceInput{URL: new("http:///jsonrpc")}, location: "body.url"},
		"unparsable url":              {in: InstanceInput{URL: new("http://[::1")}, location: "body.url"},
		"port overflows":              {in: InstanceInput{URL: new("http://192.0.2.1:99999999999999999999/jsonrpc")}, location: "body.url"},
		"port out of range":           {in: InstanceInput{URL: new("http://192.0.2.1:65536/jsonrpc")}, location: "body.url"},
		"websocket port out of range": {in: InstanceInput{WebSocketURL: new("ws://192.0.2.1:70000/jsonrpc")}, location: "body.websocket_url"},
		"http websocket url":          {in: InstanceInput{WebSocketURL: new("http://192.0.2.1:9090/jsonrpc")}, location: "body.websocket_url"},
		"websocket url without host":  {in: InstanceInput{WebSocketURL: new("ws:///jsonrpc")}, location: "body.websocket_url"},
		"not a mac":                   {in: InstanceInput{MAC: new("dc:a6:32")}, location: "body.mac"},
		"broadcast host name":         {in: InstanceInput{Broadcast: new("broadcast.lan")}, location: "body.broadcast"},
		"unknown language":            {in: InstanceInput{Language: new("tlh")}, location: "body.language"},
		"unknown time zone":           {in: InstanceInput{Timezone: new("Mars/Olympus")}, location: "body.timezone"},
		"server-local time zone":      {in: InstanceInput{Timezone: new("Local")}, location: "body.timezone"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := store.KodiInstance{Name: "Lounge", Scheme: "http", Host: "192.0.2.10", Port: 8080, Path: "/jsonrpc", WSPort: 9090}

			_, err := applyInstanceInput(&rec, tc.in, tc.creating)
			loc, _ := fieldLocation(t, err)
			assert.Equal(t, tc.location, loc)
		})
	}
}

// The database refuses an out-of-range port too, but only after the password is stored, and with its own constraint text.
func TestKodiInstancePortOutOfRange(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	p := c.do(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Den", "url": "http://192.0.2.1:70000/jsonrpc", "password": "s3cret"}).problem(t)
	require.Equal(t, http.StatusUnprocessableEntity, p.Status)
	require.Len(t, p.Errors, 1, "%+v", p)
	assert.Equal(t, "body.url", p.Errors[0].Location)
	assert.NotContains(t, p.Detail, "constraint")

	_, err := h.app.Secrets().Get(t.Context(), "kodi/Den/password")
	require.ErrorIs(t, err, secrets.ErrNotFound, "a refused instance leaves no password behind")
}

func TestApplyInstanceInputNeverEchoesURLPassword(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in       InstanceInput
		location string
	}{
		"ftp url":                     {in: InstanceInput{URL: new("ftp://kodi:s3cret@192.0.2.1/jsonrpc")}, location: "body.url"},                          // trufflehog:ignore
		"unparsable port":             {in: InstanceInput{URL: new("http://kodi:s3cret@192.0.2.1:80a/jsonrpc")}, location: "body.url"},                     // trufflehog:ignore
		"unparsable host":             {in: InstanceInput{URL: new("http://kodi:s3cret@[::1/jsonrpc")}, location: "body.url"},                              // trufflehog:ignore
		"port out of range":           {in: InstanceInput{URL: new("http://kodi:s3cret@192.0.2.1:70000/jsonrpc")}, location: "body.url"},                   // trufflehog:ignore
		"http websocket url":          {in: InstanceInput{WebSocketURL: new("http://kodi:s3cret@192.0.2.1:9090/jsonrpc")}, location: "body.websocket_url"}, // trufflehog:ignore
		"unparsable websocket url":    {in: InstanceInput{WebSocketURL: new("ws://kodi:s3cret@[::1/jsonrpc")}, location: "body.websocket_url"},             // trufflehog:ignore
		"websocket port out of range": {in: InstanceInput{WebSocketURL: new("ws://kodi:s3cret@192.0.2.1:70000/jsonrpc")}, location: "body.websocket_url"},  // trufflehog:ignore
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := store.KodiInstance{Name: "Lounge", Scheme: "http", Host: "192.0.2.10", Port: 8080, Path: "/jsonrpc", WSPort: 9090}

			_, err := applyInstanceInput(&rec, tc.in, false)
			loc, value := fieldLocation(t, err)
			assert.Equal(t, tc.location, loc)
			assert.NotContains(t, fmt.Sprint(value), "s3cret")
			assert.Contains(t, fmt.Sprint(value), "kodi:xxxxx@", "the value is still shown, redacted")
			assert.NotContains(t, err.Error(), "s3cret")
		})
	}
}

func TestApplyInstanceInput(t *testing.T) {
	t.Parallel()

	existing := func() store.KodiInstance {
		return store.KodiInstance{Name: "Lounge", Scheme: "http", Host: "192.0.2.10", Port: 8080, Path: "/jsonrpc", WSPort: 9090, Username: "kodi", MAC: "DC:A6:32:4B:19:0E", BroadcastAddr: "192.0.2.255", Language: "de", Timezone: "Australia/Melbourne", ShutdownQuits: new(true)}
	}

	t.Run("credentials in the url move to their own fields", func(t *testing.T) {
		t.Parallel()

		rec := existing()
		password, err := applyInstanceInput(&rec, InstanceInput{URL: new(" https://alice:pa55@kodi.lan:8443/rpc ")}, false) // trufflehog:ignore
		require.NoError(t, err)
		require.NotNil(t, password)
		assert.Equal(t, "pa55", *password)
		assert.Equal(t, "alice", rec.Username)
		assert.Equal(t, "https://kodi.lan:8443/rpc", httpURL(rec), "the stored URL has no credentials")
		assert.Equal(t, "wss://kodi.lan:9090/rpc", wsURL(rec), "the WebSocket URL keeps its port and follows the scheme")
	})

	t.Run("explicit credentials win over the url's", func(t *testing.T) {
		t.Parallel()

		rec := existing()
		password, err := applyInstanceInput(&rec, InstanceInput{URL: new("http://alice:pa55@kodi.lan/jsonrpc"), Username: new(" bob "), Password: new("hunter2")}, false) // trufflehog:ignore
		require.NoError(t, err)
		require.NotNil(t, password)
		assert.Equal(t, "hunter2", *password)
		assert.Equal(t, "bob", rec.Username)
		assert.Equal(t, 8080, rec.Port, "Kodi's default port")
	})

	t.Run("an empty websocket url turns live updates off", func(t *testing.T) {
		t.Parallel()

		rec := existing()
		password, err := applyInstanceInput(&rec, InstanceInput{WebSocketURL: new("")}, false)
		require.NoError(t, err)
		assert.Nil(t, password, "unchanged")
		assert.Zero(t, rec.WSPort)
		assert.Empty(t, wsURL(rec))
		assert.Equal(t, "http://192.0.2.10:8080/jsonrpc", httpURL(rec), "the HTTP URL is kept")
	})

	t.Run("fields left out are kept", func(t *testing.T) {
		t.Parallel()

		rec := existing()
		password, err := applyInstanceInput(&rec, InstanceInput{}, false)
		require.NoError(t, err)
		assert.Nil(t, password)
		assert.Equal(t, existing(), rec)
	})

	t.Run("normalises and clears", func(t *testing.T) {
		t.Parallel()

		rec := existing()
		_, err := applyInstanceInput(&rec, InstanceInput{Name: new(" Den "), MAC: new("aa-bb-cc-dd-ee-ff"), Broadcast: new(" 10.0.0.255 "), Language: new(" en "), Timezone: new(""), Shutdown: new(shutdownPowerOff)}, false)
		require.NoError(t, err)
		assert.Equal(t, "Den", rec.Name)
		assert.Equal(t, "AA:BB:CC:DD:EE:FF", rec.MAC)
		assert.Equal(t, "10.0.0.255", rec.BroadcastAddr)
		assert.Equal(t, "en", rec.Language)
		assert.Empty(t, rec.Timezone, "empty learns the zone from Kodi again")
		assert.Equal(t, new(false), rec.ShutdownQuits)

		_, err = applyInstanceInput(&rec, InstanceInput{MAC: new(" "), Broadcast: new(""), Shutdown: new(shutdownInherit)}, false)
		require.NoError(t, err)
		assert.Empty(t, rec.MAC)
		assert.Empty(t, rec.BroadcastAddr)
		assert.Nil(t, rec.ShutdownQuits)
	})
}

func TestShutdownOf(t *testing.T) {
	t.Parallel()

	assert.Equal(t, shutdownInherit, shutdownOf(nil))
	assert.Equal(t, shutdownQuit, shutdownOf(new(true)))
	assert.Equal(t, shutdownPowerOff, shutdownOf(new(false)))
}

func TestStateOf(t *testing.T) {
	t.Parallel()

	for want, st := range map[string]app.InstanceStatus{
		"connected": {Connected: true, Reachable: true, CanWake: true},
		"http_only": {Reachable: true, CanWake: true},
		"asleep":    {CanWake: true},
		"offline":   {},
	} {
		assert.Equal(t, want, stateOf(st), "%+v", st)
	}
}

func TestTimePtr(t *testing.T) {
	t.Parallel()

	assert.Nil(t, timePtr(time.Time{}), "a zero time is absent")

	melbourne, err := time.LoadLocation("Australia/Melbourne")
	require.NoError(t, err)

	at := time.Date(2026, 10, 28, 9, 30, 0, 0, melbourne)
	got := timePtr(at)
	require.NotNil(t, got)
	assert.Equal(t, time.UTC, got.Location(), "times are reported in UTC")
	assert.True(t, got.Equal(at))
}

func TestRedactURL(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "http://kodi:xxxxx@192.0.2.1/jsonrpc", redactURL("http://kodi:s3cret@192.0.2.1/jsonrpc")) // trufflehog:ignore
	assert.Equal(t, "http://192.0.2.1/jsonrpc", redactURL("http://192.0.2.1/jsonrpc"))
	assert.Equal(t, "http://[::1", redactURL("http://[::1"), "an unparsable URL without credentials is kept")
	assert.Equal(t, "http://kodi:xxxxx@[::1/jsonrpc", redactURL("http://kodi:s3cret@[::1/jsonrpc"), "an unparsable URL still hides its password") // trufflehog:ignore
	assert.Equal(t, "ws://kodi:xxxxx@192.0.2.1:80a?x=@", redactURL("ws://kodi:s3cret@192.0.2.1:80a?x=@"), "only the authority's @ counts")        // trufflehog:ignore
	assert.Equal(t, "http://kodi@[::1", redactURL("http://kodi@[::1"), "a user name alone isn't secret")
}

func TestArtURL(t *testing.T) {
	t.Parallel()

	assert.Empty(t, artURL(3, ""), "no artwork, no URL")
	assert.Equal(t, "/api/v1/kodi/instances/3/art?ref=image%3A%2F%2Fsmb%3A%2F%2Fnas%2Fposter+%C3%A9t%C3%A9.jpg%2F", artURL(3, "image://smb://nas/poster été.jpg/"))
}

func TestJobIDsAreUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for range 100 {
		id := jobID("d")
		require.True(t, strings.HasPrefix(id, "d-"), id)
		require.False(t, seen[id], "duplicate %s", id)

		seen[id] = true
	}
}

func TestToInstanceDescribesStatus(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	rec := store.KodiInstance{ID: 42, Name: "Den", Scheme: "http", Host: "192.0.2.5", Port: 8080, Path: "/jsonrpc", PasswordSecretRef: "kodi/Den/password"}

	offline := h.api.toInstance(t.Context(), rec, app.InstanceStatus{Connected: true}, false)
	assert.Equal(t, "offline", offline.Status.State, "without a status the instance is offline")
	assert.Equal(t, "en", offline.Language, "English matching by default")
	assert.Empty(t, offline.WebSocketURL, "no WebSocket port, no live updates")
	assert.True(t, offline.HasPassword)
	assert.Nil(t, offline.NowPlaying.V)

	st := app.InstanceStatus{
		Reachable: true, KodiVersion: "21.3", APIVersion: "13.5.0",
		Counts: app.LibraryCounts{Movies: 2, Shows: 1, Episodes: 9, Songs: 4},
		Newest: &media.Item{Kind: media.KindEpisode, ID: 5, Title: "The Rains of Castamere", ShowTitle: "Game of Thrones", Year: 2013},
		NowPlaying: &media.NowPlaying{
			Item:     media.Item{Kind: media.KindEpisode, Title: "Pilot", ShowTitle: "Severance", Season: 1, Episode: 1, Art: media.Artwork{Thumb: "image://thumb/"}},
			Position: 90*time.Second + 900*time.Millisecond, Duration: time.Hour, Paused: true,
		},
	}

	inst := h.api.toInstance(t.Context(), rec, st, true)
	assert.Equal(t, "http_only", inst.Status.State)
	assert.Equal(t, "21.3", inst.Status.KodiVersion)
	assert.Nil(t, inst.Status.LastSyncAt, "never synced")
	assert.Equal(t, [4]int{2, 1, 9, 4}, [4]int{inst.Library.Movies, inst.Library.Shows, inst.Library.Episodes, inst.Library.Songs})
	require.NotNil(t, inst.Library.NewestTitle.V)
	assert.Equal(t, "Game of Thrones: The Rains of Castamere", inst.Library.NewestTitle.V.Title, "an episode is named with its show")
	assert.Equal(t, 2013, inst.Library.NewestTitle.V.Year)
	assert.False(t, inst.Library.NewestTitle.V.Findable, "no runtime has indexed it")

	np := inst.NowPlaying.V
	require.NotNil(t, np)
	assert.Equal(t, NowPlaying{Kind: "episode", Title: "Pilot", Show: "Severance", Season: 1, Episode: 1, PositionS: 90, DurationS: 3600, Paused: true, ArtURL: "/api/v1/kodi/instances/42/art?ref=image%3A%2F%2Fthumb%2F"}, *np)

	movie := h.api.toInstance(t.Context(), rec, app.InstanceStatus{Newest: &media.Item{Kind: media.KindMovie, Title: "Dune", ShowTitle: "ignored"}}, true)
	assert.Equal(t, "Dune", movie.Library.NewestTitle.V.Title, "only episodes take the show's name")
}

func TestKodiInstanceErrors(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/kodi/instances/999"},
		{http.MethodPatch, "/api/v1/kodi/instances/999"},
		{http.MethodDelete, "/api/v1/kodi/instances/999"},
		{http.MethodPost, "/api/v1/kodi/instances/999/test"},
		{http.MethodPost, "/api/v1/kodi/instances/999/wake"},
		{http.MethodPost, "/api/v1/kodi/instances/999/default"},
	} {
		var body any
		if tc.method == http.MethodPatch {
			body = map[string]string{"name": "Den"}
		}

		assert.Equal(t, http.StatusNotFound, c.do(tc.method, tc.path, body).status, "%s %s", tc.method, tc.path)
	}

	var list []Instance

	resp := c.json(http.MethodGet, "/api/v1/kodi/instances", nil, http.StatusOK, &list)
	assert.Equal(t, "[]", strings.TrimSpace(string(resp.body)), "an empty list, not null")

	assert.Equal(t, http.StatusUnauthorized, h.client().do(http.MethodGet, "/api/v1/kodi/instances", nil).status, "listing needs a sign-in")
}

func TestKodiInstanceNamesDeclaredInConfig(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	h.loungeID()

	den := fakekodi.New(t, fakekodi.Options{})

	p := c.do(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "lounge", "url": den.URL()}).problem(t)
	assert.Equal(t, http.StatusConflict, p.Status, "names match config case-insensitively")
	assert.Contains(t, p.Detail, "declared in config")

	var created Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Den", "url": den.URL()}, http.StatusCreated, &created)
	assert.False(t, created.DefaultForUnlinked, "the configured Lounge already answers unlinked Echos")

	p = c.do(http.MethodPatch, fmt.Sprintf("/api/v1/kodi/instances/%d", created.ID), map[string]string{"name": "LOUNGE"}).problem(t)
	assert.Equal(t, http.StatusConflict, p.Status, "a rename can't take a configured name either")
	assert.Equal(t, `a Kodi instance named "LOUNGE" is declared in config`, p.Detail)

	var inst Instance

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/kodi/instances/%d", created.ID), nil, http.StatusOK, &inst)
	assert.Equal(t, "Den", inst.Name, "the refused rename changed nothing")
}

func TestKodiPasswordLifecycle(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	den := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "s3cret"})

	var inst Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Den", "url": den.URL(), "username": "kodi", "password": "s3cret"}, http.StatusCreated, &inst)
	require.True(t, inst.HasPassword)

	path := fmt.Sprintf("/api/v1/kodi/instances/%d", inst.ID)

	secret, err := h.app.Secrets().Get(t.Context(), "kodi/Den/password")
	require.NoError(t, err)
	assert.Equal(t, "s3cret", string(secret))

	// A rename moves the secret to the new name and removes the old one.
	c.json(http.MethodPatch, path, map[string]string{"name": "Study"}, http.StatusOK, &inst)
	assert.True(t, inst.HasPassword)

	_, err = h.app.Secrets().Get(t.Context(), "kodi/Den/password")
	require.ErrorIs(t, err, secrets.ErrNotFound, "the old name's secret is gone")

	secret, err = h.app.Secrets().Get(t.Context(), "kodi/Study/password")
	require.NoError(t, err)
	assert.Equal(t, "s3cret", string(secret))

	// An empty password removes it.
	c.json(http.MethodPatch, path, map[string]string{"password": ""}, http.StatusOK, &inst)
	assert.False(t, inst.HasPassword)

	_, err = h.app.Secrets().Get(t.Context(), "kodi/Study/password")
	require.ErrorIs(t, err, secrets.ErrNotFound)

	var res struct {
		Checks []Check `json:"checks"`
	}

	c.json(http.MethodPost, path+"/test", nil, http.StatusOK, &res)
	require.Len(t, res.Checks, 4)
	assert.False(t, res.Checks[2].OK, "without the password Kodi refuses: %+v", res.Checks[2])

	// Deleting an instance removes its password too.
	c.json(http.MethodPatch, path, map[string]string{"password": "s3cret"}, http.StatusOK, &inst)
	c.json(http.MethodDelete, path, nil, http.StatusNoContent, nil)

	_, err = h.app.Secrets().Get(t.Context(), "kodi/Study/password")
	require.ErrorIs(t, err, secrets.ErrNotFound)
}

func TestDiscoverErrors(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, discover: func(context.Context) ([]kodi.Instance, error) {
		return nil, errors.New("multicast is blocked")
	}})
	c := h.admin()

	assert.Equal(t, http.StatusInternalServerError, c.do(http.MethodGet, "/api/v1/kodi/discovered", nil).problem(t).Status)
	assert.Contains(t, h.logs.String(), "discovering Kodi instances: multicast is blocked")
}

func TestDiscoverVersions(t *testing.T) {
	var probed sync.Map

	h := newHarness(t, harnessOpts{
		noKodi: true,
		discover: func(context.Context) ([]kodi.Instance, error) {
			return []kodi.Instance{
				{Name: "Kodi (garage)", Host: "garage.local.", Addrs: []net.IP{net.ParseIP("192.0.2.77")}, Port: 8080, WebSocketPort: 9090},
				{Name: "Kodi (shed)", Host: "shed.local.", Addrs: []net.IP{net.ParseIP("192.0.2.78")}, Port: 8080, WebSocketPort: 9091},
			}, nil
		},
		mutate: func(o *Options) {
			o.ProbeVersion = func(ctx context.Context, host string, port int) (string, error) {
				probed.Store(host, port)

				if host == "192.0.2.78" {
					<-ctx.Done() // a host that never answers

					return "", ctx.Err()
				}

				return "22.0", nil
			}
		},
	})
	c := h.admin()

	var found []DiscoveredInstance

	c.json(http.MethodGet, "/api/v1/kodi/discovered", nil, http.StatusOK, &found)
	require.Len(t, found, 2)
	assert.Equal(t, "22.0", found[0].Version)
	assert.Empty(t, found[1].Version, "one that can't be asked has no version")

	port, ok := probed.Load("192.0.2.78")
	require.True(t, ok)
	assert.Equal(t, 9091, port, "the version is asked on the WebSocket port")
}

func TestDiscoverWithoutPortMatchesHost(t *testing.T) {
	var h *harness

	h = newHarness(t, harnessOpts{discover: func(context.Context) ([]kodi.Instance, error) {
		host, _, _ := net.SplitHostPort(strings.TrimPrefix(strings.TrimSuffix(h.kodi.URL(), "/jsonrpc"), "http://"))

		// Seen over mDNS without a port: the host alone says it's configured.
		return []kodi.Instance{{Name: "Kodi", Host: host, Addrs: []net.IP{net.ParseIP(host)}}}, nil
	}})
	c := h.admin()
	h.loungeID()

	var found []DiscoveredInstance

	resp := c.json(http.MethodGet, "/api/v1/kodi/discovered", nil, http.StatusOK, &found)
	assert.Equal(t, "[]", strings.TrimSpace(string(resp.body)))
}

func TestDeletingAnInstanceForgetsItsStatus(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	c := h.admin()
	den := fakekodi.New(t, fakekodi.Options{})

	var inst Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Den", "url": den.URL()}, http.StatusCreated, &inst)

	_, err := h.api.status.get(t.Context(), inst.ID)
	require.NoError(t, err)

	cached := func() bool {
		h.api.status.mu.Lock()
		defer h.api.status.mu.Unlock()

		_, ok := h.api.status.entries[inst.ID]

		return ok
	}

	require.True(t, cached())

	c.json(http.MethodDelete, fmt.Sprintf("/api/v1/kodi/instances/%d", inst.ID), nil, http.StatusNoContent, nil)
	assert.False(t, cached(), "a removed instance's status isn't kept for one that reuses its ID")
}

func TestDiscoverMatchesHostNamesWhateverTheirCase(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, discover: func(context.Context) ([]kodi.Instance, error) {
		return []kodi.Instance{{Name: "Kodi (garage)", Host: "garage.local.", Port: 8080, WebSocketPort: 9090}}, nil
	}})
	c := h.admin()

	// Stored directly: only the saved address matters here, not a runtime.
	require.NoError(t, h.app.DB().KodiInstances().Create(t.Context(), &store.KodiInstance{Name: "Garage", Scheme: "http", Host: "Garage.Local", Port: 8080, Path: "/jsonrpc"}))

	var found []DiscoveredInstance

	c.json(http.MethodGet, "/api/v1/kodi/discovered", nil, http.StatusOK, &found)
	assert.Empty(t, found, "host names aren't case-sensitive")
}
