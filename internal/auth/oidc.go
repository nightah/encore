package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func newOIDCState() *oidcState {
	return &oidcState{providers: map[string]*oidc.Provider{}, pending: map[string]pendingOIDC{}, authStyles: map[string]oauth2.AuthStyle{}}
}

// provider discovers and caches the issuer's configuration. The provider keeps the HTTP client for fetching signing keys later.
func (s *Service) provider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.oidc.mu.Lock()
	p := s.oidc.providers[issuer]
	s.oidc.mu.Unlock()

	if p != nil {
		return p, nil
	}

	discoverCtx, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.httpClient), 15*time.Second)
	defer cancel()

	p, err := oidc.NewProvider(discoverCtx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering OpenID Connect provider %s: %w", issuer, err)
	}

	s.oidc.mu.Lock()
	s.oidc.providers[issuer] = p
	s.oidc.mu.Unlock()

	return p, nil
}

func (s *Service) oauthConfig(p *oidc.Provider, o OIDCSettings, redirectURI string) *oauth2.Config {
	scopes := slices.Clone(o.Scopes)
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}

	return &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  redirectURI,
		Scopes:       scopes,
	}
}

// OIDCEnabled reports whether OIDC sign-in is configured and selected.
func (s Settings) OIDCEnabled() bool {
	return s.Mode == ModeOIDC && s.OIDC.Issuer != "" && s.OIDC.ClientID != ""
}

// BeginOIDC starts a sign-in and returns the provider URL to redirect the browser to. State, nonce and the PKCE verifier stay server-side; the state is also bound to this browser with a short-lived cookie, so a callback started elsewhere is refused.
func (s *Service) BeginOIDC(ctx context.Context, w http.ResponseWriter, r *http.Request) (string, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return "", err
	}

	if !settings.OIDCEnabled() {
		return "", ErrOIDCDisabled
	}

	p, err := s.provider(ctx, settings.OIDC.Issuer)
	if err != nil {
		return "", err
	}

	redirectURI := settings.OIDC.RedirectURI
	if redirectURI == "" {
		redirectURI = s.ExternalBase(r) + OIDCCallbackPath
	}

	state, nonce, verifier := randomToken(24), randomToken(24), oauth2.GenerateVerifier()

	s.oidc.mu.Lock()

	now := s.now()
	for k, v := range s.oidc.pending {
		if now.After(v.expires) {
			delete(s.oidc.pending, k)
		}
	}

	if len(s.oidc.pending) >= maxPendingOIDC {
		s.oidc.mu.Unlock()

		return "", errors.New("too many sign-ins in progress; try again in a few minutes")
	}

	s.oidc.pending[state] = pendingOIDC{nonce: nonce, verifier: verifier, redirectURI: redirectURI, expires: now.Add(oidcPendingTTL)}
	s.oidc.mu.Unlock()

	s.setCookie(w, r, oidcCookie, state, oidcPendingTTL)

	return s.oauthConfig(p, settings.OIDC, redirectURI).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// FinishOIDC completes a sign-in from the provider's redirect: it checks the state against this browser, redeems the code with the PKCE verifier, verifies the ID token and nonce, checks the admin group and starts a session.
