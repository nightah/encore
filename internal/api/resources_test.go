package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/store"
)

func TestDevices(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lounge := h.loungeID()
	ctx := t.Context()

	dev, err := h.app.DB().EchoDevices().Seen(ctx, "amzn1.ask.device.KITCHEN")
	require.NoError(t, err)
	require.NoError(t, h.app.DB().RequestLog().Insert(ctx, &store.RequestLogEntry{DeviceID: "amzn1.ask.device.KITCHEN", Intent: "WatchMovie", Outcome: "played"}))

	var room Room

	c.json(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "Kitchen"}, http.StatusCreated, &room)

	var list []Device

	c.json(http.MethodGet, "/api/v1/devices", nil, http.StatusOK, &list)
	require.Len(t, list, 1)
	assert.Nil(t, list[0].KodiInstanceID, "unlinked")
	assert.Equal(t, 1, list[0].RequestCountToday)

	var got Device

	path := fmt.Sprintf("/api/v1/devices/%d", dev.ID)
	c.json(http.MethodPatch, path, map[string]any{"name": "Kitchen Echo", "room_id": room.ID, "kodi_instance_id": lounge}, http.StatusOK, &got)
	assert.Equal(t, "Kitchen Echo", got.Name)
	require.NotNil(t, got.RoomID)
	assert.Equal(t, room.ID, *got.RoomID)
	require.NotNil(t, got.KodiInstanceID)

	c.json(http.MethodPatch, path, map[string]any{"kodi_instance_id": nil}, http.StatusOK, &got)
	assert.Nil(t, got.KodiInstanceID, "null unlinks")
	assert.NotNil(t, got.RoomID, "absent fields are left alone")
	assert.Equal(t, "Kitchen Echo", got.Name)

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, path, map[string]any{"room_id": 999}).status, "an unknown room is refused")

	c.json(http.MethodDelete, path, nil, http.StatusNoContent, nil)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodDelete, path, nil).status)
}

func TestRooms(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var lounge, den Room

	c.json(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "Lounge"}, http.StatusCreated, &lounge)
	c.json(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "Den"}, http.StatusCreated, &den)
	assert.Empty(t, lounge.Aliases)
	assert.Nil(t, lounge.KodiInstanceID)

	var alias RoomAlias

	c.json(http.MethodPost, fmt.Sprintf("/api/v1/rooms/%d/aliases", lounge.ID), map[string]string{"alias": "living room"}, http.StatusCreated, &alias)
	assert.Equal(t, "living room", alias.Alias)

	p := c.do(http.MethodPost, fmt.Sprintf("/api/v1/rooms/%d/aliases", den.ID), map[string]string{"alias": "Living-Room"}).problem(t)
	assert.Equal(t, http.StatusConflict, p.Status, "a spoken name belongs to one room")
	assert.Contains(t, p.Detail, "living room")

	assert.Equal(t, http.StatusConflict, c.do(http.MethodPatch, fmt.Sprintf("/api/v1/rooms/%d", den.ID), map[string]string{"name": "lounge"}).status)

	var rooms []Room

	c.json(http.MethodGet, "/api/v1/rooms", nil, http.StatusOK, &rooms)
	require.Len(t, rooms, 2)
	assert.Equal(t, []RoomAlias{alias}, rooms[1].Aliases)

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodDelete, fmt.Sprintf("/api/v1/rooms/%d/aliases/%d", den.ID, alias.ID), nil).status, "the alias belongs to another room")
	c.json(http.MethodDelete, fmt.Sprintf("/api/v1/rooms/%d/aliases/%d", lounge.ID, alias.ID), nil, http.StatusNoContent, nil)
	c.json(http.MethodDelete, fmt.Sprintf("/api/v1/rooms/%d", den.ID), nil, http.StatusNoContent, nil)
}

func logMatch(t *testing.T, target, decision string) json.RawMessage {
	t.Helper()

	b, err := json.Marshal(map[string]any{
		"target": target, "normalized": "hot fuzz", "decision": decision, "margin": 0.4,
		"candidates": []map[string]any{{"title": "Hot Fuzz", "year": 2007, "score": 0.98, "breakdown": map[string]any{"Alias": "hot fuzz", "TokenSet": 1, "JaroWinkler": 0.99}}},
	})
	require.NoError(t, err)

	return b
}

