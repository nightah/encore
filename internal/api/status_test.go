package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// cachedStatus waits for the startup poll to cache the Lounge's status, so a test's own probes are the only ones.
func cachedStatus(t *testing.T, h *harness) int64 {
	t.Helper()

	lounge := h.loungeID()

	require.Eventually(t, func() bool {
		h.api.status.mu.Lock()
		defer h.api.status.mu.Unlock()

		e, ok := h.api.status.entries[lounge]

		return ok && e.st.KodiVersion != ""
	}, 10*time.Second, 20*time.Millisecond)

	return lounge
}

func versionCalls(k *fakekodi.Server) int {
	n := 0

	for _, c := range k.CallsTo("Application.GetProperties") {
		if strings.Contains(string(c.Params), `"version"`) {
			n++
		}
	}

	return n
}

func TestStatusAsksForVersionOnlyNowAndThen(t *testing.T) {
	// A clock that moves only when told, so infoEvery's boundary can be hit exactly.
	var skew atomic.Int64

	start := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	h := newHarness(t, harnessOpts{mutate: func(o *Options) {
		o.StatusInterval = time.Hour
		o.Now = func() time.Time { return start.Add(time.Duration(skew.Load())) }
	}})
	lounge := cachedStatus(t, h)
	cache := h.api.status

	asked := versionCalls(h.kodi)

	cache.mu.Lock()
	full := cache.entries[lounge].st
	cache.mu.Unlock()

	st, err := cache.probe(t.Context(), lounge)
	require.NoError(t, err)
	assert.Equal(t, asked, versionCalls(h.kodi), "the version was asked for moments ago")
	assert.Equal(t, full.KodiVersion, st.KodiVersion, "and is kept from the last full probe")
	assert.Equal(t, full.APIVersion, st.APIVersion)
	assert.Equal(t, 4, st.Counts.Episodes, "as is the episode total")

	skew.Add(int64(infoEvery - time.Nanosecond))

	_, err = cache.probe(t.Context(), lounge)
	require.NoError(t, err)
	assert.Equal(t, asked, versionCalls(h.kodi), "not until infoEvery has passed")

	skew.Add(int64(time.Nanosecond))

	st, err = cache.probe(t.Context(), lounge)
	require.NoError(t, err)
	assert.Equal(t, asked+1, versionCalls(h.kodi), "after a while it's asked for again")
	assert.NotEmpty(t, st.KodiVersion)
}

