package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

// An entry logged without an outcome is reported as ok, one of the documented outcomes.
func TestSummaryOfEntryWithoutOutcome(t *testing.T) {
	assert.Equal(t, "ok", toSummary(store.RequestLogEntry{Intent: "SessionEndedRequest"}, nil).Outcome)
	assert.Equal(t, "played", toSummary(store.RequestLogEntry{Outcome: "played"}, nil).Outcome)
}

func TestHeard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		slots map[string]string
		want  string
	}{
		{"no slots", nil, ""},
		{"empty slots", map[string]string{"Movie": "", "Room": ""}, ""},
		{"a title slot", map[string]string{"Room": "lounge", "Movie": "hot fuzz"}, "hot fuzz"},
		{"the preferred title slot", map[string]string{"Song": "bohemian rhapsody", "Artist": "queen", "Movie": "dune"}, "dune"},
		{"an empty title slot is skipped", map[string]string{"Movie": "", "Show": "bluey"}, "bluey"},
		{"no title slot falls back by name", map[string]string{"Room": "lounge", "Number": "3"}, "3"},
		{"fallback skips empty values", map[string]string{"Amount": "", "Room": "den"}, "den"},
		{"unicode", map[string]string{"Query": "amélie"}, "amélie"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, heard(tt.slots), tt.name)
	}
}

func TestToSummary(t *testing.T) {
	t.Parallel()

	devs := map[string]store.EchoDevice{"amzn1.ask.device.LOUNGE": {ID: 4, AlexaDeviceID: "amzn1.ask.device.LOUNGE", DisplayName: "Lounge Echo"}}
	unlinked := json.RawMessage(fmt.Sprintf(`{"error":"handling: %s"}`, alexa.ErrUnlinked.Error()))

	tests := []struct {
		name  string
		entry store.RequestLogEntry
		check func(t *testing.T, s RequestSummary)
	}{
		{
			name:  "a known device",
			entry: store.RequestLogEntry{ID: 9, DeviceID: "amzn1.ask.device.LOUNGE", Intent: "WatchMovie", Outcome: "played", Duration: 1500 * time.Microsecond, Slots: json.RawMessage(`{"Movie":"dune"}`), Match: json.RawMessage(`{"target":"Lounge"}`)},
			check: func(t *testing.T, s RequestSummary) {
				require.NotNil(t, s.DeviceID)
				assert.Equal(t, int64(4), *s.DeviceID)
				assert.Equal(t, "Lounge Echo", s.DeviceName)
				assert.Equal(t, "dune", s.Heard)
				assert.Equal(t, "Lounge", s.Target)
				assert.Equal(t, int64(1), s.DurationMS)
			},
		},
		{
			name:  "an unknown device",
			entry: store.RequestLogEntry{DeviceID: "amzn1.ask.device.NEW"},
			check: func(t *testing.T, s RequestSummary) {
				assert.Nil(t, s.DeviceID)
				assert.Empty(t, s.DeviceName)
			},
		},
		{
			name:  "an error from an unlinked device",
			entry: store.RequestLogEntry{Outcome: "error", Match: unlinked},
			check: func(t *testing.T, s RequestSummary) { assert.Equal(t, "unlinked", s.Outcome) },
		},
		{
			name:  "another error",
			entry: store.RequestLogEntry{Outcome: "error", Match: json.RawMessage(`{"error":"kodi: connection refused"}`)},
			check: func(t *testing.T, s RequestSummary) { assert.Equal(t, "error", s.Outcome) },
		},
		{
			name:  "unlinked is only an error's reason",
			entry: store.RequestLogEntry{Outcome: "asked", Match: unlinked},
			check: func(t *testing.T, s RequestSummary) { assert.Equal(t, "asked", s.Outcome) },
		},
		{
			name:  "malformed JSON is ignored",
			entry: store.RequestLogEntry{Outcome: "error", Slots: json.RawMessage(`{"Movie":`), Match: json.RawMessage(`[1,2]`)},
			check: func(t *testing.T, s RequestSummary) {
				assert.Empty(t, s.Heard)
				assert.Empty(t, s.Target)
				assert.Equal(t, "error", s.Outcome)
			},
		},
		{
			name:  "a simulated request",
			entry: store.RequestLogEntry{Simulated: true, Intent: "LaunchRequest"},
			check: func(t *testing.T, s RequestSummary) { assert.True(t, s.Simulated) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, toSummary(tt.entry, devs))
		})
	}
}

