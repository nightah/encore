package smapitest

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// SimulationLocales are the locales the simulation API accepts.
var SimulationLocales = []string{"de-DE", "en-AU", "en-CA", "en-GB", "en-IN", "en-US", "fr-FR", "ja-JP"}

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
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "FAILED", "result": map[string]any{"error": map[string]any{"message": job.failure}}})

		return
	}

	result := map[string]any{
		"alexaExecutionInfo": map[string]any{
			"alexaResponses": []any{map[string]any{"type": "Speech", "content": map[string]any{"caption": job.caption}}},
		},
	}

	if job.reached {
		// The real body mixes strings and objects at the top level, which the generated client cannot decode.
		result["skillExecutionInfo"] = map[string]any{"invocations": []any{map[string]any{
			"invocationRequest":  map[string]any{"endpoint": "https://example.invalid", "body": map[string]any{"version": "1.0", "request": map[string]any{"type": "LaunchRequest"}}},
			"invocationResponse": map[string]any{"body": map[string]any{"version": "1.0", "response": map[string]any{}}},
		}}}
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "SUCCESSFUL", "result": result})
}
