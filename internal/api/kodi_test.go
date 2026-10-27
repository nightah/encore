package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
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
