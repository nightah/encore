package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/store"
)

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
