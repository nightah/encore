package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/store"
)

func TestRequestFilters(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	ctx := t.Context()

	den, err := h.app.DB().EchoDevices().Seen(ctx, "amzn1.ask.device.DEN")
	require.NoError(t, err)

	entries := []store.RequestLogEntry{
		{DeviceID: "amzn1.ask.device.DEN", Intent: "LaunchRequest"},
		{DeviceID: "amzn1.ask.device.DEN", Intent: "WatchMovie", Slots: json.RawMessage(`{"Movie":"amélie"}`), Speech: "Playing Amélie in the Den.", Outcome: "played"},
		{DeviceID: "amzn1.ask.device.KITCHEN", Intent: "WatchShow", Slots: json.RawMessage(`{"Show":"bluey"}`), Outcome: "not_found"},
		{DeviceID: "amzn1.ask.device.KITCHEN", Intent: "WatchShow", Slots: json.RawMessage(`{"Show":"severance"}`), Outcome: "played"},
	}

	for i := range entries {
		require.NoError(t, h.app.DB().RequestLog().Insert(ctx, &entries[i]))
	}

	ids := func(q string) []int64 {
		var page RequestPage

		c.json(http.MethodGet, "/api/v1/requests"+q, nil, http.StatusOK, &page)

		out := []int64{}
		for _, s := range page.Items {
			out = append(out, s.ID)
		}

		return out
	}

	assert.Equal(t, []int64{entries[1].ID}, ids("?q=AMÉLIE"), "search ignores case")
	assert.Equal(t, []int64{entries[1].ID}, ids("?q=+in+the+den+"), "and searches what Alexa said, trimmed")
	assert.Equal(t, []int64{entries[3].ID, entries[2].ID}, ids("?q=watchshow"), "and the intent")
	assert.Equal(t, []int64{entries[0].ID}, ids("?outcome=ok"), "an entry without an outcome counts as ok")
	assert.Equal(t, []int64{entries[3].ID, entries[1].ID}, ids("?outcome=played"))
	assert.Equal(t, []int64{entries[1].ID, entries[0].ID}, ids(fmt.Sprintf("?device=%d", den.ID)))
	assert.Equal(t, []int64{}, ids("?device=999"))
	assert.Equal(t, []int64{entries[1].ID}, ids(fmt.Sprintf("?device=%d&outcome=played", den.ID)), "filters combine")
	assert.Equal(t, []int64{}, ids(fmt.Sprintf("?before=%d", entries[0].ID)), "nothing is older than the first")

	var page RequestPage

	c.json(http.MethodGet, "/api/v1/requests?outcome=played&limit=2", nil, http.StatusOK, &page)
	assert.Len(t, page.Items, 2)
	assert.Nil(t, page.NextBefore, "exactly a page of matches leaves no next page")

	c.json(http.MethodGet, "/api/v1/requests?outcome=played&limit=1", nil, http.StatusOK, &page)
	require.NotNil(t, page.NextBefore)
	assert.Equal(t, entries[3].ID, *page.NextBefore, "the next page starts after the last one shown")

	for _, q := range []string{"?limit=0", "?limit=201", "?outcome=bogus", "?before=x"} {
		assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, "/api/v1/requests"+q, nil).status, q)
	}
}

func TestRequestDetailCandidatesWithoutAKnownTarget(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	h.loungeID()

	entries := []store.RequestLogEntry{
		{DeviceID: "amzn1.ask.device.X", Intent: "WatchMovie", Match: logMatch(t, "Attic", "play")},
		{DeviceID: "amzn1.ask.device.X", Intent: "WatchMovie", Match: json.RawMessage(`{"target":"Lounge","decision":"play","candidates":[{"title":"Hot Fuzz","year":2007,"kind":"movie","media_id":99}]}`)},
		{DeviceID: "amzn1.ask.device.X", Intent: "WatchMovie", Match: json.RawMessage(`{"target":"Lounge","decision":"ask","candidates":[{"title":"Hot Fuzz","year":1999}]}`)},
	}

	for i := range entries {
		require.NoError(t, h.app.DB().RequestLog().Insert(t.Context(), &entries[i]))
	}

	var d RequestDetail

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/requests/%d", entries[0].ID), nil, http.StatusOK, &d)
	require.NotNil(t, d.Match.V)
	assert.Empty(t, d.Match.V.Candidates[0].Kind, "an instance that no longer exists can't fill in the title")

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/requests/%d", entries[1].ID), nil, http.StatusOK, &d)
	assert.Equal(t, 99, d.Match.V.Candidates[0].MediaID, "a logged reference is kept as it was")

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/requests/%d", entries[2].ID), nil, http.StatusOK, &d)
	assert.Empty(t, d.Match.V.Candidates[0].Kind, "a title must match its year too")
}

