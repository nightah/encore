package lwa

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/amazon"
)

// Scopes are the LWA scopes SMAPI needs to create, build, enable and test a skill.
var Scopes = []string{"alexa::ask:skills:readwrite", "alexa::ask:models:readwrite", "alexa::ask:skills:test"}

// New returns a Client. The client ID and secret are both required: LWA only returns a refresh token when the secret is sent.
func New(cfg Config) (*Client, error) {
	if cfg.Credentials.ClientID == "" || cfg.Credentials.ClientSecret == "" {
		return nil, errors.New("lwa: client ID and client secret are required")
	}

	ep := amazon.Endpoint
	if cfg.Endpoint != nil {
		ep = *cfg.Endpoint
	}

	ep.AuthStyle = oauth2.AuthStyleInParams

	return &Client{
		oauth: &oauth2.Config{
			ClientID:     cfg.Credentials.ClientID,
			ClientSecret: cfg.Credentials.ClientSecret,
			Endpoint:     ep,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       Scopes,
		},
		http: cfg.HTTPClient,
	}, nil
}

// NewPKCE generates a fresh PKCE verifier.
func NewPKCE() PKCE {
	return PKCE{Verifier: oauth2.GenerateVerifier()}
}

// Challenge returns the S256 code challenge for the verifier.
func (p PKCE) Challenge() string {
	return oauth2.S256ChallengeFromVerifier(p.Verifier)
}

// NewState returns an unguessable state value for one authorisation attempt.
func NewState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error.

	return base64.RawURLEncoding.EncodeToString(b)
}

// AuthURL returns the LWA consent page URL for one authorisation attempt.
func (c *Client) AuthURL(state string, pkce PKCE) string {
	return c.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(pkce.Verifier))
}

// Exchange trades an authorisation code for tokens.
func (c *Client) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	tok, err := c.oauth.Exchange(c.ctx(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, mapError("exchanging authorisation code", err)
	}

	if tok.RefreshToken == "" {
		return nil, errors.New("lwa: token response has no refresh token")
	}

	return tok, nil
}

// FromRefreshToken checks a pasted refresh token (e.g. from `ask util generate-lwa-tokens`) by redeeming it, and returns the resulting token for storing. The token must have been issued to this client ID.
func (c *Client) FromRefreshToken(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, errors.New("lwa: refresh token is empty")
	}

	return c.refresh(ctx, refreshToken)
}

// LWA leaves refresh_token out of a refresh's response; oauth2 then keeps the one refreshed with, as TestFromRefreshToken checks.
func (c *Client) refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	tok, err := c.oauth.TokenSource(c.ctx(ctx), &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return nil, mapError("refreshing access token", err)
	}

	return tok, nil
}

func (c *Client) ctx(ctx context.Context) context.Context {
	if c.http == nil {
		return ctx
	}

	return context.WithValue(ctx, oauth2.HTTPClient, c.http)
}

// mapError turns LWA's OAuth error codes into the package's sentinel errors, keeping LWA's description but never the token request itself.
func mapError(op string, err error) error {
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		return fmt.Errorf("lwa: %s: %w", op, err)
	}

	detail := re.ErrorCode
	if re.ErrorDescription != "" {
		detail += ": " + re.ErrorDescription
	}

	switch re.ErrorCode {
	case "invalid_grant":
		return fmt.Errorf("%w (%s)", ErrReauthorise, detail)
	case "invalid_client", "unauthorized_client":
		return fmt.Errorf("%w (%s)", ErrInvalidClient, detail)
	case "":
		return fmt.Errorf("lwa: %s: HTTP %d", op, re.Response.StatusCode)
	default:
		return fmt.Errorf("lwa: %s: %s", op, detail)
	}
}
