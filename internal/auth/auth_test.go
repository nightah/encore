package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/auth/oidctest"
	"github.com/nightah/encore/internal/basepath"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

type fixture struct {
	svc *Service
	db  *store.DB
	sec *secrets.Store
	log *bytes.Buffer

	mu          sync.Mutex
	settings    Settings
	settingsErr error
	now         time.Time
}

func newFixture(t *testing.T, mutate func(*Settings)) *fixture {
	t.Helper()

	dir := t.TempDir()
	f := &fixture{log: &bytes.Buffer{}, now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}

	db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(dir), Now: f.clock})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	f.db = db

	sec, err := secrets.Open(t.Context(), db.SecretRows(), secrets.Options{DataDir: dir})
	require.NoError(t, err)

	f.sec = sec
	f.settings = Settings{
		Mode:            ModeLocal,
		SessionTTL:      time.Hour,
		LockoutAttempts: 3,
		LockoutWindow:   15 * time.Minute,
		Header:          HeaderSettings{UserHeader: "Remote-User", GroupsHeader: "Remote-Groups"},
		OIDC:            OIDCSettings{Scopes: []string{"openid", "profile", "groups"}, GroupsClaim: "groups"},
	}

	if mutate != nil {
		mutate(&f.settings)
	}

	f.svc, err = New(t.Context(), Options{
		DB:      db,
		Secrets: sec,
		Settings: func(context.Context) (Settings, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.settings, f.settingsErr
		},
		Logger: slog.New(slog.NewTextHandler(f.log, nil)),
		Now:    f.clock,
	})
	require.NoError(t, err)

	return f
}

func (f *fixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.now
}

func (f *fixture) update(fn func(s *Settings)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	fn(&f.settings)
}

func (f *fixture) failSettings(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.settingsErr = err
}

func (f *fixture) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func request(method, target string, cookies ...*http.Cookie) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	r.RemoteAddr = "192.0.2.10:5555"

	for _, c := range cookies {
		r.AddCookie(c)
	}

	return r
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}

	return nil
}

func (f *fixture) setup(t *testing.T) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	require.NoError(t, f.svc.CreateAdmin(t.Context(), rec, request(http.MethodPost, "/"), f.svc.setupToken, "admin", "correct horse battery"))

	c := cookie(rec, SessionCookie)
	require.NotNil(t, c)

	return c
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("hunter2hunter2")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$"))

	ok, err := VerifyPassword(h, "hunter2hunter2")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = VerifyPassword(h, "hunter3hunter3")
	require.NoError(t, err)
	assert.False(t, ok)

	_, err = VerifyPassword("$2a$10$bcrypt", "x")
	require.Error(t, err)

	other, err := HashPassword("hunter2hunter2")
	require.NoError(t, err)
	assert.NotEqual(t, h, other, "salts differ")

	require.Error(t, ValidatePassword("short"))
	require.NoError(t, ValidatePassword("long enough!"))
}

func TestSetupTokenFlow(t *testing.T) {
	f := newFixture(t, nil)

	token := f.svc.setupToken
	require.NotEmpty(t, token)
	assert.Contains(t, f.log.String(), token, "the setup token is printed to the log")

	required, err := f.svc.SetupRequired(t.Context())
	require.NoError(t, err)
	assert.True(t, required)

	rec := httptest.NewRecorder()
	err = f.svc.CreateAdmin(t.Context(), rec, request(http.MethodPost, "/"), "wrong", "admin", "correct horse battery")
	require.ErrorIs(t, err, ErrBadSetupToken)

	var input *InputError
	require.ErrorAs(t, f.svc.CreateAdmin(t.Context(), rec, request(http.MethodPost, "/"), token, "admin", "short"), &input)
	assert.Equal(t, "password", input.Field)

	c := f.setup(t)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.False(t, c.Secure, "plain HTTP from an untrusted peer")

	required, err = f.svc.SetupRequired(t.Context())
	require.NoError(t, err)
	assert.False(t, required)

	err = f.svc.CreateAdmin(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), token, "other", "correct horse battery")
	require.ErrorIs(t, err, ErrAdminExists)

	p, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "admin", p.Name)

	var stored store.Session
	sessions, err := f.db.Sessions().Get(t.Context(), hashToken(c.Value))
	require.NoError(t, err)
	stored = sessions
	assert.NotEqual(t, []byte(c.Value), stored.IDHash, "only a hash is stored")
}