func TestRequestEventsUpdateTheDevice(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	dev, err := h.app.DB().EchoDevices().Seen(t.Context(), "amzn1.ask.device.DEN")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, resp := c.stream(ctx, "")
	defer resp.Body.Close()

	next(t, ch, "comment")

	require.NoError(t, h.app.DB().RequestLog().Insert(t.Context(), &store.RequestLogEntry{DeviceID: "amzn1.ask.device.DEN", Intent: "WatchMovie", Outcome: "played"}))

	var s RequestSummary

	require.NoError(t, json.Unmarshal([]byte(next(t, ch, "request").data), &s))
	require.NotNil(t, s.DeviceID)
	assert.Equal(t, dev.ID, *s.DeviceID)

	var d Device

	require.NoError(t, json.Unmarshal([]byte(next(t, ch, "device").data), &d))
	assert.Equal(t, dev.ID, d.ID)
	assert.Equal(t, 1, d.RequestCountToday, "the device's count today follows the new request")
}

func TestReplayLaunch(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	h.loungeID()

	e := store.RequestLogEntry{DeviceID: "amzn1.ask.device.X", Locale: "en-AU", Intent: "LaunchRequest", Outcome: "ok"}
	require.NoError(t, h.app.DB().RequestLog().Insert(t.Context(), &e))

	var r Replay

	c.json(http.MethodPost, fmt.Sprintf("/api/v1/requests/%d/replay", e.ID), nil, http.StatusOK, &r)
	assert.NotEmpty(t, r.Speech, "the welcome is spoken again")
	assert.Nil(t, r.Match.V, "nothing was matched")
	assert.Equal(t, []WouldCall{}, r.WouldCall)

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPost, "/api/v1/requests/999/replay", nil).status)
}

func TestReplayUnavailableWithoutSpeech(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.Speech = nil }})
	c := h.admin()

	e := store.RequestLogEntry{DeviceID: "amzn1.ask.device.X", Intent: "LaunchRequest"}
	require.NoError(t, h.app.DB().RequestLog().Insert(t.Context(), &e))

	p := c.do(http.MethodPost, fmt.Sprintf("/api/v1/requests/%d/replay", e.ID), nil).problem(t)
	assert.Equal(t, http.StatusServiceUnavailable, p.Status)
}

func TestSimulatedRequests(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	repo := h.app.DB().RequestLog()

	require.NoError(t, repo.Insert(t.Context(), &store.RequestLogEntry{RequestID: "sim", DeviceID: "amzn1.ask.device.SIM", Intent: "LaunchRequest", Outcome: "ok", Simulated: true}))
	require.NoError(t, repo.Insert(t.Context(), &store.RequestLogEntry{RequestID: "echo", DeviceID: "amzn1.ask.device.ECHO", Intent: "LaunchRequest", Outcome: "ok"}))

	var all, echo RequestPage

	c.json(http.MethodGet, "/api/v1/requests", nil, http.StatusOK, &all)
	require.Len(t, all.Items, 2)
	assert.True(t, all.Items[1].Simulated, "the log keeps them, marked")

	c.json(http.MethodGet, "/api/v1/requests?echo=true", nil, http.StatusOK, &echo)
	require.Len(t, echo.Items, 1)
	assert.False(t, echo.Items[0].Simulated)

	var ov Overview

	c.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, &ov)
	assert.Equal(t, 1, ov.RequestsToday.Total, "the simulator's requests don't count")
	assert.Len(t, ov.RecentRequests, 1)
}
