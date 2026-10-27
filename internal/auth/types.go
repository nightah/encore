package auth

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

// Mode is how admins sign in.
type Mode string

// Settings are the effective auth settings, merged from config and the database by the caller.
type Settings struct {
	Mode            Mode
	SessionTTL      time.Duration
	LockoutAttempts int
	LockoutWindow   time.Duration
	BreakGlass      bool
	TrustedProxies  []netip.Prefix
	OIDC            OIDCSettings
	Header          HeaderSettings
}

// OIDCSettings configure OpenID Connect sign-in.
type OIDCSettings struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	AdminGroup   string
	GroupsClaim  string
	RedirectURI  string
}

// HeaderSettings configure trusted-header sign-in.
type HeaderSettings struct {
	UserHeader   string
	GroupsHeader string
	AdminGroup   string
}

// Principal is an authenticated admin.
type Principal struct {
	Name   string
	Method Mode
	// SessionCreated is when the cookie session began; zero for trusted-header requests.
	SessionCreated time.Time
}

// Options configure New.
type Options struct {
	DB      *store.DB
	Secrets *secrets.Store
	// Settings returns the effective settings; it is called on every request, so it should be cheap.
	Settings func(ctx context.Context) (Settings, error)
	Logger   *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// HTTPClient makes requests to the OIDC provider; nil means http.DefaultClient.
	HTTPClient *http.Client
}

// Service authenticates requests and runs the sign-in flows.
type Service struct {
	db         *store.DB
	secrets    *secrets.Store
	settings   func(ctx context.Context) (Settings, error)
	log        *slog.Logger
	now        func() time.Time
	httpClient *http.Client

	csrfKey []byte
	limiter *limiter
	oidc    *oidcState

	mu         sync.Mutex
	setupToken string
	lastTOTP   int64
}

// admin is stored as a setting; the TOTP secret lives in internal/secrets.
type admin struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	TOTP         bool      `json:"totp"`
	CreatedAt    time.Time `json:"created_at"`
}

// Admin describes the local admin account for the settings page.
type Admin struct {
	Username string
	TOTP     bool
}

// limiter counts failed sign-ins per key (an account or a client address) and locks a key out once it reaches its limit within the window. It is in memory: a restart forgives everyone, which is acceptable for a single-admin server.
type limiter struct {
	mu   sync.Mutex
	keys map[string]*attempts
}

type attempts struct {
	first       time.Time
	failures    int
	lockedUntil time.Time
}

type oidcState struct {
	mu        sync.Mutex
	providers map[string]*oidc.Provider
	pending   map[string]pendingOIDC
	// authStyles remembers, per issuer, how its token endpoint accepted the client secret.
	authStyles map[string]oauth2.AuthStyle
}

type pendingOIDC struct {
	nonce       string
	verifier    string
	redirectURI string
	expires     time.Time
}