func TestLoginLockout(t *testing.T) {
	f := newFixture(t, nil)
	f.setup(t)

	login := func(user, password string) error {
		return f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), user, password, "")
	}

	require.ErrorIs(t, login("nobody", "correct horse battery"), ErrBadCredentials)
	require.ErrorIs(t, login("admin", "wrong password"), ErrBadCredentials)
	require.ErrorIs(t, login("Admin", "wrong password"), ErrBadCredentials, "accounts are counted case-insensitively")

	var locked *LockedError
	require.ErrorAs(t, login("admin", "wrong again"), &locked, "third failure locks the account")
	assert.Equal(t, 15*time.Minute, locked.RetryAfter)

	require.ErrorAs(t, login("admin", "correct horse battery"), &locked, "even the right password is refused while locked")

	f.advance(16 * time.Minute)
	require.NoError(t, login("admin", "correct horse battery"))
}

func TestLoginIPLockoutAcrossUsernames(t *testing.T) {
	f := newFixture(t, nil)
	f.setup(t)

	var locked *LockedError

	for i := range 12 {
		err := f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "user"+string(rune('a'+i)), "x", "")
		if i < 11 {
			require.ErrorIs(t, err, ErrBadCredentials)
		} else {
			require.ErrorAs(t, err, &locked, "the address is locked after 4x the per-account limit")
		}
	}
}

func TestLocalLoginDisabledInOtherModes(t *testing.T) {
	f := newFixture(t, func(s *Settings) { s.Mode = ModeOIDC })
	f.setup(t)

	err := f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin", "correct horse battery", "")
	require.ErrorIs(t, err, ErrLocalDisabled)

	f.settings.BreakGlass = true
	require.NoError(t, f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin", "correct horse battery", ""))
}

func TestTOTP(t *testing.T) {
	f := newFixture(t, nil)
	f.setup(t)

	secret, otpURL, err := f.svc.EnrollTOTP(t.Context())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(otpURL, "otpauth://totp/Encore:admin?"))

	code, err := totpCode(secret, f.clock())
	require.NoError(t, err)

	require.ErrorIs(t, f.svc.ConfirmTOTP(t.Context(), "000000"), ErrBadTOTP)
	require.NoError(t, f.svc.ConfirmTOTP(t.Context(), code))

	a, err := f.svc.Admin(t.Context())
	require.NoError(t, err)
	assert.True(t, a.TOTP)

	login := func(code string) error {
		return f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin", "correct horse battery", code)
	}

	require.ErrorIs(t, login(""), ErrTOTPRequired)
	require.ErrorIs(t, login(code), ErrBadTOTP, "a code cannot be replayed")

	f.advance(30 * time.Second)

	code, err = totpCode(secret, f.clock())
	require.NoError(t, err)
	require.NoError(t, login(code))
}

func TestSessionSlidingExpiry(t *testing.T) {
	f := newFixture(t, nil)
	c := f.setup(t)

	for range 3 {
		f.advance(50 * time.Minute)

		p, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
		require.NoError(t, err)
		require.NotNil(t, p, "activity keeps the session alive past its original hour")
	}

	f.advance(61 * time.Minute)

	rec := httptest.NewRecorder()
	p, err := f.svc.Authenticate(rec, request(http.MethodGet, "/", c))
	require.NoError(t, err)
	assert.Nil(t, p)
	assert.Equal(t, -1, cookie(rec, SessionCookie).MaxAge, "the stale cookie is cleared")
}

func TestLogoutAndEndOtherSessions(t *testing.T) {
	f := newFixture(t, nil)
	c := f.setup(t)

	rec := httptest.NewRecorder()
	require.NoError(t, f.svc.Login(t.Context(), rec, request(http.MethodPost, "/"), "admin", "correct horse battery", ""))
	other := cookie(rec, SessionCookie)

	require.NoError(t, f.svc.EndOtherSessions(t.Context(), request(http.MethodGet, "/", c)))

	p, _ := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", other))
	assert.Nil(t, p)

	p, _ = f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
	assert.NotNil(t, p)

	require.NoError(t, f.svc.Logout(httptest.NewRecorder(), request(http.MethodPost, "/", c)))

	p, _ = f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
	assert.Nil(t, p)
}