func TestStatusGetReusesAFreshProbe(t *testing.T) {
	h := newHarness(t, harnessOpts{mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	lounge := cachedStatus(t, h)
	cache := h.api.status

	pings := len(h.kodi.CallsTo("JSONRPC.Ping"))

	st, err := cache.get(t.Context(), lounge)
	require.NoError(t, err)
	assert.True(t, st.Reachable)
	assert.Len(t, h.kodi.CallsTo("JSONRPC.Ping"), pings, "a fresh status is served from the cache")

	h.skew.Add(int64(90 * time.Minute))

	_, err = cache.get(t.Context(), lounge)
	require.NoError(t, err)
	assert.Len(t, h.kodi.CallsTo("JSONRPC.Ping"), pings, "a status is fresh for two poll intervals, so a late poll doesn't mean a probe")

	h.skew.Add(int64(30 * time.Minute))

	_, err = cache.get(t.Context(), lounge)
	require.NoError(t, err)
	assert.Len(t, h.kodi.CallsTo("JSONRPC.Ping"), pings+1, "a stale one is probed again")

	cache.forget(lounge)

	_, err = cache.get(t.Context(), lounge)
	require.NoError(t, err)
	assert.Len(t, h.kodi.CallsTo("JSONRPC.Ping"), pings+2, "a forgotten one too")
}

func TestStatusOfUnknownInstance(t *testing.T) {
	h := newHarness(t, harnessOpts{mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	lounge := cachedStatus(t, h)
	cache := h.api.status

	_, err := cache.get(t.Context(), 999)
	require.ErrorIs(t, err, app.ErrNoRuntime)

	all := cache.getAll(t.Context(), []int64{lounge, 999})
	assert.Len(t, all, 1, "an instance without a runtime is left out")
	assert.Contains(t, all, lounge)

	assert.Empty(t, cache.getAll(t.Context(), nil))
}

func TestPublishIfChanged(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	cache := newStatusCache(h.api)

	sub, _ := h.api.events.subscribe(0)
	defer h.api.events.unsubscribe(sub)

	received := func() []event {
		var out []event

		for {
			select {
			case ev := <-sub.ch:
				if ev.Name == "test" {
					out = append(out, ev)
				}
			default:
				return out
			}
		}
	}

	cache.publishIfChanged("test", "a", []string{"x"})
	cache.publishIfChanged("test", "a", []string{"x"})
	cache.publishIfChanged("test", "b", []string{"x"})
	cache.publishIfChanged("test", "a", []string{"y"})
	cache.publishIfChanged("test", "a", []string(nil))
	cache.publishIfChanged("test", "a", []string{})
	cache.publishIfChanged("test", "a", make(chan int))

	evs := received()
	require.Len(t, evs, 4, "unchanged values and values that can't be encoded aren't published")
	assert.JSONEq(t, `["x"]`, string(evs[0].Data))
	assert.JSONEq(t, `["x"]`, string(evs[1].Data), "each key is tracked on its own")
	assert.JSONEq(t, `["y"]`, string(evs[2].Data))
	assert.JSONEq(t, `[]`, string(evs[3].Data), "nil is published as an empty list, and the same as one")
}

func TestPlayerChangeIsIgnoredUntilPolling(t *testing.T) {
	h := newHarness(t, harnessOpts{mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	cache := newStatusCache(h.api) // not running

	cache.playerChanged(1)

	cache.mu.Lock()
	defer cache.mu.Unlock()

	assert.Empty(t, cache.refreshing, "nothing is scheduled before the poll runs")
}

func TestPlayerChangesCoalesce(t *testing.T) {
	h := newHarness(t, harnessOpts{mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	lounge := cachedStatus(t, h)
	cache := newStatusCache(h.api)

	ctx, cancel := context.WithCancel(t.Context())
	cache.ctx = ctx

	// Refreshes are run by hand instead of after playerSettle.
	var scheduled []func()

	cache.afterFunc = func(d time.Duration, f func()) {
		assert.Equal(t, playerSettle, d, "a change is read once Kodi has had time to settle")

		scheduled = append(scheduled, f)
	}

	refreshing := func() bool {
		cache.mu.Lock()
		defer cache.mu.Unlock()

		return cache.refreshing[lounge]
	}

	pings := len(h.kodi.CallsTo("JSONRPC.Ping"))

	for range 5 {
		cache.playerChanged(lounge)
	}

	require.Len(t, scheduled, 1, "a burst of changes schedules one refresh")
	assert.True(t, refreshing())
	assert.Len(t, h.kodi.CallsTo("JSONRPC.Ping"), pings, "nothing is read before it runs")

	scheduled[0]()
	assert.False(t, refreshing(), "a refresh that ran makes way for the next")
	assert.Len(t, h.kodi.CallsTo("JSONRPC.Ping"), pings+1, "a burst of changes is read once")

	cache.playerChanged(lounge)
	require.Len(t, scheduled, 2, "a change after the refresh schedules another")

	// After the poll stops, a scheduled refresh does nothing.
	cancel()
	scheduled[1]()
	assert.False(t, refreshing())
	assert.Len(t, h.kodi.CallsTo("JSONRPC.Ping"), pings+1)
}

// A full probe that finds Kodi unreachable learns nothing, so the version from the last one that did is kept.
func TestStatusKeepsTheVersionWhileKodiIsUnreachable(t *testing.T) {
	h := newHarness(t, harnessOpts{mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	lounge := cachedStatus(t, h)
	cache := h.api.status

	cache.mu.Lock()
	known := cache.entries[lounge].st.KodiVersion
	cache.mu.Unlock()

	h.kodi.Handle("JSONRPC.Ping", func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: -32100, Message: "busy"}
	})
	h.skew.Add(int64(infoEvery))

	st, err := cache.probe(t.Context(), lounge)
	require.NoError(t, err)
	require.False(t, st.Reachable)
	assert.Equal(t, known, st.KodiVersion)

	asked := versionCalls(h.kodi)

	h.kodi.Handle("JSONRPC.Ping", func(json.RawMessage) (any, *fakekodi.Error) { return "pong", nil })

	st, err = cache.probe(t.Context(), lounge)
	require.NoError(t, err)
	assert.True(t, st.Reachable)
	assert.Equal(t, asked+1, versionCalls(h.kodi), "the version is asked for as soon as Kodi answers again")
}

// A probe already under way when an instance is forgotten (its runtime replaced or removed) must not put the old status back.
func TestStatusProbeOutlivedByForget(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	c := h.admin()
	cache := h.api.status
	den := fakekodi.New(t, fakekodi.Options{})

	var created Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Den", "url": den.URL(), "websocket_url": "ws://127.0.0.1:1/jsonrpc"}, http.StatusCreated, &created)
	cache.forget(created.ID)

	held, release := make(chan struct{}), make(chan struct{})

	var first atomic.Bool

	den.Handle("Player.GetActivePlayers", func(json.RawMessage) (any, *fakekodi.Error) {
		if first.CompareAndSwap(false, true) {
			close(held)
			<-release
		}

		return []any{}, nil
	})

	done := make(chan error, 1)

	go func() {
		_, err := cache.probe(t.Context(), created.ID)
		done <- err
	}()

	<-held
	cache.forget(created.ID)
	close(release)
	require.NoError(t, <-done)

	cache.mu.Lock()
	_, cached := cache.entries[created.ID]
	cache.mu.Unlock()

	assert.False(t, cached, "the forgotten instance's old status isn't cached")
}
