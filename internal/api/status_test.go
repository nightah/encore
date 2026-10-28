package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
)

// The first poll can finish before the initial library sync; while its status is cached, the instance must still report the synced library.
func TestStatusCacheShowsCurrentLibrary(t *testing.T) {
	// Polling every hour keeps the cached status fresh for the whole test.
	h := newHarness(t, harnessOpts{mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	c := h.admin()
	lounge := h.loungeID()
	cache := h.api.status

	// Wait for the startup poll so it can't overwrite the status cached below.
	require.Eventually(t, func() bool {
		cache.mu.Lock()
		defer cache.mu.Unlock()

		_, ok := cache.entries[lounge]

		return ok
	}, 10*time.Second, 20*time.Millisecond)

	cache.mu.Lock()
	e := cache.entries[lounge]
	e.st.Counts, e.st.LastSyncAt, e.st.NextSyncAt, e.st.Newest = app.LibraryCounts{Episodes: e.st.Counts.Episodes}, time.Time{}, time.Time{}, nil
	e.at = h.api.now()
	cache.entries[lounge] = e
	cache.mu.Unlock()

	var inst Instance

	c.json(http.MethodGet, "/api/v1/kodi/instances/"+itoa(lounge), nil, http.StatusOK, &inst)

	assert.Equal(t, 7, inst.Library.Movies)
	assert.Equal(t, 4, inst.Library.Shows)
	assert.NotNil(t, inst.Status.LastSyncAt)
	require.NotNil(t, inst.Library.NewestTitle.V)
	assert.Equal(t, "Conclave", inst.Library.NewestTitle.V.Title)
}

// A probe of the runtime a save replaced can finish after the new runtime's; the cache keeps the newer status rather than going back to the old one.
func TestStatusCacheKeepsTheNewestProbe(t *testing.T) {
	h := newHarness(t, harnessOpts{mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	c := h.admin()
	cache := h.api.status
	den := fakekodi.New(t, fakekodi.Options{})

	var created Instance

	// Kodi's usual WebSocket port, where nothing listens here, so the instance starts HTTP only, as the setup wizard leaves it.
	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Den", "url": den.URL(), "websocket_url": "ws://127.0.0.1:1/jsonrpc"}, http.StatusCreated, &created)

	held, release := make(chan struct{}), make(chan struct{})

	var first atomic.Bool

	den.Handle("Player.GetActivePlayers", func(json.RawMessage) (any, *fakekodi.Error) {
		if first.CompareAndSwap(false, true) {
			close(held)
			<-release
		}

		return []any{}, nil
	})

	stale := make(chan app.InstanceStatus, 1)

	go func() {
		st, _ := cache.probe(t.Context(), created.ID)
		stale <- st
	}()

	<-held

	// Saving the right WebSocket URL replaces the runtime, which connects.
	c.json(http.MethodPatch, fmt.Sprintf("/api/v1/kodi/instances/%d", created.ID), map[string]string{"websocket_url": den.WebSocketURL()}, http.StatusOK, nil)
	require.Eventually(t, func() bool {
		st, err := cache.probe(t.Context(), created.ID)

		return err == nil && st.Connected
	}, 10*time.Second, 20*time.Millisecond)

	// The held probe asked the replaced runtime, which had no WebSocket, and finishes last.
	close(release)
	assert.True(t, (<-stale).Connected, "a probe that finishes after a newer one reports the newer status")

	st, err := cache.get(t.Context(), created.ID)
	require.NoError(t, err)
	assert.True(t, st.Connected, "and doesn't replace it in the cache")
}
