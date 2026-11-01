package lwa

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
)

// TokenSource returns a TokenSource backed by store.
func (c *Client) TokenSource(store TokenStore) *TokenSource {
	return &TokenSource{client: c, store: store}
}

// Token implements oauth2.TokenSource without a request context; prefer TokenContext.
func (s *TokenSource) Token() (*oauth2.Token, error) {
	return s.TokenContext(context.Background())
}

// TokenContext returns a valid access token, refreshing it if needed.
func (s *TokenSource) TokenContext(ctx context.Context) (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tok == nil {
		tok, err := s.store.Load(ctx)
		if err != nil {
			return nil, fmt.Errorf("lwa: loading token: %w", err)
		}

		if tok == nil || tok.RefreshToken == "" {
			return nil, ErrNotAuthorised
		}

		s.tok = tok
	}

	if s.tok.Valid() && !s.reset {
		return s.tok, nil
	}

	tok, err := s.client.refresh(ctx, s.tok.RefreshToken)
	if err != nil {
		return nil, err
	}

	rotated := tok.RefreshToken != s.tok.RefreshToken
	s.tok = tok
	s.reset = false

	// LWA does not rotate refresh tokens, so a failed save only costs an extra refresh after a restart; it matters only if the refresh token changed.
	if err := s.store.Save(ctx, tok); err != nil && rotated {
		return nil, fmt.Errorf("lwa: saving rotated refresh token: %w", err)
	}

	return tok, nil
}

// Reset drops the cached token, so the next call reloads the refresh token from the store and redeems it for a new access token even if the old one hasn't expired yet: SMAPI rejected it, or the user re-authorised.
func (s *TokenSource) Reset() {
	s.mu.Lock()
	s.tok = nil
	s.reset = true
	s.mu.Unlock()
}

// Rejected is Reset for an access token SMAPI refused, unless the token source has already moved on from it: requests sent together with one token all get a 401, and only the first should cost a refresh.
func (s *TokenSource) Rejected(accessToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tok != nil && s.tok.AccessToken != accessToken {
		return
	}

	s.tok = nil
	s.reset = true
}

// Load implements TokenStore.
func (m *MemoryStore) Load(context.Context) (*oauth2.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.tok == nil {
		return nil, nil //nolint:nilnil // documented TokenStore contract: nothing stored is not an error
	}

	tok := *m.tok

	return &tok, nil
}

// Save implements TokenStore.
func (m *MemoryStore) Save(_ context.Context, tok *oauth2.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := *tok
	m.tok = &cp

	return nil
}
