package smapitest

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /auth/o2/token", s.handleToken)
	mux.HandleFunc("PUT /s3/{id}", s.handleS3Put)

	api := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, s.authorised(pattern, h))
	}

	api("GET /v1/vendors", s.handleVendors)
	api("GET /v1/skills", s.handleListSkills)
	api("POST /v1/skills/uploads", s.handleCreateUpload)
	api("POST /v1/skills/imports", s.handleImportNew)
	api("POST /v1/skills/{skillId}/imports", s.handleImportExisting)
	// GET /v1/skills/imports/{id} and GET /v1/skills/{id}/status overlap as mux patterns, so one handler serves both.
	api("GET /v1/skills/{a}/{b}", s.handleSkillGet)
	api("PUT /v1/skills/{skillId}/stages/{stage}/enablement", s.handleEnable)
	api("GET /v1/skills/{skillId}/stages/{stage}/enablement", s.handleGetEnablement)
	api("GET /v1/skills/{skillId}/stages/{stage}/manifest", s.handleGetManifest)
	api("POST /v2/skills/{skillId}/stages/{stage}/simulations", s.handleSimulate)
	api("GET /v2/skills/{skillId}/stages/{stage}/simulations/{simId}", s.handleGetSimulation)

	return mux
}

// authorised checks the bearer token and serves injected failures before h.
func (s *Server) authorised(pattern string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()

		if len(s.inject) > 0 {
			status := s.inject[0]
			s.inject = s.inject[1:]
			s.injectN++
			s.mu.Unlock()

			w.Header().Set("Retry-After", "0")
			writeError(w, status, "injected failure")

			return
		}

		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		expiry, known := s.tokens[token]
		s.mu.Unlock()

		if !ok || !known || time.Now().After(expiry) {
			writeError(w, http.StatusUnauthorized, "Your token has expired or is invalid")

			return
		}

		s.count(pattern)
		h(w, r)
	})
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())

		return
	}

	if r.PostForm.Get("client_id") != s.opts.ClientID || r.PostForm.Get("client_secret") != s.opts.ClientSecret {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "Client authentication failed")

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	resp := map[string]any{"token_type": "bearer", "expires_in": int(s.opts.TokenTTL / time.Second)}

	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		if r.PostForm.Get("code") != s.opts.AuthCode {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "The authorization code is invalid")

			return
		}

		if r.PostForm.Get("code_verifier") == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code_verifier is required")

			return
		}

		s.exchanges++
		resp["refresh_token"] = s.opts.RefreshToken
	case "refresh_token":
		if s.revoked || r.PostForm.Get("refresh_token") != s.opts.RefreshToken {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "The request has an invalid grant parameter : refresh_token")

			return
		}

		s.refreshes++
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "")

		return
	}

	token := "Atza|fake-access-" + strconv.Itoa(s.exchanges+s.refreshes)
	s.tokens[token] = time.Now().Add(s.opts.TokenTTL)
	resp["access_token"] = token

	writeJSON(w, http.StatusOK, resp)
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