func TestToDetail(t *testing.T) {
	t.Parallel()

	full := toDetail(store.RequestLogEntry{
		Slots:     json.RawMessage(`{"Movie":"hot fuzz","Room":"lounge"}`),
		Match:     json.RawMessage(`{"target":"Lounge","normalized":"hot fuzz","decision":"play","margin":0.4,"candidates":[{"title":"Hot Fuzz","year":2007,"kind":"movie","media_id":6,"score":0.98,"breakdown":{"Alias":"hot fuzz","TokenSet":1}}]}`),
		KodiCalls: json.RawMessage(`[{"method":"Player.Open","ms":12,"ok":true}]`),
		Timing:    json.RawMessage(`{"verify_ms":2,"match_ms":3,"kodi_ms":12}`),
		Speech:    "Playing Hot Fuzz.",
	}, nil)

	assert.Equal(t, map[string]string{"Movie": "hot fuzz", "Room": "lounge"}, full.Slots)
	assert.Equal(t, []KodiCall{{Method: "Player.Open", MS: 12, OK: true}}, full.KodiCalls)
	require.NotNil(t, full.Timing.V)
	assert.Equal(t, Timing{VerifyMS: 2, MatchMS: 3, KodiMS: 12}, *full.Timing.V)
	require.NotNil(t, full.Match.V)
	assert.Equal(t, "play", full.Match.V.Decision)
	assert.Equal(t, "Lounge", full.Match.V.Target)
	require.Len(t, full.Match.V.Candidates, 1)
	assert.Equal(t, Candidate{Title: "Hot Fuzz", Year: 2007, Kind: "movie", MediaID: 6, Score: 0.98, Breakdown: Breakdown{Alias: "hot fuzz", TokenSet: 1}}, full.Match.V.Candidates[0])
	assert.Equal(t, "Playing Hot Fuzz.", full.Speech)

	bare := toDetail(store.RequestLogEntry{Intent: "LaunchRequest"}, nil)
	assert.Equal(t, map[string]string{}, bare.Slots, "empty, not null")
	assert.Equal(t, []KodiCall{}, bare.KodiCalls)
	assert.Nil(t, bare.Match.V)
	assert.Nil(t, bare.Timing.V, "requests logged before timing was recorded have none")

	noDecision := toDetail(store.RequestLogEntry{Match: json.RawMessage(`{"normalized":"x","candidates":[]}`), Timing: json.RawMessage(`"slow"`)}, nil)
	assert.Nil(t, noDecision.Match.V, "a match with no decision, error or target says nothing")
	assert.Nil(t, noDecision.Timing.V, "malformed timing is left out")

	onlyErr := toDetail(store.RequestLogEntry{Match: json.RawMessage(`{"error":"no such room"}`)}, nil)
	require.NotNil(t, onlyErr.Match.V)
	assert.Equal(t, "no such room", onlyErr.Match.V.Error)
	assert.Equal(t, []Candidate{}, onlyErr.Match.V.Candidates)
}

func TestMatchResult(t *testing.T) {
	t.Parallel()

	r := match.Result{Normalized: "dune", Decision: match.DecisionAsk, Margin: 0.01}
	for i := range 12 {
		r.Candidates = append(r.Candidates, match.Candidate{Doc: match.Doc{Title: fmt.Sprintf("Dune %d", i), Year: 2000 + i, Ref: media.Ref{Kind: media.KindMovie, ID: i + 1}}, Score: 1 - float64(i)/100, Breakdown: match.Breakdown{TokenSet: 1}})
	}

	m := matchResult(r, "Lounge", errors.New("kodi: connection refused"))
	assert.Equal(t, "dune", m.Normalized)
	assert.Equal(t, "ask", m.Decision)
	assert.InDelta(t, 0.01, m.Margin, 1e-9)
	assert.Equal(t, "Lounge", m.Target)
	assert.Equal(t, "kodi: connection refused", m.Error)
	require.Len(t, m.Candidates, 10, "the top ten are shown")
	assert.Equal(t, Candidate{Title: "Dune 0", Year: 2000, Kind: "movie", MediaID: 1, Score: 1, Breakdown: Breakdown{TokenSet: 1}}, m.Candidates[0])
	assert.Equal(t, "Dune 9", m.Candidates[9].Title)

	empty := matchResult(match.Result{}, "", nil)
	assert.Equal(t, "not_found", empty.Decision)
	assert.Empty(t, empty.Error)
	assert.Equal(t, []Candidate{}, empty.Candidates, "empty, not null")
}
