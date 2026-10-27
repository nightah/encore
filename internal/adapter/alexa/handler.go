package alexa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// ServeBody handles a verified request body. It is an alexa.BodyHandler.
func (h *Handler) ServeBody(w http.ResponseWriter, r *http.Request, body []byte) {
	start := h.now()

	var env RequestEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		h.Log.WarnContext(r.Context(), "undecodable Alexa request", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}

	entry := LogEntry{
		Time:      start,
		RequestID: env.Request.RequestID,
		DeviceID:  env.Context.System.Device.DeviceID,
		Locale:    env.Request.Locale,
		Type:      env.Request.Type,
		Intent:    env.Request.Intent.Name,
		Slots:     slotValues(env.Request.Intent.Slots),
		Verify:    verifyTime(r.Context()),
		Simulated: h.Simulating != nil && h.Simulating(),
	}

	var (
		callsMu sync.Mutex
		calls   []media.Call
	)

	ctx := media.ObserveCalls(r.Context(), func(c media.Call) {
		callsMu.Lock()
		calls = append(calls, c)
		callsMu.Unlock()
	})

	resp, outcome := h.handle(ctx, &env, &entry)

	// Work the action left running (a progressive response, a slow wake) may still report calls; the log keeps those made so far.
	callsMu.Lock()
	entry.KodiCalls = slices.Clone(calls)
	callsMu.Unlock()

	if outcome != nil {
		entry.Match, entry.Err = outcome.Match, outcome.Err
		entry.Outcome = describeOutcome(*outcome)
	} else {
		// Session ends and launches have no action outcome; they were handled fine.
		entry.Outcome = "ok"
	}

	if resp.Response.OutputSpeech != nil {
		entry.Speech = resp.Response.OutputSpeech.Text
	}

	entry.Duration = h.now().Sub(start)

	if h.Recorder != nil {
		h.Recorder.Record(r.Context(), entry)
	}

	if entry.Err != nil {
		h.Log.WarnContext(r.Context(), "action failed", "intent", entry.Intent, "target", entry.Target, "error", entry.Err)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}

	return time.Now()
}

func (h *Handler) handle(ctx context.Context, env *RequestEnvelope, entry *LogEntry) (ResponseEnvelope, *actions.Outcome) {
	locale := env.Request.Locale

	switch env.Request.Type {
	case TypeSessionEnded:
		return ResponseEnvelope{Version: "1.0"}, nil
	case TypeLaunch:
		welcome := actions.Message{ID: "launch.welcome"}
		help := helpMessage()

		return h.respond(locale, actions.Outcome{Say: welcome, Reprompt: &help}, false), nil
	case TypeIntent:
	default:
		return ResponseEnvelope{Version: "1.0"}, nil
	}

	intent := env.Request.Intent
	followup := sessionFollowupOf(env.Session.Attributes)

	// Intents that don't need Kodi.
	switch intent.Name {
	case "AMAZON.HelpIntent":
		msg := helpMessage()

		return h.respond(locale, actions.Outcome{Say: msg, Reprompt: &msg}, false), nil
	case "AMAZON.CancelIntent":
		return h.respond(locale, actions.Outcome{Say: actions.Message{ID: "match.cancelled"}, EndSession: true}, false), nil
	case "AMAZON.NoIntent":
		if followup == nil || followup.Recommend == nil {
			return h.respond(locale, actions.Outcome{Say: actions.Message{ID: "match.cancelled"}, EndSession: true}, false), nil
		}
	case "AMAZON.FallbackIntent", "AMAZON.NavigateHomeIntent":
		msg := fallbackMessage()

		return h.respond(locale, actions.Outcome{Say: msg, Reprompt: &msg}, false), nil
	case "IdentifyEcho":
		o := h.identify(ctx, entry.DeviceID)

		return h.respond(locale, o, false), &o
	}

	var (
		target actions.Target
		err    error
	)

	if room := strings.TrimSpace(intent.Slots["Room"].Value); room != "" {
		target, err = h.Targets.TargetForRoom(ctx, entry.DeviceID, room)
	} else {
		target, err = h.Targets.Target(ctx, entry.DeviceID)
	}

	if err != nil {
		o := actions.Outcome{Say: actions.Message{ID: "error.generic"}, EndSession: true, Err: err}

		switch {
		case errors.Is(err, ErrUnlinked):
			o.Say = actions.Message{ID: "error.device_unlinked"}
		case errors.Is(err, ErrUnknownRoom):
			o.Say = actions.Message{ID: "error.unknown_room", Params: map[string]string{"room": intent.Slots["Room"].Value}}
		}

		return h.respond(locale, o, false), &o
	}

	entry.Target = target.Name

	var timed *timedMatcher

	if target.Matcher != nil {
		timed = &timedMatcher{Matcher: target.Matcher}
		target.Matcher = timed
	}

	o := h.dispatch(ctx, env, target, intent)

	if timed != nil {
		entry.MatchTime = time.Duration(timed.total.Load())
	}

	return h.respond(locale, o, target.ConfirmSound), &o
}

