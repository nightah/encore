package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/auth/oidctest"
)

// beginOIDC starts a sign-in and returns the provider URL and the browser's state cookie.
func beginOIDC(t *testing.T, f *fixture) (*url.URL, *http.Cookie) {
	t.Helper()

	rec := httptest.NewRecorder()
	loc, err := f.svc.BeginOIDC(t.Context(), rec, request(http.MethodGet, "http://encore.test/api/v1/auth/oidc/login"))
	require.NoError(t, err)

	u, err := url.Parse(loc)
	require.NoError(t, err)

	c := cookie(rec, oidcCookie)
	require.NotNil(t, c)

	return u, c
}

func TestBeginOIDCAuthorisationRequest(t *testing.T) {
	f, idp := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
	f.update(func(s *Settings) {
		s.OIDC.Scopes = []string{"profile", "groups"}
		s.OIDC.RedirectURI = "https://encore.example.net/api/v1/auth/oidc/callback"
	})

	u, state := beginOIDC(t, f)
	q := u.Query()

	assert.Equal(t, idp.URL+"/authorize", u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, "openid profile groups", q.Get("scope"), "openid is always requested, first")
	assert.Equal(t, "https://encore.example.net/api/v1/auth/oidc/callback", q.Get("redirect_uri"), "a configured redirect URI wins")
	assert.Equal(t, oidctest.DefaultClientID, q.Get("client_id"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.NotEmpty(t, q.Get("nonce"))
	assert.Equal(t, state.Value, q.Get("state"), "the state is bound to this browser")
	assert.True(t, state.HttpOnly)
	assert.Equal(t, int(oidcPendingTTL.Seconds()), state.MaxAge)

	u2, state2 := beginOIDC(t, f)
	assert.NotEqual(t, state.Value, state2.Value, "every sign-in gets its own state")
	assert.NotEqual(t, q.Get("nonce"), u2.Query().Get("nonce"))
}

func TestBeginOIDCKeepsOpenIDScopeOnce(t *testing.T) {
	f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
	f.update(func(s *Settings) { s.OIDC.Scopes = []string{"email", "openid"} })

	u, _ := beginOIDC(t, f)
	assert.Equal(t, "email openid", u.Query().Get("scope"))
}

func TestOIDCDiscoveryIsCached(t *testing.T) {
	f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")

	var discoveries atomic.Int32

	f.svc.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
			discoveries.Add(1)
		}

		return http.DefaultTransport.RoundTrip(r)
	})}

	_, err := signIn(t, f)
	require.NoError(t, err)

	_, err = signIn(t, f)
	require.NoError(t, err)
	assert.EqualValues(t, 1, discoveries.Load())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestOIDCUnreachableProvider(t *testing.T) {
	idp := httptest.NewServer(http.NotFoundHandler())
	idp.Close()

	f := newFixture(t, func(s *Settings) {
		s.Mode = ModeOIDC
		s.OIDC.Issuer = idp.URL
		s.OIDC.ClientID = "encore"
	})

	_, err := f.svc.BeginOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/"))
	require.ErrorContains(t, err, "discovering OpenID Connect provider")

	f.svc.oidc.mu.Lock()
	assert.Empty(t, f.svc.oidc.providers, "a failed discovery is not cached")
	assert.Empty(t, f.svc.oidc.pending, "and starts no sign-in")
	f.svc.oidc.mu.Unlock()
}

func TestOIDCPendingSignInsAreBounded(t *testing.T) {
	f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")

	for range maxPendingOIDC {
		_, err := f.svc.BeginOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/"))
		require.NoError(t, err)
	}

	_, err := f.svc.BeginOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/"))
	require.ErrorContains(t, err, "too many sign-ins in progress")

	f.advance(oidcPendingTTL + time.Second)

	_, err = f.svc.BeginOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/"))
	require.NoError(t, err, "expired attempts make room")
}

