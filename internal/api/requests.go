package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/store"
)

func (a *API) registerRequests(api huma.API) {
	huma.Register(api, protected(huma.Operation{OperationID: "listRequests", Method: http.MethodGet, Path: Prefix + "/requests", Tags: []string{"requests"}, Summary: "List handled Alexa requests"}),
		func(ctx context.Context, in *struct {
			Limit   int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
			Before  int64  `query:"before" doc:"Only requests older than this ID"`
			Device  int64  `query:"device" doc:"Only requests from this Echo device ID"`
			Outcome string `query:"outcome" enum:",played,asked,not_found,unlinked,error,ok"`
			Q       string `query:"q" doc:"Search intent, heard text, speech and target"`
			Echo    bool   `query:"echo" doc:"Only requests from an Echo, leaving out the simulator's during deploy smoke tests"`
		},
		) (*body[RequestPage], error) {
			page, err := a.listRequests(ctx, in.Limit, in.Before, in.Device, in.Outcome, in.Q, in.Echo)

			return &body[RequestPage]{Body: page}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "getRequest", Method: http.MethodGet, Path: Prefix + "/requests/{id}", Tags: []string{"requests"}, Summary: "Get one request with its match details"}),
		func(ctx context.Context, in *idPath) (*body[RequestDetail], error) {
			e, err := a.app.DB().RequestLog().Get(ctx, in.ID)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			devs, err := a.deviceIndex(ctx)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			d := toDetail(e, devs)
			a.fillRefs(ctx, &d)

			return &body[RequestDetail]{Body: d}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "replayRequest", Method: http.MethodPost, Path: Prefix + "/requests/{id}/replay", Tags: []string{"requests"}, Summary: "Replay a request against the current index without touching Kodi"}),
		func(ctx context.Context, in *idPath) (*body[Replay], error) {
			e, err := a.app.DB().RequestLog().Get(ctx, in.ID)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			r, err := a.replay(ctx, e)

			return &body[Replay]{Body: r}, a.problemFor(ctx, err)
		})
}

func (a *API) deviceIndex(ctx context.Context) (map[string]store.EchoDevice, error) {
	devs, err := a.app.DB().EchoDevices().List(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]store.EchoDevice, len(devs))
	for _, d := range devs {
		out[d.AlexaDeviceID] = d
	}

	return out, nil
}

func (a *API) listRequests(ctx context.Context, limit int, before, device int64, outcome, q string, echoOnly bool) (RequestPage, error) {
	devs, err := a.deviceIndex(ctx)
	if err != nil {
		return RequestPage{}, err
	}

	entries, err := a.app.DB().RequestLog().List(ctx, store.RequestLogQuery{Limit: requestScan, BeforeID: before})
	if err != nil {
		return RequestPage{}, err
	}

	q = strings.ToLower(strings.TrimSpace(q))
	page := RequestPage{Items: []RequestSummary{}}

	for _, e := range entries {
		s := toSummary(e, devs)

		switch {
		case echoOnly && e.Simulated:
			continue
		case device != 0 && (s.DeviceID == nil || *s.DeviceID != device):
			continue
		case outcome != "" && s.Outcome != outcome:
			continue
		case q != "" && !strings.Contains(strings.ToLower(strings.Join([]string{s.Intent, s.Heard, s.Target, s.DeviceName, e.Speech}, "\x00")), q):
			continue
		}

		if len(page.Items) == limit {
			last := page.Items[len(page.Items)-1].ID
			page.NextBefore = &last

			break
		}

		page.Items = append(page.Items, s)
	}

	return page, nil
}

// titleSlots are the slots holding what the user asked for, in order of preference.
var titleSlots = []string{"Movie", "Show", "Query", "Title", "Artist", "Album", "Song", "Playlist"}

func heard(slots map[string]string) string {
	for _, name := range titleSlots {
		if v := slots[name]; v != "" {
			return v
		}
	}

	names := make([]string, 0, len(slots))
	for k := range slots {
		names = append(names, k)
	}

	sort.Strings(names)

	for _, k := range names {
		if slots[k] != "" {
			return slots[k]
		}
	}

	return ""
}

func decodeMatch(raw json.RawMessage) (loggedMatch, bool) {
	var m loggedMatch
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return loggedMatch{}, false
	}

	return m, true
}