func TestSessionInHeaderModeIsNotAHeaderPrincipal(t *testing.T) {
	f := newFixture(t, func(s *Settings) { s.BreakGlass = true })
	c := f.setup(t)

	f.mu.Lock()
	f.settings.Mode = ModeHeader
	f.mu.Unlock()

	p, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, ModeLocal, p.Method, "a break-glass session must not skip checks that trust the proxy's sign-in")
	assert.False(t, p.SessionCreated.IsZero())
}

func TestTrustedHeaders(t *testing.T) {
	f := newFixture(t, func(s *Settings) {
		s.Mode = ModeHeader
		s.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.20.0.0/16")}
		s.Header.AdminGroup = "encore-admins"
	})

	fromProxy := func(user, groups string) *http.Request {
		r := request(http.MethodGet, "/")
		r.RemoteAddr = "172.20.0.5:40000"
		r.Header.Set("Remote-User", user)
		r.Header.Set("Remote-Groups", groups)

		return r
	}

	p, err := f.svc.Authenticate(httptest.NewRecorder(), fromProxy("household-admin", "users, encore-admins"))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "household-admin", p.Name)
	assert.Equal(t, ModeHeader, p.Method)

	_, err = f.svc.Authenticate(httptest.NewRecorder(), fromProxy("household-member", "users"))
	require.ErrorIs(t, err, ErrNotAdmin)

	spoofed := fromProxy("intruder", "encore-admins")
	spoofed.RemoteAddr = "192.0.2.66:1234"

	p, err = f.svc.Authenticate(httptest.NewRecorder(), spoofed)
	require.NoError(t, err)
	assert.Nil(t, p, "headers from an untrusted peer are ignored")
	assert.Empty(t, spoofed.Header.Get("Remote-User"), "and stripped")
}

func TestCookiesAndURLsFollowTheRequestPrefix(t *testing.T) {
	f := newFixture(t, nil)

	r := request(http.MethodPost, "/")
	assert.Equal(t, "http://"+r.Host, f.svc.ExternalBase(r))

	r = r.WithContext(basepath.With(r.Context(), "/encore"))
	assert.Equal(t, "http://"+r.Host+"/encore", f.svc.ExternalBase(r))

	rec := httptest.NewRecorder()
	require.NoError(t, f.svc.CreateAdmin(t.Context(), rec, r, f.svc.setupToken, "admin", "correct horse battery"))

	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)
	assert.Equal(t, "/encore/", cookies[0].Path)
}

func TestSecureAndClientIP(t *testing.T) {
	f := newFixture(t, func(s *Settings) { s.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")} })

	r := request(http.MethodGet, "/")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	assert.False(t, f.svc.Secure(r), "X-Forwarded-Proto from an untrusted peer is ignored")
	assert.Equal(t, "192.0.2.10", f.svc.ClientIP(r))

	r.RemoteAddr = "10.1.2.3:443"
	assert.True(t, f.svc.Secure(r))
	assert.Equal(t, "203.0.113.9", f.svc.ClientIP(r))
}

func TestCSRF(t *testing.T) {
	f := newFixture(t, nil)

	rec := httptest.NewRecorder()
	token := f.svc.CSRFToken(rec, request(http.MethodGet, "/"))
	c := cookie(rec, csrfCookie)
	require.NotNil(t, c)
	assert.True(t, c.HttpOnly)
	assert.NotEqual(t, c.Value, token, "the token is not the cookie")

	r := request(http.MethodPost, "/", c)
	assert.False(t, f.svc.CheckCSRF(r), "missing header")

	r.Header.Set(CSRFHeader, "forged")
	assert.False(t, f.svc.CheckCSRF(r))

	r.Header.Set(CSRFHeader, token)
	assert.True(t, f.svc.CheckCSRF(r))

	r = request(http.MethodPost, "/")
	r.Header.Set(CSRFHeader, token)
	assert.False(t, f.svc.CheckCSRF(r), "a token without its cookie is refused")

	assert.Equal(t, token, f.svc.CSRFToken(httptest.NewRecorder(), request(http.MethodGet, "/", c)), "stable for a browser")
}

func oidcFixture(t *testing.T, opts oidctest.Options, adminGroup string) (*fixture, *oidctest.Server) {
	t.Helper()

	idp := oidctest.New(t, opts)
	f := newFixture(t, func(s *Settings) {
		s.Mode = ModeOIDC
		s.OIDC.Issuer = idp.URL
		s.OIDC.ClientID = oidctest.DefaultClientID
		s.OIDC.ClientSecret = oidctest.DefaultClientSecret
		s.OIDC.AdminGroup = adminGroup
	})

	// The provider signs real tokens, so verification needs the real clock.
	f.mu.Lock()
	f.now = time.Now()
	f.mu.Unlock()

	return f, idp
}

// signIn runs the browser's side of the code flow and returns the callback's result.
func signIn(t *testing.T, f *fixture) (*httptest.ResponseRecorder, error) {
	t.Helper()

	rec := httptest.NewRecorder()
	loc, err := f.svc.BeginOIDC(t.Context(), rec, request(http.MethodGet, "http://encore.test/api/v1/auth/oidc/login"))
	require.NoError(t, err)

	state := cookie(rec, oidcCookie)
	require.NotNil(t, state)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, loc, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusFound, resp.StatusCode)

	cb, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "http://encore.test/api/v1/auth/oidc/callback", cb.Scheme+"://"+cb.Host+cb.Path)

	out := httptest.NewRecorder()
	err = f.svc.FinishOIDC(t.Context(), out, request(http.MethodGet, cb.String(), state))

	return out, err
}

