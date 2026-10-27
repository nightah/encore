package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/app"
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
