package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

var _ alexa.Recorder = (*recorder)(nil)

func newRecorder(repo store.RequestLog, log *slog.Logger) *recorder {
	return &recorder{repo: repo, log: log, entries: make(chan alexa.LogEntry, recorderBuffer)}
}

// Recorder returns the request log recorder for the Alexa adapter.
func (a *App) Recorder() alexa.Recorder { return a.recorder }

// Rejections returns the counts of voice requests the Alexa endpoint refused, for VerifiedFunc to add to and the overview to show.
func (a *App) Rejections() *alexa.Rejections { return a.rejections }

// Record queues e for the request log, dropping it if the queue is full.
func (r *recorder) Record(_ context.Context, e alexa.LogEntry) {
	select {
	case r.entries <- e:
	default:
		r.log.Warn("request log queue full; dropping entry", "request_id", e.RequestID)
	}
}

func (r *recorder) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-r.entries:
			row := toRow(e)
			if err := r.repo.Insert(context.WithoutCancel(ctx), &row); err != nil {
				r.log.Warn("writing request log entry", "error", err)
			}
		}
	}
}

func toRow(e alexa.LogEntry) store.RequestLogEntry {
	row := store.RequestLogEntry{
		Time:      e.Time,
		RequestID: e.RequestID,
		DeviceID:  e.DeviceID,
		Locale:    e.Locale,
		Intent:    e.Intent,
		Speech:    e.Speech,
		Outcome:   e.Outcome,
		Duration:  e.Duration,
		Simulated: e.Simulated,
	}

	if row.Intent == "" {
		row.Intent = e.Type
	}

	if len(e.Slots) > 0 {
		row.Slots, _ = json.Marshal(e.Slots)
	}

	if len(e.KodiCalls) > 0 {
		calls := make([]loggedCall, len(e.KodiCalls))
		for i, c := range e.KodiCalls {
			calls[i] = loggedCall{Method: c.Method, MS: c.Duration.Milliseconds(), OK: c.Err == nil}
		}

		row.KodiCalls, _ = json.Marshal(calls)
	}

	// Calls made in parallel would add up to more than the request took, so Kodi's share is capped at what verifying and matching left.
	kodi := min(kodiTime(e.KodiCalls), max(0, e.Duration-e.Verify-e.MatchTime))
	row.Timing, _ = json.Marshal(loggedTiming{VerifyMS: e.Verify.Milliseconds(), MatchMS: e.MatchTime.Milliseconds(), KodiMS: kodi.Milliseconds()})

	if e.Match != nil {
		row.Match, _ = json.Marshal(summariseMatch(e.Match, e.Target, e.Via, e.Err))
	} else if e.Target != "" || e.Err != nil {
		row.Match, _ = json.Marshal(summariseMatch(nil, e.Target, e.Via, e.Err))
	}

	return row
}

func kodiTime(calls []media.Call) time.Duration {
	var total time.Duration
	for _, c := range calls {
		total += c.Duration
	}

	return total
}

func summariseMatch(res *match.Result, target string, via actions.Via, err error) loggedMatch {
	m := loggedMatch{Target: target}

	if via.Spoken != "" {
		m.Via = &loggedVia{Spoken: via.Spoken, Kind: via.Kind, Room: via.Room}
	}

	if err != nil {
		m.Error = err.Error()
	}

	if res == nil {
		return m
	}

	m.Normalized, m.Decision, m.Margin = res.Normalized, res.Decision.String(), res.Margin

	for _, c := range res.Candidates {
		if len(m.Candidates) == maxLoggedCandidates {
			break
		}

		m.Candidates = append(m.Candidates, loggedCandidate{Title: c.Doc.Title, Year: c.Doc.Year, Kind: string(c.Doc.Ref.Kind), MediaID: c.Doc.Ref.ID, Score: c.Score, Why: c.Breakdown})
	}

	return m
}
