package alexa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
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

	tr := &requestTrace{}

	ctx := media.ObserveCalls(r.Context(), tr.addCall)

	resp, outcome := h.handleInTime(ctx, &env, entry.Verify, tr)

	// Work the action left running (a progressive response, a slow wake, a stalled Kodi instance) may still report calls; the log keeps those made so far.
	entry.KodiCalls, entry.Target, entry.Via, entry.MatchTime = tr.snapshot()

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

	// Verification ran before this handler, but Alexa waited for it too.
	entry.Duration = h.now().Sub(start) + entry.Verify

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

// handleInTime runs handle under RequestDeadline, less the verify time already spent. When the deadline passes first, or the action failed after it passed, it answers that Kodi isn't answering, so the user hears why rather than Alexa saying the skill didn't respond. Work still running carries on with a cancelled context and its answer is dropped.
func (h *Handler) handleInTime(ctx context.Context, env *RequestEnvelope, verify time.Duration, tr *requestTrace) (ResponseEnvelope, *actions.Outcome) {
	ctx, cancel := context.WithTimeout(ctx, RequestDeadline-verify)
	defer cancel()

	done := make(chan handled, 1)

	go func() {
		// net/http only recovers a panic in its own goroutine; one here would take Encore down.
		defer func() {
			if p := recover(); p != nil {
				h.Log.ErrorContext(ctx, "panicked handling an Alexa request", "panic", p, "stack", string(debug.Stack()))

				o := actions.Outcome{Say: actions.Message{ID: "error.generic"}, EndSession: true, Err: fmt.Errorf("%w: %v", ErrPanicked, p)}
				done <- handled{h.respond(env.Request.Locale, o, false), &o}
			}
		}()

		resp, o := h.handle(ctx, env, tr)
		done <- handled{resp, o}
	}()

	if r, ok := inTime(ctx, done); ok {
		return r.resp, r.outcome
	}

	o := actions.Outcome{Say: actions.Message{ID: "error.not_answering"}, EndSession: true, Err: ErrTooSlow}

	if name := tr.targetName(); name != "" {
		o.Say = actions.Message{ID: "error.kodi_not_answering", Params: map[string]string{"kodi": name}}
	}

	return h.respond(env.Request.Locale, o, false), &o
}

// inTime waits for the handler's answer on done until ctx is done, and reports whether it is one to give. An action that failed once the deadline passed most likely failed because of it; one that succeeded is answered all the same, even when it finished just as the deadline passed.
func inTime(ctx context.Context, done <-chan handled) (handled, bool) {
	var r handled

	select {
	case r = <-done:
	case <-ctx.Done():
		select {
		case r = <-done:
		default:
			return r, false
		}
	}

	return r, ctx.Err() == nil || r.outcome == nil || r.outcome.Err == nil
}