func TestRequests(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	h.loungeID()
	ctx := t.Context()

	dev, err := h.app.DB().EchoDevices().Seen(ctx, "amzn1.ask.device.LOUNGE")
	require.NoError(t, err)

	dev.DisplayName = "Lounge Echo"
	require.NoError(t, h.app.DB().EchoDevices().Update(ctx, &dev))

	entries := []store.RequestLogEntry{
		{DeviceID: "amzn1.ask.device.LOUNGE", Locale: "en-AU", Intent: "WatchMovie", Slots: json.RawMessage(`{"Movie":"hot fuzz"}`), Match: logMatch(t, "Lounge", "play"), Speech: "Playing Hot Fuzz.", Outcome: "played", Duration: 150 * time.Millisecond},
		{DeviceID: "amzn1.ask.device.OTHER", Locale: "en-AU", Intent: "AMAZON.PauseIntent", Outcome: "error", Match: json.RawMessage(`{"error":"` + alexa.ErrUnlinked.Error() + `"}`), Duration: 20 * time.Millisecond},
		{DeviceID: "amzn1.ask.device.LOUNGE", Locale: "en-AU", Intent: "WatchMovie", Slots: json.RawMessage(`{"Movie":"dune"}`), Outcome: "asked", Duration: 90 * time.Millisecond},
	}

	for i := range entries {
		require.NoError(t, h.app.DB().RequestLog().Insert(ctx, &entries[i]))
	}

	var page RequestPage

	c.json(http.MethodGet, "/api/v1/requests?limit=2", nil, http.StatusOK, &page)
	require.Len(t, page.Items, 2)
	assert.Equal(t, entries[2].ID, page.Items[0].ID, "newest first")
	assert.Equal(t, "dune", page.Items[0].Heard)
	assert.Equal(t, "Lounge Echo", page.Items[0].DeviceName)
	require.NotNil(t, page.NextBefore)

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/requests?limit=2&before=%d", *page.NextBefore), nil, http.StatusOK, &page)
	require.Len(t, page.Items, 1)
	assert.Nil(t, page.NextBefore)
	assert.Equal(t, "Lounge", page.Items[0].Target)

	c.json(http.MethodGet, "/api/v1/requests?outcome=unlinked", nil, http.StatusOK, &page)
	require.Len(t, page.Items, 1, "an unlinked device's error reads as unlinked")
	assert.Nil(t, page.Items[0].DeviceID, "a device never seen has no ID")

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/requests?device=%d&q=FUZZ", dev.ID), nil, http.StatusOK, &page)
	require.Len(t, page.Items, 1)

	var detail RequestDetail

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/requests/%d", entries[0].ID), nil, http.StatusOK, &detail)
	assert.Equal(t, map[string]string{"Movie": "hot fuzz"}, detail.Slots)
	require.NotNil(t, detail.Match.V)
	require.Len(t, detail.Match.V.Candidates, 1)

	cand := detail.Match.V.Candidates[0]
	assert.Equal(t, "hot fuzz", cand.Breakdown.Alias)
	assert.InDelta(t, 0.99, cand.Breakdown.JaroWinkler, 1e-9)
	assert.Equal(t, "movie", cand.Kind, "filled in from the index")
	assert.Equal(t, 6, cand.MediaID)
	assert.Empty(t, detail.KodiCalls)

	raw := c.do(http.MethodGet, fmt.Sprintf("/api/v1/requests/%d", entries[0].ID), nil).body
	assert.Contains(t, string(raw), `"jaro_winkler":0.99`, "breakdown keys are snake_case")

	var replay Replay

	calls := len(h.kodi.CallsTo("Player.Open"))
	c.json(http.MethodPost, fmt.Sprintf("/api/v1/requests/%d/replay", entries[0].ID), nil, http.StatusOK, &replay)
	assert.Equal(t, "Playing Hot Fuzz.", replay.Speech)
	require.NotNil(t, replay.Match.V)
	assert.Equal(t, "play", replay.Match.V.Decision)
	assert.Equal(t, "movie", replay.Match.V.Candidates[0].Kind)
	require.Len(t, replay.WouldCall, 1)
	assert.Equal(t, "Player.Open", replay.WouldCall[0].Method)
	assert.EqualValues(t, 6, replay.WouldCall[0].Params["id"])
	assert.Len(t, h.kodi.CallsTo("Player.Open"), calls, "a replay never touches Kodi")

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/v1/requests/999", nil).status)
}

func TestOverview(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	h.loungeID()

	_, err := h.app.DB().EchoDevices().Seen(t.Context(), "amzn1.ask.device.NEW")
	require.NoError(t, err)

	var o Overview

	c.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, &o)
	assert.Equal(t, "not_deployed", o.Skill.State)
	assert.Equal(t, 1, o.Libraries.Count)
	require.Len(t, o.Instances, 1)

	links := map[string]string{}
	for _, at := range o.Attention {
		links[at.Kind] = at.Link
	}

	assert.Equal(t, "/echo-devices", links["unlinked_device"])
	assert.Equal(t, "/alexa-skill", links["skill_not_deployed"])
}

func TestReachability(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/alexa":
			http.Error(w, "bad request", http.StatusBadRequest)
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(public.Close)

	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.client()

	assert.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": public.URL, "setup_token": "wrong"}).status)

	var res Reachability

	c.json(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": public.URL + "/", "setup_token": h.setupToken()}, http.StatusOK, &res)
	assert.Equal(t, public.URL, res.URL)

	levels := map[string]string{}
	for _, ch := range res.Checks {
		levels[ch.Name] = ch.Level
	}

	assert.Equal(t, map[string]string{"alexa_endpoint": "ok", "certificate": "fail", "admin_exposed": "ok"}, levels, "%+v", res.Checks)
	assert.Contains(t, res.Checks[2].Note, "sign-in", "a reachable admin API passes with a note")

	admin := h.admin()
	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": public.URL, "setup_token": h.setupToken()}).status, "the setup token stops working once the admin exists")
	admin.json(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": public.URL}, http.StatusOK, &res)
	assert.Equal(t, http.StatusUnprocessableEntity, admin.do(http.MethodPost, "/api/v1/setup/reachability", map[string]string{"url": "file:///etc/passwd"}).status)
}
