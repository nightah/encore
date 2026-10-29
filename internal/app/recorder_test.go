package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

func TestRecorderDropsEntriesRatherThanBlock(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		a := newBareApp(t)
		r := newRecorder(a.DB().RequestLog(), slog.New(slog.DiscardHandler))
		start := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

		for i := range recorderBuffer + 5 {
			r.Record(t.Context(), alexa.LogEntry{Time: start.Add(time.Duration(i) * time.Second), RequestID: fmt.Sprintf("req-%d", i), Intent: "WatchMovie"})
		}

		assert.Len(t, r.entries, recorderBuffer, "entries beyond the buffer are dropped")

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			r.run(ctx)
		}()

		settled(t, func() bool {
			entries, err := a.DB().RequestLog().List(t.Context(), store.RequestLogQuery{Limit: 1000})
			return err == nil && len(entries) == recorderBuffer
		})

		cancel()
		<-done

		entries, err := a.DB().RequestLog().List(t.Context(), store.RequestLogQuery{Limit: 1000})
		require.NoError(t, err)

		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.RequestID
		}

		assert.Contains(t, ids, "req-0", "the oldest queued entries are kept")
		assert.NotContains(t, ids, fmt.Sprintf("req-%d", recorderBuffer), "the ones that didn't fit are not")
	})
}

func TestRecorderKeepsGoingWhenTheDatabaseFails(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		a := newBareApp(t)
		r := newRecorder(a.DB().RequestLog(), slog.New(slog.DiscardHandler))

		require.NoError(t, a.DB().Close())

		for i := range 3 {
			r.Record(t.Context(), alexa.LogEntry{RequestID: fmt.Sprintf("req-%d", i)})
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			r.run(ctx)
		}()

		settled(t, func() bool { return len(r.entries) == 0 }, "failed writes don't back the queue up")

		cancel()
		<-done
	})
}

func TestRecorderRow(t *testing.T) {
	failed := errors.New("kodi went away")

	t.Run("a launch has no intent, so its type stands in", func(t *testing.T) {
		row := toRow(alexa.LogEntry{Type: "LaunchRequest"})
		assert.Equal(t, "LaunchRequest", row.Intent)
		assert.Nil(t, row.Match, "nothing was matched")
		assert.Nil(t, row.Slots)
		assert.Nil(t, row.KodiCalls)
		assert.JSONEq(t, `{"verify_ms":0,"match_ms":0,"kodi_ms":0}`, string(row.Timing))
	})

	t.Run("slots are kept as heard", func(t *testing.T) {
		row := toRow(alexa.LogEntry{Intent: "WatchMovie", Slots: map[string]string{"Movie": "hot fuzz"}})

		var slots map[string]string
		require.NoError(t, json.Unmarshal(row.Slots, &slots))
		assert.Equal(t, map[string]string{"Movie": "hot fuzz"}, slots)
	})

	t.Run("an error without a match is still logged", func(t *testing.T) {
		row := toRow(alexa.LogEntry{Intent: "Pause", Target: "Lounge", Err: failed})
		assert.JSONEq(t, `{"target":"Lounge","error":"kodi went away"}`, string(row.Match))
	})

	t.Run("the room name that picked the target is logged with it", func(t *testing.T) {
		row := toRow(alexa.LogEntry{Intent: "RoomControl", Target: "Lounge", Via: actions.Via{Spoken: "TV", Kind: "device", Room: "Lounge"}})
		assert.JSONEq(t, `{"target":"Lounge","via":{"spoken":"TV","kind":"device","room":"Lounge"}}`, string(row.Match))
	})

	t.Run("matching that took longer than the request leaves Kodi nothing", func(t *testing.T) {
		row := toRow(alexa.LogEntry{Duration: 10 * time.Millisecond, MatchTime: 20 * time.Millisecond, KodiCalls: []media.Call{{Method: "Player.Open", Duration: 5 * time.Millisecond}}})
		assert.JSONEq(t, `{"verify_ms":0,"match_ms":20,"kodi_ms":0}`, string(row.Timing))
	})

	t.Run("only the top candidates are logged", func(t *testing.T) {
		res := &match.Result{Normalized: "dune", Decision: match.DecisionAsk, Margin: 0.01}
		for i := range maxLoggedCandidates + 3 {
			res.Candidates = append(res.Candidates, match.Candidate{Doc: match.Doc{Title: fmt.Sprintf("Dune %d", i), Ref: media.Ref{Kind: media.KindMovie, ID: i + 1}}, Score: 0.9})
		}

		row := toRow(alexa.LogEntry{Intent: "WatchMovie", Match: res, Err: failed})

		var m loggedMatch
		require.NoError(t, json.Unmarshal(row.Match, &m))
		assert.Len(t, m.Candidates, maxLoggedCandidates)
		assert.Equal(t, "Dune 0", m.Candidates[0].Title)
		assert.Equal(t, "dune", m.Normalized)
		assert.Equal(t, "ask", m.Decision)
		assert.InDelta(t, 0.01, m.Margin, 1e-9)
		assert.Equal(t, "kodi went away", m.Error)
	})
}

func TestRecorderLogsCandidateRefs(t *testing.T) {
	res := &match.Result{Decision: match.DecisionPlay, Candidates: []match.Candidate{
		{Doc: match.Doc{Title: "Hot Fuzz", Year: 2007, Ref: media.Ref{Kind: media.KindMovie, ID: 4}}, Score: 0.97},
	}}

	row := toRow(alexa.LogEntry{Intent: "WatchMovie", Target: "Lounge", Match: res})
	require.NotNil(t, row.Match)
	assert.Contains(t, string(row.Match), `"kind":"movie","media_id":4`)
}

func TestRecorderLogsCallsAndTiming(t *testing.T) {
	row := toRow(alexa.LogEntry{
		Intent: "WatchMovie", Duration: 300 * time.Millisecond, Verify: 3 * time.Millisecond, MatchTime: 20 * time.Millisecond,
		KodiCalls: []media.Call{{Method: "Player.Open", Duration: 200 * time.Millisecond}, {Method: "Player.GetItem", Duration: 150 * time.Millisecond, Err: io.EOF}},
	})

	assert.JSONEq(t, `[{"method":"Player.Open","ms":200,"ok":true},{"method":"Player.GetItem","ms":150,"ok":false}]`, string(row.KodiCalls))
	assert.JSONEq(t, `{"verify_ms":3,"match_ms":20,"kodi_ms":280}`, string(row.Timing), "Kodi's share is capped at what matching left")
}