func (h *Handler) handle(ctx context.Context, env *RequestEnvelope, tr *requestTrace) (ResponseEnvelope, *actions.Outcome) {
	locale := env.Request.Locale
	deviceID := env.Context.System.Device.DeviceID

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
	question := sessionQuestionOf(env.Session.Attributes)

	// Intents that don't need Kodi.
	switch intent.Name {
	case "AMAZON.HelpIntent":
		if question != nil {
			// A question offering several titles is answered by naming one; any other by yes or no.
			id := "question.help"
			if len(pendingChoices(env.Session.Attributes)) > 1 {
				id = "match.help"
			}

			return h.respond(locale, askAgain(env, id, *question), false), nil
		}

		msg := helpMessage()

		return h.respond(locale, actions.Outcome{Say: msg, Reprompt: &msg}, false), nil
	case "AMAZON.CancelIntent":
		return h.respond(locale, actions.Outcome{Say: actions.Message{ID: "match.cancelled"}, EndSession: true}, false), nil
	case "AMAZON.YesIntent":
		// "Yes" to "A or B?" chooses neither.
		if pending := pendingChoices(env.Session.Attributes); len(pending) > 1 && question != nil {
			return h.respond(locale, askAgain(env, "match.which_one", *question), false), nil
		}
	case "AMAZON.NoIntent":
		switch {
		case followup != nil && followup.Recommend != nil:
			// Dispatched: another recommendation needs Kodi.
		case (followup == nil || followup.Confirm == "") && len(pendingChoices(env.Session.Attributes)) > 0:
			var more []actions.Choice
			if followup != nil {
				more = followup.More
			}

			o := h.Service.OtherChoices(more)

			return h.respond(locale, o, false), &o
		default:
			return h.respond(locale, actions.Outcome{Say: actions.Message{ID: "match.cancelled"}, EndSession: true}, false), nil
		}
	case "AMAZON.FallbackIntent", "AMAZON.NavigateHomeIntent":
		if question != nil {
			return h.respond(locale, askAgain(env, "question.again", *question), false), nil
		}

		msg := fallbackMessage()

		return h.respond(locale, actions.Outcome{Say: msg, Reprompt: &msg}, false), nil
	case chooseIntent:
		// An answer is acted on only while a question's titles wait for one.
		if len(pendingChoices(env.Session.Attributes)) > 0 {
			break
		}

		if bare, ok := h.Speech.BareCommand(locale, intent.Slots["Answer"].Value); ok {
			intent = bare

			break
		}

		if question != nil {
			return h.respond(locale, askAgain(env, "question.again", *question), false), nil
		}

		msg := fallbackMessage()

		return h.respond(locale, actions.Outcome{Say: msg, Reprompt: &msg}, false), nil
	case "IdentifyEcho":
		o := h.identify(ctx, deviceID)

		return h.respond(locale, o, false), &o
	}

	var (
		target actions.Target
		err    error
	)

	// The ways to read the request, tried in turn until one finds something: a search query may name a film, TV or music intent's sample.
	readings := []Intent{intent}

	switch room := strings.TrimSpace(intent.Slots["Room"].Value); {
	case intent.Name == "PlayMedia":
		target, intent, err = h.targetInQuery(ctx, deviceID, locale, intent)
		readings = h.Speech.Reinterpret(locale, intent)
	case room != "":
		target, err = h.Targets.TargetForRoom(ctx, deviceID, room)
	default:
		target, err = h.Targets.Target(ctx, deviceID)
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

	var timed *timedMatcher

	if target.Matcher != nil {
		timed = &timedMatcher{Matcher: target.Matcher}
		target.Matcher = timed
	}

	tr.setTarget(target, timed)

	o := h.dispatch(ctx, env, target, readings[0])

	for _, reading := range readings[1:] {
		if !o.Missed() {
			break
		}

		if next := h.dispatch(ctx, env, target, reading); !next.Missed() {
			o = next
		}
	}

	if o.AskAgain {
		if question == nil {
			msg := fallbackMessage()
			question = &msg
		}

		o = askAgain(env, o.Say.ID, *question)
	}

	return h.respond(locale, o, target.ConfirmSound), &o
}

// targetInQuery finds a room named in a search query. An AMAZON.SearchQuery slot can't share an utterance with another slot, so "play frozen in the lounge" arrives as the query "frozen in the lounge"; when its ending (or, in some languages, its start) names a known room, the room is taken off the query and its Kodi instance is the target.
func (h *Handler) targetInQuery(ctx context.Context, deviceID, locale string, intent Intent) (actions.Target, Intent, error) {
	const title = "Query"

	for _, split := range h.Speech.RoomSplits(locale, intent.Slots[title].Value) {
		target, err := h.Targets.TargetForRoom(ctx, deviceID, split.Room)

		switch {
		case errors.Is(err, ErrUnknownRoom):
			continue
		case err != nil:
			return actions.Target{}, intent, err
		}

		slots := maps.Clone(intent.Slots)
		slots[title] = Slot{Name: title, Value: split.Title}

		return target, Intent{Name: intent.Name, Slots: slots}, nil
	}

	target, err := h.Targets.Target(ctx, deviceID)

	return target, intent, err
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

		// A question's reprompt is the question.
		if o.Reprompt != nil {
			env.SessionAttributes[sessionQuestion] = *o.Reprompt
		}
	}

	return env
}

func (tr *requestTrace) addCall(c media.Call) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	tr.calls = append(tr.calls, c)
}

func (tr *requestTrace) setTarget(t actions.Target, timed *timedMatcher) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	tr.target, tr.via, tr.timed = t.Name, t.Via, timed
}

func (tr *requestTrace) targetName() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	return tr.target
}

// snapshot returns what the request did so far: its Kodi calls, its target and the time spent matching titles.
func (tr *requestTrace) snapshot() ([]media.Call, string, actions.Via, time.Duration) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	var matching time.Duration
	if tr.timed != nil {
		matching = time.Duration(tr.timed.total.Load())
	}

	return slices.Clone(tr.calls), tr.target, tr.via, matching
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

// askAgain repeats the question a session is waiting on, after the message id, which says why and ends with the {question}. The question's choices and what its answer does are kept.
func askAgain(env *RequestEnvelope, id string, question actions.Message) actions.Outcome {
	return actions.Outcome{
		Say:      actions.Message{ID: id, Messages: map[string]actions.Message{"question": question}},
		Reprompt: &question,
		Pending:  pendingChoices(env.Session.Attributes),
		Followup: sessionFollowupOf(env.Session.Attributes),
	}
}

func titleRequest(slot Slot, kinds []media.Kind, pending []actions.Choice) actions.TitleRequest {
	req := actions.TitleRequest{Query: slot.Value, Kinds: kinds, Pending: pending}

	if res := slot.Resolved(); len(res) > 0 {
		req.Resolved = res
		req.Hints = map[string]float64{}

		for _, r := range res {
			req.Hints[r.ID] = resolvedBoost
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

func sessionQuestionOf(attrs map[string]any) *actions.Message {
	var q actions.Message

	if !sessionValue(attrs, sessionQuestion, &q) || q.ID == "" {
		return nil
	}

	return &q
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
