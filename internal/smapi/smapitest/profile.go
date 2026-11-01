package smapitest

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Profile sets what profileNlu answers for utterance in locale: each call takes the next answer and the last one repeats. An utterance with no answers selects no intent, as Amazon's NLU does for words it can't place.
func (s *Server) Profile(locale, utterance string, answers ...NLUAnswer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.profiles[profileKey(locale, utterance)] = answers
}

// Profiled counts profileNlu calls for utterance in locale.
func (s *Server) Profiled(locale, utterance string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.profiled[profileKey(locale, utterance)]
}

func profileKey(locale, utterance string) string {
	return locale + "\x00" + strings.ToLower(utterance)
}

func (s *Server) handleProfileNLU(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Utterance string `json:"utterance"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Utterance == "" {
		writeError(w, http.StatusBadRequest, "utterance is required")

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.skills[r.PathValue("skillId")]; !ok {
		writeError(w, http.StatusNotFound, "skill not found")

		return
	}

	key := profileKey(r.PathValue("locale"), req.Utterance)
	n := s.profiled[key]
	s.profiled[key]++

	answers := s.profiles[key]
	if len(answers) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"sessionEnded": false, "consideredIntents": []any{}})

		return
	}

	a := answers[min(n, len(answers)-1)]
	if a.Status != 0 {
		writeError(w, a.Status, "profile failed")

		return
	}

	resp := map[string]any{"sessionEnded": a.SessionEnded}

	if a.Intent != "" {
		resp["selectedIntent"] = nluIntent(a.Intent, a.Slots, a.Resolved)
	}

	considered := []any{}
	for _, name := range a.Considered {
		considered = append(considered, nluIntent(name, nil, nil))
	}

	resp["consideredIntents"] = considered

	writeJSON(w, http.StatusOK, resp)
}

// nluIntent writes an intent as Amazon does, resolutions under value as in a skill request.
func nluIntent(name string, slots, resolved map[string]string) map[string]any {
	out := map[string]any{}

	for slot, value := range slots {
		s := map[string]any{"name": slot, "value": value, "confirmationStatus": "NONE"}

		if id, ok := resolved[slot]; ok {
			s["resolutions"] = map[string]any{"resolutionsPerAuthority": []any{map[string]any{
				"authority": "amzn1.er-authority.echo-sdk.fake." + slot,
				"status":    map[string]any{"code": "ER_SUCCESS_MATCH"},
				"values":    []any{map[string]any{"value": map[string]any{"name": value, "id": id}}},
			}}}
		}

		out[slot] = s
	}

	return map[string]any{"name": name, "confirmationStatus": "NONE", "slots": out}
}