func TestOIDCSignIn(t *testing.T) {
	f, _ := oidcFixture(t, oidctest.Options{Username: "household-admin", Groups: []string{"encore-admins"}}, "encore-admins")

	rec, err := signIn(t, f)
	require.NoError(t, err)

	c := cookie(rec, SessionCookie)
	require.NotNil(t, c)

	p, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "household-admin", p.Name)
	assert.Equal(t, ModeOIDC, p.Method)
}

func TestOIDCClientSecretPostFirst(t *testing.T) {
	f, idp := oidcFixture(t, oidctest.Options{Username: "admin", Groups: []string{"encore-admins"}}, "encore-admins")

	_, err := signIn(t, f)
	require.NoError(t, err)
	assert.Equal(t, []string{"client_secret_post"}, idp.TokenAuths(), "post is tried first, and accepted")
}

func TestOIDCClientSecretBasicFallback(t *testing.T) {
	f, idp := oidcFixture(t, oidctest.Options{Username: "admin", Groups: []string{"encore-admins"}, AuthMethod: "client_secret_basic"}, "encore-admins")

	_, err := signIn(t, f)
	require.NoError(t, err)
	assert.Equal(t, []string{"client_secret_post", "client_secret_basic"}, idp.TokenAuths(), "a rejected post falls back to basic")

	_, err = signIn(t, f)
	require.NoError(t, err)
	assert.Equal(t, []string{"client_secret_post", "client_secret_basic", "client_secret_basic"}, idp.TokenAuths(), "basic is remembered for the next sign-in")
}

func TestOIDCClientSecretPostOnly(t *testing.T) {
	f, idp := oidcFixture(t, oidctest.Options{Username: "admin", Groups: []string{"encore-admins"}, AuthMethod: "client_secret_post"}, "encore-admins")

	_, err := signIn(t, f)
	require.NoError(t, err)
	assert.Equal(t, []string{"client_secret_post"}, idp.TokenAuths(), "no rejected basic attempt first")
}

func TestOIDCGroupsFromUserInfo(t *testing.T) {
	f, _ := oidcFixture(t, oidctest.Options{Username: "household-admin", Groups: []string{"encore-admins"}, GroupsInUserInfoOnly: true}, "encore-admins")

	_, err := signIn(t, f)
	require.NoError(t, err)
}

func TestOIDCClaimsFromUserInfo(t *testing.T) {
	f, _ := oidcFixture(t, oidctest.Options{Subject: "c0ffee", Username: "household-admin", Groups: []string{"encore-admins"}, ClaimsInUserInfoOnly: true}, "encore-admins")

	rec, err := signIn(t, f)
	require.NoError(t, err)

	p, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", cookie(rec, SessionCookie)))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "household-admin", p.Name, "the username comes from userinfo, not the subject")
}