func (s *Service) FinishOIDC(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	settings, err := s.settings(ctx)
	if err != nil {
		return err
	}

	if !settings.OIDCEnabled() {
		return ErrOIDCDisabled
	}

	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		return fmt.Errorf("the provider refused the sign-in: %s %s", e, q.Get("error_description"))
	}

	state := q.Get("state")

	c, err := r.Cookie(oidcCookie)
	if err != nil || state == "" || c.Value != state {
		return errors.New("the sign-in did not start in this browser or took too long; try again")
	}

	s.clearCookie(w, r, oidcCookie)

	s.oidc.mu.Lock()
	pending, ok := s.oidc.pending[state]
	delete(s.oidc.pending, state)
	s.oidc.mu.Unlock()

	if !ok || s.now().After(pending.expires) {
		return errors.New("the sign-in expired; try again")
	}

	p, err := s.provider(ctx, settings.OIDC.Issuer)
	if err != nil {
		return err
	}

	hctx := oidc.ClientContext(ctx, s.httpClient)
	cfg := s.oauthConfig(p, settings.OIDC, pending.redirectURI)

	tok, err := s.exchange(hctx, cfg, settings.OIDC.Issuer, q.Get("code"), pending.verifier)
	if err != nil {
		return fmt.Errorf("redeeming the authorisation code: %w", err)
	}

	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return errors.New("the provider returned no ID token")
	}

	idt, err := p.Verifier(&oidc.Config{ClientID: settings.OIDC.ClientID, Now: s.now}).Verify(hctx, raw)
	if err != nil {
		return fmt.Errorf("verifying the ID token: %w", err)
	}

	if idt.Nonce != pending.nonce {
		return errors.New("the ID token's nonce does not match this sign-in")
	}

	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		return fmt.Errorf("reading ID token claims: %w", err)
	}

	groupsClaim := settings.OIDC.GroupsClaim
	if groupsClaim == "" {
		groupsClaim = "groups"
	}

	// Some providers put groups and the username only in the userinfo response; fill in what the ID token left out.
	_, hasGroups := claims[groupsClaim]
	if (!hasGroups && settings.OIDC.AdminGroup != "") || firstString(claims, userClaims...) == "" {
		if info, err := p.UserInfo(hctx, oauth2.StaticTokenSource(tok)); err == nil && info.Subject == idt.Subject {
			extra := map[string]any{}
			if err := info.Claims(&extra); err == nil {
				for _, k := range append([]string{groupsClaim}, userClaims...) {
					if _, has := claims[k]; !has && extra[k] != nil {
						claims[k] = extra[k]
					}
				}
			}
		}
	}

	if g := settings.OIDC.AdminGroup; g != "" && !slices.Contains(claimStrings(claims[groupsClaim]), g) {
		s.log.Warn("OpenID Connect sign-in refused: not in the admin group", "subject", idt.Subject, "admin_group", g)

		return ErrNotAdmin
	}

	name := firstString(claims, userClaims...)
	if name == "" {
		name = idt.Subject
	}

	return s.StartSession(ctx, w, r, name)
}

// userClaims name the signed-in user, in order of preference.
var userClaims = []string{"preferred_username", "email", "name"}

func claimStrings(v any) []string {
	switch v := v.(type) {
	case string:
		return splitGroups(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}

		return out
	default:
		return nil
	}
}

func firstString(claims map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := claims[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}

	return ""
}

// exchange redeems the code, sending the client secret in the request body (client_secret_post) and, if the provider rejects the client, in a Basic header (client_secret_basic). The style that worked is remembered per issuer. Retrying is safe because providers authenticate the client before they look at the code.
func (s *Service) exchange(ctx context.Context, cfg *oauth2.Config, issuer, code, verifier string) (*oauth2.Token, error) {
	s.oidc.mu.Lock()
	style, known := s.oidc.authStyles[issuer]
	s.oidc.mu.Unlock()

	if !known {
		style = oauth2.AuthStyleInParams
	}

	cfg.Endpoint.AuthStyle = style

	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil && !known && invalidClient(err) {
		style = oauth2.AuthStyleInHeader
		cfg.Endpoint.AuthStyle = style
		tok, err = cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	}

	if err != nil {
		return nil, err
	}

	s.oidc.mu.Lock()
	s.oidc.authStyles[issuer] = style
	s.oidc.mu.Unlock()

	return tok, nil
}

func invalidClient(err error) bool {
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		return false
	}

	return re.ErrorCode == "invalid_client" || (re.Response != nil && re.Response.StatusCode == http.StatusUnauthorized)
}