func TestFinishOIDCRefusals(t *testing.T) {
	t.Run("provider error", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		_, state := beginOIDC(t, f)

		err := f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?error=access_denied&error_description=user+cancelled&state="+state.Value, state))
		require.ErrorContains(t, err, "access_denied user cancelled")
	})

	t.Run("missing state", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		_, state := beginOIDC(t, f)

		err := f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=x", &http.Cookie{Name: oidcCookie, Value: ""}))
		require.ErrorContains(t, err, "did not start in this browser")

		err = f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=x&state=other", state))
		require.ErrorContains(t, err, "did not start in this browser", "the state must match the cookie")
	})

	t.Run("expired", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		_, state := beginOIDC(t, f)

		f.advance(oidcPendingTTL + time.Second)

		rec := httptest.NewRecorder()
		err := f.svc.FinishOIDC(t.Context(), rec, request(http.MethodGet, "/cb?code=x&state="+state.Value, state))
		require.ErrorContains(t, err, "expired")
		assert.Equal(t, -1, cookie(rec, oidcCookie).MaxAge, "the state cookie is cleared")
	})

	t.Run("unknown state", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		forged := &http.Cookie{Name: oidcCookie, Value: "forged"}

		err := f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=x&state=forged", forged))
		require.ErrorContains(t, err, "expired")
	})

	t.Run("bad code", func(t *testing.T) {
		f, idp := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		_, state := beginOIDC(t, f)

		err := f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=bogus&state="+state.Value, state))
		require.ErrorContains(t, err, "redeeming the authorisation code")
		assert.Equal(t, []string{"client_secret_post"}, idp.TokenAuths(), "a bad code is not retried with another client authentication")
	})

	t.Run("wrong client secret", func(t *testing.T) {
		f, idp := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		f.update(func(s *Settings) { s.OIDC.ClientSecret = "wrong" })

		_, err := signIn(t, f)
		require.ErrorContains(t, err, "redeeming the authorisation code")
		assert.Equal(t, []string{"client_secret_post", "client_secret_basic"}, idp.TokenAuths())

		f.update(func(s *Settings) { s.OIDC.ClientSecret = oidctest.DefaultClientSecret })

		_, err = signIn(t, f)
		require.NoError(t, err)
		assert.Equal(t, []string{"client_secret_post", "client_secret_basic", "client_secret_post"}, idp.TokenAuths(), "a failed exchange teaches nothing about the provider")
	})

	t.Run("replayed callback", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		u, state := beginOIDC(t, f)

		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u.String(), http.NoBody)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())

		callback := resp.Header.Get("Location")
		require.NoError(t, f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, callback, state)))

		err = f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, callback, state))
		require.ErrorContains(t, err, "expired", "a callback works once")
	})

	t.Run("disabled", func(t *testing.T) {
		f := newFixture(t, nil)
		require.ErrorIs(t, f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb")), ErrOIDCDisabled)
	})
}

func TestOIDCSubjectWhenNoUsernameClaim(t *testing.T) {
	f, _ := oidcFixture(t, oidctest.Options{Subject: "c0ffee", ClaimsInUserInfoOnly: true}, "")

	rec, err := signIn(t, f)
	require.NoError(t, err)

	p, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", cookie(rec, SessionCookie)))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "c0ffee", p.Name, "the subject names a user with no username claims")
}

func TestOIDCEnabled(t *testing.T) {
	full := Settings{Mode: ModeOIDC, OIDC: OIDCSettings{Issuer: "https://id.example.net", ClientID: "encore"}}
	assert.True(t, full.OIDCEnabled())

	for name, mutate := range map[string]func(*Settings){
		"local mode":   func(s *Settings) { s.Mode = ModeLocal },
		"no issuer":    func(s *Settings) { s.OIDC.Issuer = "" },
		"no client ID": func(s *Settings) { s.OIDC.ClientID = "" },
	} {
		s := full
		mutate(&s)
		assert.False(t, s.OIDCEnabled(), name)
	}
}

func TestClaimStrings(t *testing.T) {
	for name, tt := range map[string]struct {
		in   any
		want []string
	}{
		"comma string": {"admins, users,,", []string{"admins", "users"}},
		"array":        {[]any{"admins", 7, "users", nil}, []string{"admins", "users"}},
		"empty array":  {[]any{}, []string{}},
		"missing":      {nil, nil},
		"number":       {42.0, nil},
		"object":       {map[string]any{"admins": true}, nil},
	} {
		assert.Equal(t, tt.want, claimStrings(tt.in), name)
	}
}

func TestFirstString(t *testing.T) {
	claims := map[string]any{"preferred_username": "  ", "email": 7, "name": "Household Admin"}
	assert.Equal(t, "Household Admin", firstString(claims, userClaims...), "blank and non-string claims are skipped")
	assert.Empty(t, firstString(claims, "missing"))
	assert.Empty(t, firstString(nil, userClaims...))
}

func TestInvalidClient(t *testing.T) {
	assert.False(t, invalidClient(errors.New("network down")))
	assert.True(t, invalidClient(&oauth2.RetrieveError{ErrorCode: "invalid_client", Response: &http.Response{StatusCode: http.StatusBadRequest}}))
	assert.True(t, invalidClient(&oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusUnauthorized}}))
	assert.False(t, invalidClient(&oauth2.RetrieveError{ErrorCode: "invalid_grant", Response: &http.Response{StatusCode: http.StatusBadRequest}}))
	assert.False(t, invalidClient(&oauth2.RetrieveError{ErrorCode: "invalid_grant"}))
}
