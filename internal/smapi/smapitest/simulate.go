package smapitest

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// SimulationLocales are the locales the simulation API accepts.
var SimulationLocales = []string{"de-DE", "en-AU", "en-CA", "en-GB", "en-IN", "en-US", "fr-FR", "ja-JP"}

// Answer makes simulating phrase in locale answer a, reaching the skill unless a is Unresolved, once the skill is enabled.
func (s *Server) Answer(locale, phrase string, a Answer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.answers[locale+"\x00"+phrase] = a
}

// SessionModes are the session modes the simulations asked for, in order.
func (s *Server) SessionModes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.modes)
}

// Reach makes simulating phrase in locale reach the skill, as "open <invocation>" does in every locale, once the skill is enabled and has a model for locale.
func (s *Server) Reach(locale, phrase string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reaches[locale+"\x00"+phrase] = true
}

func (s *Server) handleSimulate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input struct {
			Content string `json:"content"`
		} `json:"input"`
		Device struct {
			Locale string `json:"locale"`
		} `json:"device"`
		Session struct {
			Mode string `json:"mode"`
		} `json:"session"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Input.Content == "" {
		writeError(w, http.StatusBadRequest, "input.content is required")

		return
	}

	if !slices.Contains(SimulationLocales, req.Device.Locale) {
		writeError(w, http.StatusBadRequest, "locale %s is not supported for simulation", req.Device.Locale)

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sk, ok := s.skills[r.PathValue("skillId")]
	if !ok {
		writeError(w, http.StatusNotFound, "skill not found")

		return
	}

	if s.busy < s.opts.BusySimulations {
		s.busy++
		writeError(w, http.StatusConflict, "%s", BusyMessage)

		return
	}

	if !sk.Enabled {
		writeError(w, http.StatusConflict, "skill is not enabled for testing")

		return
	}

	job := &simJob{caption: "Sorry, I don't know that."}

	var model struct {
		InteractionModel struct {
			LanguageModel struct {
				InvocationName string `json:"invocationName"`
			} `json:"languageModel"`
		} `json:"interactionModel"`
	}

	if raw, ok := sk.Models[req.Device.Locale]; ok && json.Unmarshal(raw, &model) == nil {
		inv := model.InteractionModel.LanguageModel.InvocationName
		if inv != "" && (strings.EqualFold(req.Input.Content, "open "+inv) || s.reaches[req.Device.Locale+"\x00"+req.Input.Content]) {
			job.reached = true
			job.caption = "Welcome to " + inv + "."
		}
	}

	if a, ok := s.answers[req.Device.Locale+"\x00"+req.Input.Content]; ok {
		job = &simJob{answer: &a, reached: !a.Unresolved, caption: a.Speech}
		if a.Unresolved {
			job.failure = UnresolvedMessage
		}
	}

	s.modes = append(s.modes, req.Session.Mode)

	if s.unresolved < s.opts.UnresolvedSimulations {
		s.unresolved++
		job = &simJob{failure: UnresolvedMessage}
	}

	id := s.nextID("sim-")
	s.sims[id] = job

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "IN_PROGRESS"})
}

func (s *Server) handleGetSimulation(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := r.PathValue("simId")

	job, ok := s.sims[id]
	if !ok {
		writeError(w, http.StatusNotFound, "simulation not found")

		return
	}

	if job.polls < s.opts.Pending {
		job.polls++
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "IN_PROGRESS"})

		return
	}

	if job.failure != "" {
		result := map[string]any{"error": map[string]any{"message": job.failure}}
		if job.answer != nil {
			result["alexaExecutionInfo"] = map[string]any{"consideredIntents": job.answer.considered()}
		}

		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "FAILED", "result": result})

		return
	}

	info := map[string]any{
		"alexaResponses": []any{map[string]any{"type": "Speech", "content": map[string]any{"caption": job.caption}}},
	}

	result := map[string]any{"alexaExecutionInfo": info}

	if a := job.answer; a != nil {
		info["consideredIntents"] = a.considered()
		result["skillExecutionInfo"] = map[string]any{"invocations": []any{map[string]any{
			"invocationRequest":  map[string]any{"endpoint": "https://example.invalid", "body": map[string]any{"version": "1.0", "request": map[string]any{"type": "IntentRequest"}}},
			"invocationResponse": map[string]any{"body": map[string]any{"version": "1.0", "response": a.response()}},
		}}}
	} else if job.reached {
		// The real body mixes strings and objects at the top level, which the generated client cannot decode.
		result["skillExecutionInfo"] = map[string]any{"invocations": []any{map[string]any{
			"invocationRequest":  map[string]any{"endpoint": "https://example.invalid", "body": map[string]any{"version": "1.0", "request": map[string]any{"type": "LaunchRequest"}}},
			"invocationResponse": map[string]any{"body": map[string]any{"version": "1.0", "response": map[string]any{}}},
		}}}
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "SUCCESSFUL", "result": result})
}

// considered is the answer's intent as the simulator lists the intents it weighed, with an empty slot for Alexa's unfilled ones.
func (a *Answer) considered() []any {
	slots := map[string]any{"Room": map[string]any{"name": "Room", "confirmationStatus": "NONE", "source": "USER"}}

	for name, v := range a.Slots {
		slot := map[string]any{"name": name, "value": v, "confirmationStatus": "NONE", "source": "USER"}
		if r, ok := a.Resolved[name]; ok {
			slot["resolutions"] = map[string]any{"resolutionsPerAuthority": []any{
				map[string]any{"authority": "amzn1.er-authority.echo-sdk.TYPE", "status": map[string]any{"code": "ER_SUCCESS_NO_MATCH"}},
				map[string]any{"authority": "amzn1.er-authority.echo-sdk.TYPE", "status": map[string]any{"code": "ER_SUCCESS_MATCH"}, "values": []any{map[string]any{"value": map[string]any{"name": r, "id": "id"}}}},
			}}
		}

		slots[name] = slot
	}

	return []any{map[string]any{"name": a.Intent, "confirmationStatus": "NONE", "slots": slots}, map[string]any{"name": "AMAZON.FallbackIntent"}}
}

// response is the skill's response body for the answer: PlainText speech, SSML reprompt, as Encore's own replies vary.
func (a *Answer) response() map[string]any {
	r := map[string]any{}

	if a.Speech != "" {
		r["outputSpeech"] = map[string]any{"type": "PlainText", "text": a.Speech}
	}

	if a.Reprompt != "" {
		r["reprompt"] = map[string]any{"outputSpeech": map[string]any{"type": "SSML", "ssml": "<speak>" + a.Reprompt + "</speak>"}}
	}

	if a.EndSession != nil {
		r["shouldEndSession"] = *a.EndSession
	}

	if len(a.Directives) > 0 {
		ds := make([]any, 0, len(a.Directives))
		for _, d := range a.Directives {
			ds = append(ds, map[string]any{"type": d})
		}

		r["directives"] = ds
	}

	return r
}