func toSummary(e store.RequestLogEntry, devs map[string]store.EchoDevice) RequestSummary {
	s := RequestSummary{
		ID: e.ID, Time: e.Time, Locale: e.Locale, Intent: e.Intent, Outcome: e.Outcome, DurationMS: e.Duration.Milliseconds(),
		Simulated: e.Simulated,
	}

	// An entry logged without an outcome (a session end or launch) is reported as ok.
	if s.Outcome == "" {
		s.Outcome = "ok"
	}

	if d, ok := devs[e.DeviceID]; ok {
		s.DeviceID = &d.ID
		s.DeviceName = d.DisplayName
	}

	var slots map[string]string
	if len(e.Slots) > 0 {
		_ = json.Unmarshal(e.Slots, &slots)
	}

	s.Heard = heard(slots)

	if m, ok := decodeMatch(e.Match); ok {
		s.Target = m.Target

		if s.Outcome == "error" && strings.Contains(m.Error, alexa.ErrUnlinked.Error()) {
			s.Outcome = "unlinked"
		}
	}

	return s
}

func toMatch(m loggedMatch) Match {
	out := Match{Normalized: m.Normalized, Decision: m.Decision, Margin: m.Margin, Target: m.Target, Error: m.Error, Candidates: []Candidate{}}

	for _, c := range m.Candidates {
		out.Candidates = append(out.Candidates, Candidate{Title: c.Title, Year: c.Year, Kind: c.Kind, MediaID: c.MediaID, Score: c.Score, Breakdown: toBreakdown(c.Why)})
	}

	return out
}

func toBreakdown(b match.Breakdown) Breakdown {
	return Breakdown{Alias: b.Alias, TokenSet: b.TokenSet, JaroWinkler: b.JaroWinkler, OSA: b.OSA, Phonetic: b.Phonetic, Boost: b.Boost}
}

func toDetail(e store.RequestLogEntry, devs map[string]store.EchoDevice) RequestDetail {
	d := RequestDetail{RequestSummary: toSummary(e, devs), Slots: map[string]string{}, KodiCalls: []KodiCall{}, Speech: e.Speech}

	if len(e.Slots) > 0 {
		_ = json.Unmarshal(e.Slots, &d.Slots)
	}

	m, ok := decodeMatch(e.Match)
	if ok && (m.Decision != "" || m.Error != "" || m.Target != "") {
		d.Match = Some(toMatch(m))
	}

	if m.Via != nil {
		d.Via = Some(*m.Via)
	}

	if len(e.KodiCalls) > 0 {
		_ = json.Unmarshal(e.KodiCalls, &d.KodiCalls)
	}

	var t Timing
	if len(e.Timing) > 0 && json.Unmarshal(e.Timing, &t) == nil {
		d.Timing = Some(t)
	}

	return d
}

// fillRefs finds kind and media_id for logged candidates that lack them, by looking each title up in the target instance's index. Entries recorded before Encore logged them lack them.
func (a *API) fillRefs(ctx context.Context, d *RequestDetail) {
	m := d.Match.V
	if m == nil || m.Target == "" {
		return
	}

	recs, err := a.app.DB().KodiInstances().List(ctx)
	if err != nil {
		return
	}

	i := slices.IndexFunc(recs, func(r store.KodiInstance) bool { return r.Name == m.Target })
	if i < 0 {
		return
	}

	for j := range m.Candidates {
		c := &m.Candidates[j]
		if c.Kind != "" {
			continue
		}

		res, err := a.app.Search(ctx, recs[i].ID, match.Query{Text: c.Title, Limit: 5})
		if err != nil {
			return
		}

		for _, cand := range res.Candidates {
			if cand.Doc.Title == c.Title && cand.Doc.Year == c.Year {
				c.Kind, c.MediaID = string(cand.Doc.Ref.Kind), cand.Doc.Ref.ID

				break
			}
		}
	}
}

// requestsToday returns today's log entries, today being the configured time zone's calendar day.
func (a *API) requestsToday(ctx context.Context) ([]store.RequestLogEntry, error) {
	entries, err := a.app.DB().RequestLog().List(ctx, store.RequestLogQuery{Limit: requestScan})
	if err != nil {
		return nil, err
	}

	start := a.todayStart(ctx)
	cut := slices.IndexFunc(entries, func(e store.RequestLogEntry) bool { return e.Time.Before(start) })

	if cut >= 0 {
		entries = entries[:cut]
	}

	return entries, nil
}

