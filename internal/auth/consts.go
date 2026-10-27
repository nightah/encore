package auth

import (
	"time"
)

// Modes.
const (
	ModeLocal  Mode = "local"
	ModeOIDC   Mode = "oidc"
	ModeHeader Mode = "header"
)

// CSRFHeader carries the CSRF token on state-changing requests.
const CSRFHeader = "X-CSRF-Token"

const (
	csrfKeyRef    = "auth/csrf_key"
	csrfCookieTTL = 400 * 24 * time.Hour
)

// Settings keys and secret IDs for the local admin account.
const (
	adminSetting   = "auth.admin"
	totpRef        = "auth/totp"
	totpPendingRef = "auth/totp_pending"
)

// ipAttemptFactor lets one address make this many times the per-account attempts, so a household behind one NAT is not locked out by one typo-prone member, while a single host still cannot guess freely.
const ipAttemptFactor = 4

// OIDC paths, relative to the base path.
const (
	OIDCLoginPath    = "/api/v1/auth/oidc/login"
	OIDCCallbackPath = "/api/v1/auth/oidc/callback"
)

// oidcPendingTTL is how long a user has to finish signing in at the provider.
const oidcPendingTTL = 10 * time.Minute

// maxPendingOIDC bounds outstanding sign-in attempts, so unauthenticated requests to the login route cannot grow memory without limit.
const maxPendingOIDC = 1000

// Argon2id parameters, the OWASP minimum for a server with little memory to spare: 19 MiB, two passes, one lane.
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// Password length limits. The upper bound stops a huge password from being used to burn CPU.
const (
	MinPasswordLength = 10
	MaxPasswordLength = 1024
)

// Cookie names.
const (
	SessionCookie = "encore_session"
	csrfCookie    = "encore_csrf"
	oidcCookie    = "encore_oidc"
)

// touchEvery limits how often a session's sliding expiry is written back, so browsing does not write to the database on every request.
const touchEvery = time.Minute