func TestOIDCRefusals(t *testing.T) {
	t.Run("not in admin group", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "household-member", Groups: []string{"users"}}, "encore-admins")

		_, err := signIn(t, f)
		require.ErrorIs(t, err, ErrNotAdmin)
	})

	t.Run("wrong nonce", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "household-admin", WrongNonce: true}, "")

		_, err := signIn(t, f)
		require.ErrorContains(t, err, "nonce")
	})

	t.Run("state from another browser", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "household-admin"}, "")

		rec := httptest.NewRecorder()
		_, err := f.svc.BeginOIDC(t.Context(), rec, request(http.MethodGet, "http://encore.test/"))
		require.NoError(t, err)

		state := cookie(rec, oidcCookie).Value
		err = f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=x&state="+state))
		require.ErrorContains(t, err, "did not start in this browser")
	})

	t.Run("disabled", func(t *testing.T) {
		f := newFixture(t, nil)

		_, err := f.svc.BeginOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/"))
		require.ErrorIs(t, err, ErrOIDCDisabled)
	})
}

// restart builds a second Service on the fixture's database and secrets, as a process restart would.
func (f *fixture) restart(t *testing.T) (*Service, *bytes.Buffer) {
	t.Helper()

	var logs bytes.Buffer

	svc, err := New(t.Context(), Options{
		DB:      f.db,
		Secrets: f.sec,
		Settings: func(context.Context) (Settings, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.settings, f.settingsErr
		},
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		Now:    f.clock,
	})
	require.NoError(t, err)

	return svc, &logs
}

func TestNewRequiresDependencies(t *testing.T) {
	f := newFixture(t, nil)
	settings := func(context.Context) (Settings, error) { return Settings{}, nil }

	for name, opts := range map[string]Options{
		"nothing":     {},
		"no DB":       {Secrets: f.sec, Settings: settings},
		"no secrets":  {DB: f.db, Settings: settings},
		"no settings": {DB: f.db, Secrets: f.sec},
	} {
		_, err := New(t.Context(), opts)
		require.Error(t, err, name)
	}

	svc, err := New(t.Context(), Options{DB: f.db, Secrets: f.sec, Settings: settings})
	require.NoError(t, err, "the logger, clock and HTTP client have defaults")
	assert.Equal(t, http.DefaultClient, svc.httpClient)
	assert.WithinDuration(t, time.Now(), svc.now(), time.Minute)
}

func TestRestartKeepsCSRFTokensAndAccounts(t *testing.T) {
	f := newFixture(t, nil)
	session := f.setup(t)

	rec := httptest.NewRecorder()
	token := f.svc.CSRFToken(rec, request(http.MethodGet, "/"))
	c := cookie(rec, csrfCookie)

	svc, logs := f.restart(t)

	r := request(http.MethodPost, "/", c)
	r.Header.Set(CSRFHeader, token)
	assert.True(t, svc.CheckCSRF(r), "the CSRF key is stored, so tokens survive a restart")

	assert.NotContains(t, logs.String(), "setup token", "no setup token once an admin exists")
	require.ErrorIs(t, svc.CheckSetupToken(request(http.MethodPost, "/"), f.svc.setupToken), ErrAdminExists)

	p, err := svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", session))
	require.NoError(t, err)
	require.NotNil(t, p, "sessions survive a restart")
}

func TestNewReplacesAMalformedCSRFKey(t *testing.T) {
	f := newFixture(t, nil)
	require.NoError(t, f.sec.Put(t.Context(), csrfKeyRef, []byte("too short")))

	_, _ = f.restart(t)

	key, err := f.sec.Get(t.Context(), csrfKeyRef)
	require.NoError(t, err)
	assert.Len(t, key, 32)
}

func TestNewFailsOnAnUnreadableCSRFKey(t *testing.T) {
	f := newFixture(t, nil)

	row, err := f.db.SecretRows().Get(t.Context(), csrfKeyRef)
	require.NoError(t, err)

	row.Ciphertext[0] ^= 0xff
	require.NoError(t, f.db.SecretRows().Put(t.Context(), row))

	_, err = New(t.Context(), Options{DB: f.db, Secrets: f.sec, Settings: func(context.Context) (Settings, error) { return f.settings, nil }})
	require.ErrorIs(t, err, secrets.ErrDecrypt, "a damaged key is not silently replaced")
}

