package app

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

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
