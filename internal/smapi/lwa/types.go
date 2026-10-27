package lwa

import (
	"context"
	"net/http"
	"sync"

	"golang.org/x/oauth2"
)

// Credentials identify the user's own LWA security profile.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// Config configures a Client.
type Config struct {
	Credentials Credentials
	// RedirectURL must be listed under the security profile's Allowed Return URLs.
	RedirectURL string
	// Endpoint overrides Amazon's endpoints; tests point it at a fake.
	Endpoint *oauth2.Endpoint
	// HTTPClient is used for token requests; nil means http.DefaultClient.
	HTTPClient *http.Client
}

// Client performs the LWA flows for one security profile.
type Client struct {
	oauth *oauth2.Config
	http  *http.Client
}

// PKCE is a code verifier; keep it server-side with the state until the callback arrives.
type PKCE struct {
	Verifier string
}

// TokenStore persists the user's token. Load returns (nil, nil) when nothing is stored.
type TokenStore interface {
	Load(ctx context.Context) (*oauth2.Token, error)
	Save(ctx context.Context, tok *oauth2.Token) error
}

// TokenSource hands out valid access tokens, refreshing through LWA and saving refreshed tokens to its store. It is safe for concurrent use.
type TokenSource struct {
	client *Client
	store  TokenStore

	mu  sync.Mutex
	tok *oauth2.Token
}

// MemoryStore is a TokenStore that keeps the token in memory, for tests and one-off tools.
type MemoryStore struct {
	mu  sync.Mutex
	tok *oauth2.Token
}
