//go:build contract

package app

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
)

// The library in internal/backend/kodi/testdata/kodi-headless/library, which the nightly workflow loads into a headless Kodi.
const seededTitles = 6 // five movies and one show

// contractHarness runs Encore against the real Kodi at ENCORE_CONTRACT_KODI_URL once its library has been seeded (ENCORE_CONTRACT_KODI_LIBRARY), and waits until every seeded title is indexed.
func contractHarness(t *testing.T) *harness {
	t.Helper()

	url := os.Getenv("ENCORE_CONTRACT_KODI_URL")
	if url == "" || os.Getenv("ENCORE_CONTRACT_KODI_LIBRARY") == "" {
		t.Skip("ENCORE_CONTRACT_KODI_URL and ENCORE_CONTRACT_KODI_LIBRARY must be set")
	}

	h := startHarness(t, defaultTuning(), config.KodiInstance{Name: "Lounge", URL: url, WebSocketURL: os.Getenv("ENCORE_CONTRACT_KODI_WS_URL"), Default: true})

	require.Eventually(t, func() bool {
		h.app.mu.RLock()
		defer h.app.mu.RUnlock()

		for _, inst := range h.app.instances {
			return inst.Index.Len() >= seededTitles
		}

		return false
	}, time.Minute, 100*time.Millisecond, "the seeded library never reached the index")

	return h
}

// logged waits for the request log entry of the n-th request and returns its outcome and match.
func logged(t *testing.T, h *harness, n int) (string, loggedMatch) {
	t.Helper()

	var entries []store.RequestLogEntry

	require.Eventually(t, func() bool {
		var err error
		entries, err = h.app.DB().RequestLog().List(t.Context(), store.RequestLogQuery{})

		return err == nil && len(entries) >= n
	}, 5*time.Second, 10*time.Millisecond)

	var m loggedMatch
	if len(entries[0].Match) > 0 {
		require.NoError(t, json.Unmarshal(entries[0].Match, &m))
	}

	return entries[0].Outcome, m
}

// TestContractVoiceAgainstKodi sends Alexa intent requests to Encore, which matches them against what a real Kodi listed and asks that Kodi to play the result. Kodi can't play the seeded files, but it accepts the request, so "played" means Encore chose the item and Kodi took the call.
func TestContractVoiceAgainstKodi(t *testing.T) {
	h := contractHarness(t)

	cases := []struct {
		intent, slot, heard string
		outcome, speech     string
		title               string
		year                int
	}{
		{"WatchMovie", "Movie", "dune two thousand and twenty one", "played", "Playing Dune.", "Dune", 2021},
		{"WatchMovie", "Movie", "dune part two", "played", "Playing Dune: Part Two.", "Dune: Part Two", 2024},
		{"WatchMovie", "Movie", "rocky two", "played", "Playing Rocky II.", "Rocky II", 1979},
		{"WatchMovie", "Movie", "toy story three", "played", "Playing Toy Story 3.", "Toy Story 3", 2010},
		{"WatchNextEpisode", "Show", "severance", "played", "Playing Severance, season 1, episode 1, Good News About Hell.", "Severance", 2022},
	}

	for i, c := range cases {
		t.Run(c.heard, func(t *testing.T) {
			resp := h.say(t, c.intent, c.slot, c.heard)
			assert.Equal(t, c.speech, speech(resp))

			outcome, m := logged(t, h, i+1)
			assert.Equal(t, c.outcome, outcome)
			require.NotEmpty(t, m.Candidates, "%+v", m)
			assert.Equal(t, c.title, m.Candidates[0].Title)
			assert.Equal(t, c.year, m.Candidates[0].Year)
		})
	}

	t.Run("an ambiguous title asks", func(t *testing.T) {
		resp := h.say(t, "WatchMovie", "Movie", "dune")
		assert.Contains(t, speech(resp), "Did you mean")

		outcome, m := logged(t, h, len(cases)+1)
		assert.Equal(t, "asked", outcome)
		assert.Equal(t, "ask", m.Decision)
	})
}