// respond renders an outcome as an Alexa response. With confirm, a success that has nothing to say plays ConfirmSoundURL instead of answering with silence.
func (h *Handler) respond(locale string, o actions.Outcome, confirm bool) ResponseEnvelope {
	env := ResponseEnvelope{Version: "1.0"}

	if confirm && o.Confirm && h.Speech.Render(locale, o.Say) == "" {
		env.Response.OutputSpeech = &OutputSpeech{Type: "SSML", SSML: `<speak><audio src="` + ConfirmSoundURL + `"/></speak>`}
	}

	if text := h.Speech.Render(locale, o.Say); text != "" {
		env.Response.OutputSpeech = &OutputSpeech{Type: "PlainText", Text: text}

		if o.Played != nil {
			env.Response.Card = &Card{Type: "Simple", Title: "Encore", Content: text}
		}
	}

	if o.Reprompt != nil {
		if text := h.Speech.Render(locale, *o.Reprompt); text != "" {
			env.Response.Reprompt = &Reprompt{OutputSpeech: OutputSpeech{Type: "PlainText", Text: text}}
		}
	}

	end := o.EndSession
	env.Response.ShouldEndSession = &end

	if len(o.Pending) > 0 || o.Followup != nil {
		env.SessionAttributes = map[string]any{}

		if len(o.Pending) > 0 {
			env.SessionAttributes[sessionPending] = o.Pending
		}

		if o.Followup != nil {
			env.SessionAttributes[sessionFollowup] = o.Followup
		}
	}

	return env
}

func (m *timedMatcher) Search(ctx context.Context, q match.Query) (match.Result, error) {
	start := time.Now()
	res, err := m.Matcher.Search(ctx, q)
	m.total.Add(int64(time.Since(start)))

	return res, err
}

// identify answers "which Echo is this?" with the device's name in Encore, or for an unnamed device the end of its ID, which the Echo devices page shows too.
func (h *Handler) identify(ctx context.Context, deviceID string) actions.Outcome {
	name, err := h.Targets.DeviceName(ctx, deviceID)
	if err != nil {
		return actions.Outcome{Say: actions.Message{ID: "error.generic"}, EndSession: true, Err: err}
	}

	if name != "" {
		return actions.Outcome{Say: actions.Message{ID: "device.identify_named", Params: map[string]string{"name": name}}, EndSession: true}
	}

	return actions.Outcome{Say: actions.Message{ID: "device.identify_unnamed", Params: map[string]string{"suffix": spokenIDSuffix(deviceID)}}, EndSession: true}
}

// spokenIDSuffix is the last three characters of a device ID, spaced so Alexa reads them one at a time: "amzn1.ask.device.AMA…SJY" becomes "S J Y".
func spokenIDSuffix(deviceID string) string {
	r := []rune(deviceID)
	if len(r) > 3 {
		r = r[len(r)-3:]
	}

	parts := make([]string, len(r))
	for i, c := range r {
		parts[i] = string(c)
	}

	return strings.Join(parts, " ")
}

func helpMessage() actions.Message {
	return actions.Message{ID: "help.general", Params: map[string]string{"example": exampleTitle}}
}

func fallbackMessage() actions.Message {
	return actions.Message{ID: "fallback", Params: map[string]string{"example": exampleTitle}}
}

func titleRequest(slot Slot, kinds []media.Kind, pending []actions.Choice) actions.TitleRequest {
	req := actions.TitleRequest{Query: slot.Value, Kinds: kinds, Pending: pending}

	if ids := slot.ResolvedIDs(); len(ids) > 0 {
		req.Hints = map[string]float64{}
		for _, id := range ids {
			req.Hints[id] = resolvedBoost
		}
	}

	return req
}

func pendingChoices(attrs map[string]any) []actions.Choice {
	var choices []actions.Choice

	if !sessionValue(attrs, sessionPending, &choices) {
		return nil
	}

	return choices
}

func sessionFollowupOf(attrs map[string]any) *actions.Followup {
	var f actions.Followup

	if !sessionValue(attrs, sessionFollowup, &f) {
		return nil
	}

	return &f
}

// Session attributes come back as generic JSON.
func sessionValue(attrs map[string]any, key string, v any) bool {
	raw, ok := attrs[key]
	if !ok {
		return false
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return false
	}

	return json.Unmarshal(b, v) == nil
}

func slotValues(slots map[string]Slot) map[string]string {
	if len(slots) == 0 {
		return nil
	}

	out := make(map[string]string, len(slots))

	for name, s := range slots {
		if s.Value != "" {
			out[name] = s.Value
		}
	}

	return out
}

func describeOutcome(o actions.Outcome) string {
	switch {
	case o.Err != nil:
		return "error"
	case o.Played != nil:
		return "played"
	case len(o.Pending) > 0 || o.Followup != nil:
		return "asked"
	case o.Say.ID == "match.not_found":
		return "not_found"
	default:
		return "ok"
	}
}