func TestNewFailsOnACorruptAdminRecord(t *testing.T) {
	f := newFixture(t, nil)
	require.NoError(t, f.db.Settings().Set(t.Context(), adminSetting, "not an account"))

	_, err := New(t.Context(), Options{DB: f.db, Secrets: f.sec, Settings: func(context.Context) (Settings, error) { return f.settings, nil }})
	require.Error(t, err)

	_, err = f.svc.Admin(t.Context())
	require.Error(t, err)

	_, err = f.svc.SetupRequired(t.Context())
	require.Error(t, err, "a corrupt account does not reopen setup")
}

func TestSettingsErrorsAreReported(t *testing.T) {
	f := newFixture(t, nil)
	c := f.setup(t)

	want, err := f.svc.Settings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ModeLocal, want.Mode)

	f.failSettings(assert.AnError)

	_, err = f.svc.Settings(t.Context())
	require.ErrorIs(t, err, assert.AnError)

	_, err = f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
	require.ErrorIs(t, err, assert.AnError, "a request is not treated as anonymous when settings are unavailable")

	require.ErrorIs(t, f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin", "correct horse battery", ""), assert.AnError)
	require.ErrorIs(t, f.svc.StartSession(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin"), assert.AnError)

	_, err = f.svc.BeginOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/"))
	require.ErrorIs(t, err, assert.AnError)
	require.ErrorIs(t, f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/")), assert.AnError)

	r := request(http.MethodGet, "/")
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	assert.False(t, f.svc.FromTrustedProxy(r), "no proxy is trusted without settings")
	assert.Equal(t, "10.0.0.1", f.svc.ClientIP(r))
}

func TestPeerAddresses(t *testing.T) {
	f := newFixture(t, func(s *Settings) {
		s.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}
	})

	for remote, trusted := range map[string]bool{
		"10.1.2.3:443":          true,
		"10.1.2.3":              true,
		"[::ffff:10.1.2.3]:443": true,
		"[fd00::1]:443":         true,
		"192.0.2.10:443":        false,
		"[2001:db8::1]:443":     false,
		"@unix-socket":          false,
		"":                      false,
	} {
		r := request(http.MethodGet, "/")
		r.RemoteAddr = remote
		assert.Equal(t, trusted, f.svc.FromTrustedProxy(r), remote)
	}

	r := request(http.MethodGet, "/")
	r.RemoteAddr = "@unix-socket"
	assert.Equal(t, "@unix-socket", f.svc.ClientIP(r), "an unparsable peer is used as is")

	r.RemoteAddr = "[::ffff:192.0.2.10]:443"
	assert.Equal(t, "192.0.2.10", f.svc.ClientIP(r), "IPv4-mapped addresses are unmapped")
}

func TestClientIPWalksForwardedFor(t *testing.T) {
	f := newFixture(t, func(s *Settings) { s.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")} })

	for name, tt := range map[string]struct {
		xff  string
		want string
	}{
		"no header":              {"", "10.1.2.3"},
		"one client":             {"203.0.113.9", "203.0.113.9"},
		"chain of proxies":       {"203.0.113.9, 10.0.0.7, 10.0.0.8", "203.0.113.9"},
		"spoofed left-most hop":  {"198.51.100.1, 203.0.113.9, 10.0.0.7", "203.0.113.9"},
		"only trusted hops":      {"10.0.0.7, 10.0.0.8", "10.1.2.3"},
		"garbage right-most hop": {"203.0.113.9, nonsense", "10.1.2.3"},
		"mapped client":          {"::ffff:203.0.113.9", "203.0.113.9"},
		"ipv6 client":            {"2001:db8::1", "2001:db8::1"},
	} {
		t.Run(name, func(t *testing.T) {
			r := request(http.MethodGet, "/")
			r.RemoteAddr = "10.1.2.3:443"

			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}

			assert.Equal(t, tt.want, f.svc.ClientIP(r))
		})
	}
}

func TestExternalBaseBehindProxies(t *testing.T) {
	f := newFixture(t, func(s *Settings) { s.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")} })

	r := request(http.MethodGet, "http://encore.lan/")
	r.Header.Set("X-Forwarded-Proto", "HTTPS")
	r.Header.Set("X-Forwarded-Host", "encore.example.net, internal.lan")
	assert.Equal(t, "http://encore.lan", f.svc.ExternalBase(r), "forwarded headers from an untrusted peer are ignored")

	r.RemoteAddr = "10.0.0.2:5555"
	assert.Equal(t, "https://encore.example.net", f.svc.ExternalBase(r), "the first forwarded host is the browser's")

	r = request(http.MethodGet, "https://encore.lan/")
	r.TLS = &tls.ConnectionState{}
	r = r.WithContext(basepath.With(r.Context(), "/encore/"))
	assert.True(t, f.svc.Secure(r))
	assert.Equal(t, "https://encore.lan/encore", f.svc.ExternalBase(r), "without a trailing slash")
}

func TestTrustedHeaderEdgeCases(t *testing.T) {
	f := newFixture(t, func(s *Settings) {
		s.Mode = ModeHeader
		s.BreakGlass = true
		s.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.20.0.0/16")}
	})
	c := f.setup(t)

	fromProxy := func(user string, cookies ...*http.Cookie) *http.Request {
		r := request(http.MethodGet, "/", cookies...)
		r.RemoteAddr = "172.20.0.5:40000"
		r.Header.Set("Remote-User", user)

		return r
	}

	p, err := f.svc.Authenticate(httptest.NewRecorder(), fromProxy("  household-member  "))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "household-member", p.Name, "without an admin group every proxied user is an admin")

	p, err = f.svc.Authenticate(httptest.NewRecorder(), fromProxy("   ", c))
	require.NoError(t, err)
	require.NotNil(t, p, "a blank user header falls back to the session")
	assert.Equal(t, ModeLocal, p.Method)

	p, err = f.svc.Authenticate(httptest.NewRecorder(), fromProxy(""))
	require.NoError(t, err)
	assert.Nil(t, p)

	f.update(func(s *Settings) { s.Header.AdminGroup = "encore-admins" })

	r := fromProxy("household-admin")
	r.Header.Set("Remote-Groups", " ,encore-admins-old, ENCORE-ADMINS,")
	_, err = f.svc.Authenticate(httptest.NewRecorder(), r)
	require.ErrorIs(t, err, ErrNotAdmin, "group names match exactly")

	r.Header.Set("Remote-Groups", "users,, encore-admins ")
	p, err = f.svc.Authenticate(httptest.NewRecorder(), r)
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestAuthenticateStripsSpoofableHeaders(t *testing.T) {
	f := newFixture(t, func(s *Settings) {
		s.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
		s.Header = HeaderSettings{UserHeader: "X-Auth-User", GroupsHeader: "X-Auth-Groups"}
	})

	headers := []string{"X-Auth-User", "X-Auth-Groups", "Remote-User", "Remote-Groups", "Remote-Name", "Remote-Email", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Forwarded-For"}

	build := func(remote string) *http.Request {
		r := request(http.MethodGet, "/")
		r.RemoteAddr = remote

		for _, h := range headers {
			r.Header.Set(h, "spoofed")
		}

		return r
	}

	untrusted := build("192.0.2.66:1234")
	_, err := f.svc.Authenticate(httptest.NewRecorder(), untrusted)
	require.NoError(t, err)

	for _, h := range headers {
		assert.Empty(t, untrusted.Header.Get(h), h)
	}

	trusted := build("10.0.0.2:1234")
	_, err = f.svc.Authenticate(httptest.NewRecorder(), trusted)
	require.NoError(t, err)

	for _, h := range headers {
		assert.Equal(t, "spoofed", trusted.Header.Get(h), "a trusted proxy's headers are kept: %s", h)
	}
}

func TestErrorMessages(t *testing.T) {
	err := error(&InputError{Field: "username", Err: errors.New("must not be empty")})
	assert.Equal(t, "username: must not be empty", err.Error())
	require.ErrorContains(t, errors.Unwrap(err), "must not be empty")

	locked := &LockedError{RetryAfter: 14*time.Minute + 59*time.Second + 600*time.Millisecond}
	assert.Equal(t, "too many failed sign-ins; try again in 15m0s", locked.Error())
}
