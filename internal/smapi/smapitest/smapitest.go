package smapitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// New starts a fake and stops it when the test ends.
func New(t testing.TB, opts Options) *Server {
	t.Helper()

	if opts.ClientID == "" {
		opts.ClientID = DefaultClientID
	}

	if opts.ClientSecret == "" {
		opts.ClientSecret = DefaultClientSecret
	}

	if opts.RefreshToken == "" {
		opts.RefreshToken = DefaultRefreshToken
	}

	if opts.AuthCode == "" {
		opts.AuthCode = DefaultAuthCode
	}

	if opts.TokenTTL == 0 {
		opts.TokenTTL = time.Hour
	}

	if opts.Vendors == nil {
		opts.Vendors = []Vendor{{ID: DefaultVendorID, Name: "Fake Developer", Roles: []string{"ROLE_ADMINISTRATOR"}}}
	}

	s := &Server{
		opts:        opts,
		tokens:      map[string]time.Time{},
		calls:       map[string]int{},
		failLocales: map[string]string{},
		unreported:  map[string]bool{},
		unimported:  map[string]bool{},
		valueLimits: map[string]int{},
		rejected:    map[string]bool{},
		uploads:     map[string][]byte{},
		skills:      map[string]*Skill{},
		imports:     map[string]*importJob{},
		lastImport:  map[string]*importJob{},
		statusPolls: map[string]int{},
		sims:        map[string]*simJob{},
		reaches:     map[string]bool{},
		profiles:    map[string][]NLUAnswer{},
		profiled:    map[string]int{},
	}

	s.Server = httptest.NewServer(s.routes())
	t.Cleanup(s.Close)

	return s
}

// Endpoint is the fake's LWA endpoint, for lwa.Config.
func (s *Server) Endpoint() *oauth2.Endpoint {
	return &oauth2.Endpoint{AuthURL: s.URL + "/ap/oa", TokenURL: s.URL + "/auth/o2/token"}
}

// Options returns the effective options, defaults applied.
func (s *Server) Options() Options {
	return s.opts
}

// FailLocale makes interaction model builds for locale fail with message.
func (s *Server) FailLocale(locale, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failLocales[locale] = message
}

// PassLocale undoes FailLocale.
func (s *Server) PassLocale(locale string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.failLocales, locale)
}

// LimitSlotValues makes locale's builds fail, without naming a value, while its model holds more than n values of slotType, as Amazon fails a model too large.
func (s *Server) LimitSlotValues(locale, slotType string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.valueLimits[locale+"/"+slotType] = n
}

// RejectSlotValue makes the build of every locale whose model has value fail with an error naming it.
func (s *Server) RejectSlotValue(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.rejected[value] = true
}

// UnreportLocale leaves locale out of the skill status entirely.
func (s *Server) UnreportLocale(locale string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.unreported[locale] = true
}

// UnimportLocale leaves locale's model out of an import's resources unless it fails, as ImportWithoutModels does for every locale.
func (s *Server) UnimportLocale(locale string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.unimported[locale] = true
}

func (s *Server) localeKey(locale string) string {
	if s.opts.UnderscoreLocales {
		return strings.ReplaceAll(locale, "-", "_")
	}

	return locale
}

// Inject makes the next n SMAPI calls fail with status (e.g. 429 or 503) and Retry-After: 0.
func (s *Server) Inject(status, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for range n {
		s.inject = append(s.inject, status)
	}
}

// Injected reports how many injected failures have been served.
func (s *Server) Injected() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.injectN
}

// RevokeRefreshToken makes the token endpoint answer invalid_grant to refreshes, as LWA does after the user revokes access.
func (s *Server) RevokeRefreshToken() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.revoked = true
}

// ExpireAccessTokens invalidates every issued access token.
func (s *Server) ExpireAccessTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()

	clear(s.tokens)
}

// Refreshes counts successful refresh-token grants.
func (s *Server) Refreshes() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.refreshes
}

// Exchanges counts successful authorisation code grants.
func (s *Server) Exchanges() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.exchanges
}

// Calls counts requests that reached a handler, keyed by route pattern, e.g. "POST /v1/skills/uploads".
func (s *Server) Calls(pattern string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls[pattern]
}

// AddSkill registers an existing skill, as if created earlier, and returns its ID.
func (s *Server) AddSkill(vendorID string, names map[string]string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID("amzn1.ask.skill.")
	s.addSkill(&Skill{ID: id, VendorID: vendorID, Names: names, Models: map[string]json.RawMessage{}})

	return id
}

// Skill returns a copy of the skill with id.
func (s *Server) Skill(id string) (Skill, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sk, ok := s.skills[id]
	if !ok {
		return Skill{}, false
	}

	return *sk, true
}

// SkillCount reports how many skills exist.
func (s *Server) SkillCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.skills)
}

func (s *Server) addSkill(sk *Skill) {
	s.skills[sk.ID] = sk
	s.order = append(s.order, sk.ID)
}

// nextID must be called with s.mu held.
func (s *Server) nextID(prefix string) string {
	s.seq++

	return prefix + "fake-" + strconv.Itoa(s.seq)
}

func (s *Server) count(pattern string) {
	s.mu.Lock()
	s.calls[pattern]++
	s.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]any{"message": fmt.Sprintf(format, args...)})
}
