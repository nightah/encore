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
	testCases := []struct {
		name     string
		have     store.RequestLogEntry
		expected string
	}{
		{name: "ShouldReportOKWithoutAnOutcome", have: store.RequestLogEntry{Intent: "SessionEndedRequest"}, expected: "ok"},
		{name: "ShouldKeepARecordedOutcome", have: store.RequestLogEntry{Outcome: "played"}, expected: "played"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, toSummary(tc.have, nil).Outcome)
		})
	}
}

func TestHeard(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     map[string]string
		expected string
	}{
		{name: "ShouldHearNothingWithoutSlots"},
		{name: "ShouldHearNothingFromEmptySlots", have: map[string]string{"Movie": "", "Room": ""}},
		{name: "ShouldHearATitleSlot", have: map[string]string{"Room": "lounge", "Movie": "hot fuzz"}, expected: "hot fuzz"},
		{name: "ShouldHearThePreferredTitleSlot", have: map[string]string{"Song": "bohemian rhapsody", "Artist": "queen", "Movie": "dune"}, expected: "dune"},
		{name: "ShouldSkipAnEmptyTitleSlot", have: map[string]string{"Movie": "", "Show": "bluey"}, expected: "bluey"},
		{name: "ShouldFallBackByNameWithoutATitleSlot", have: map[string]string{"Room": "lounge", "Number": "3"}, expected: "3"},
		{name: "ShouldSkipEmptyValuesWhenFallingBack", have: map[string]string{"Amount": "", "Room": "den"}, expected: "den"},
		{name: "ShouldHearUnicode", have: map[string]string{"Query": "amélie"}, expected: "amélie"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, heard(tc.have))
		})
	}
}

func TestToSummary(t *testing.T) {
	t.Parallel()

	devs := map[string]store.EchoDevice{"amzn1.ask.device.LOUNGE": {ID: 4, AlexaDeviceID: "amzn1.ask.device.LOUNGE", DisplayName: "Lounge Echo"}}
	unlinked := json.RawMessage(fmt.Sprintf(`{"error":"handling: %s"}`, alexa.ErrUnlinked.Error()))

	testCases := []struct {
		name  string
		have  store.RequestLogEntry
		check func(t *testing.T, s RequestSummary)
	}{
		{name: "ShouldNameAKnownDevice", have: store.RequestLogEntry{ID: 9, DeviceID: "amzn1.ask.device.LOUNGE", Intent: "WatchMovie", Outcome: "played", Duration: 1500 * time.Microsecond, Slots: json.RawMessage(`{"Movie":"dune"}`), Match: json.RawMessage(`{"target":"Lounge"}`)}, check: func(t *testing.T, s RequestSummary) {
			require.NotNil(t, s.DeviceID)
			assert.Equal(t, int64(4), *s.DeviceID)
			assert.Equal(t, "Lounge Echo", s.DeviceName)
			assert.Equal(t, "dune", s.Heard)
			assert.Equal(t, "Lounge", s.Target)
			assert.Equal(t, int64(1), s.DurationMS)
		}},
		{name: "ShouldNotNameAnUnknownDevice", have: store.RequestLogEntry{DeviceID: "amzn1.ask.device.NEW"}, check: func(t *testing.T, s RequestSummary) {
			assert.Nil(t, s.DeviceID)
			assert.Empty(t, s.DeviceName)
		}},
		{name: "ShouldReportAnErrorFromAnUnlinkedDevice", have: store.RequestLogEntry{Outcome: "error", Match: unlinked}, check: func(t *testing.T, s RequestSummary) { assert.Equal(t, "unlinked", s.Outcome) }},
		{name: "ShouldReportAnotherError", have: store.RequestLogEntry{Outcome: "error", Match: json.RawMessage(`{"error":"kodi: connection refused"}`)}, check: func(t *testing.T, s RequestSummary) { assert.Equal(t, "error", s.Outcome) }},
		{name: "ShouldTreatUnlinkedOnlyAsAnErrorsReason", have: store.RequestLogEntry{Outcome: "asked", Match: unlinked}, check: func(t *testing.T, s RequestSummary) { assert.Equal(t, "asked", s.Outcome) }},
		{name: "ShouldIgnoreMalformedJSON", have: store.RequestLogEntry{Outcome: "error", Slots: json.RawMessage(`{"Movie":`), Match: json.RawMessage(`[1,2]`)}, check: func(t *testing.T, s RequestSummary) {
			assert.Empty(t, s.Heard)
			assert.Empty(t, s.Target)
			assert.Equal(t, "error", s.Outcome)
		}},
		{name: "ShouldMarkASimulatedRequest", have: store.RequestLogEntry{Simulated: true, Intent: "LaunchRequest"}, check: func(t *testing.T, s RequestSummary) { assert.True(t, s.Simulated) }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t, toSummary(tc.have, devs))
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

	onlyTarget := toDetail(store.RequestLogEntry{Match: json.RawMessage(`{"target":"Lounge"}`)}, nil)
	require.NotNil(t, onlyTarget.Match.V, "a target alone says where the request went")
	assert.Equal(t, "Lounge", onlyTarget.Match.V.Target)
	assert.Nil(t, onlyTarget.Via.V, "a target the Echo chose itself names no room")

	device := toDetail(store.RequestLogEntry{Match: json.RawMessage(`{"target":"Lounge","via":{"spoken":"TV","kind":"device","room":"Lounge"}}`)}, nil)
	require.NotNil(t, device.Via.V)
	assert.Equal(t, Via{Spoken: "TV", Kind: "device", Room: "Lounge"}, *device.Via.V)
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