func (a *API) todayStart(ctx context.Context) time.Time {
	loc := time.Local

	if l, err := a.localeSettings(ctx); err == nil && l.Timezone != "" {
		if z, err := time.LoadLocation(l.Timezone); err == nil {
			loc = z
		}
	}

	now := a.now().In(loc)

	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
}

// replay runs a logged request through the voice handler again with a target whose commands are recorded instead of sent.
func (a *API) replay(ctx context.Context, e store.RequestLogEntry) (Replay, error) {
	if a.opts.Speech == nil || a.opts.Actions == nil {
		return Replay{}, huma.Error503ServiceUnavailable("replay is not available in this build")
	}

	var slots map[string]string
	if len(e.Slots) > 0 {
		_ = json.Unmarshal(e.Slots, &slots)
	}

	env := alexa.RequestEnvelope{Version: "1.0"}
	env.Session.New = true
	env.Session.SessionID = "replay"
	env.Context.System.Device.DeviceID = e.DeviceID
	env.Request.RequestID = "replay-" + itoa(e.ID)
	env.Request.Locale = e.Locale
	env.Request.Timestamp = a.now().UTC().Format(time.RFC3339)

	switch e.Intent {
	case alexa.TypeLaunch, alexa.TypeSessionEnded:
		env.Request.Type = e.Intent
	default:
		env.Request.Type = alexa.TypeIntent
		env.Request.Intent.Name = e.Intent
		env.Request.Intent.Slots = map[string]alexa.Slot{}

		for k, v := range slots {
			env.Request.Intent.Slots[k] = alexa.Slot{Name: k, Value: v}
		}
	}

	body, err := json.Marshal(env)
	if err != nil {
		return Replay{}, err
	}

	run := &app.DryRun{}
	rec := &captureRecorder{}
	h := &alexa.Handler{
		Service:  a.opts.Actions,
		Speech:   a.opts.Speech,
		Targets:  app.DryRunTargets{App: a.app, Run: run},
		Recorder: rec,
		Log:      a.log,
	}

	if a.opts.Sources != nil {
		h.IntentContent = a.opts.Sources.Model.IntentContent()
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/replay", nil)
	h.ServeBody(w, req, body)

	out := Replay{Speech: rec.entry.Speech, WouldCall: []WouldCall{}}

	if m := rec.entry.Match; m != nil {
		out.Match = Some(matchResult(*m, rec.entry.Target, rec.entry.Err))
	}

	for _, c := range run.Calls() {
		out.WouldCall = append(out.WouldCall, WouldCall{Method: c.Method, Params: c.Params})
	}

	return out, nil
}

func matchResult(r match.Result, target string, err error) Match {
	m := Match{Normalized: r.Normalized, Decision: r.Decision.String(), Margin: r.Margin, Target: target, Candidates: []Candidate{}}

	if err != nil {
		m.Error = err.Error()
	}

	for i, c := range r.Candidates {
		if i == 10 {
			break
		}

		m.Candidates = append(m.Candidates, Candidate{
			Title: c.Doc.Title, Year: c.Doc.Year, Kind: string(c.Doc.Ref.Kind), MediaID: c.Doc.Ref.ID, Score: c.Score, Breakdown: toBreakdown(c.Breakdown),
		})
	}

	return m
}

func (c *captureRecorder) Record(_ context.Context, e alexa.LogEntry) { c.entry = e }

func (a *API) watchRequests(ctx context.Context) {
	entries := make(chan store.RequestLogEntry, 64)

	cancel := a.app.DB().RequestLog().Subscribe(func(e store.RequestLogEntry) {
		select {
		case entries <- e:
		default:
			a.log.Debug("event stream behind; dropping a request event", "id", e.ID)
		}
	})
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case e := <-entries:
			devs, err := a.deviceIndex(ctx)
			if err != nil {
				continue
			}

			a.events.publish("request", toSummary(e, devs))

			if d, ok := devs[e.DeviceID]; ok {
				if dev, err := a.device(ctx, d.ID); err == nil {
					a.events.publish("device", dev)
				}
			}
		}
	}
}
