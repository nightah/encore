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
	t.Run("ShouldRefuseAProviderError", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		_, state := beginOIDC(t, f)

		err := f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?error=access_denied&error_description=user+cancelled&state="+state.Value, state))
		require.ErrorContains(t, err, "access_denied user cancelled")
	})

	t.Run("ShouldRefuseAMissingState", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		_, state := beginOIDC(t, f)

		err := f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=x", &http.Cookie{Name: oidcCookie, Value: ""}))
		require.ErrorContains(t, err, "did not start in this browser")

		err = f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=x&state=other", state))
		require.ErrorContains(t, err, "did not start in this browser", "the state must match the cookie")
	})

	t.Run("ShouldRefuseAnExpiredSignIn", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		_, state := beginOIDC(t, f)

		f.advance(oidcPendingTTL + time.Second)

		rec := httptest.NewRecorder()
		err := f.svc.FinishOIDC(t.Context(), rec, request(http.MethodGet, "/cb?code=x&state="+state.Value, state))
		require.ErrorContains(t, err, "expired")
		assert.Equal(t, -1, cookie(rec, oidcCookie).MaxAge, "the state cookie is cleared")
	})

	t.Run("ShouldRefuseAnUnknownState", func(t *testing.T) {
		f, _ := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		forged := &http.Cookie{Name: oidcCookie, Value: "forged"}

		err := f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=x&state=forged", forged))
		require.ErrorContains(t, err, "expired")
	})

	t.Run("ShouldRefuseABadCode", func(t *testing.T) {
		f, idp := oidcFixture(t, oidctest.Options{Username: "admin"}, "")
		_, state := beginOIDC(t, f)

		err := f.svc.FinishOIDC(t.Context(), httptest.NewRecorder(), request(http.MethodGet, "/cb?code=bogus&state="+state.Value, state))
		require.ErrorContains(t, err, "redeeming the authorisation code")
		assert.Equal(t, []string{"client_secret_post"}, idp.TokenAuths(), "a bad code is not retried with another client authentication")
	})

	t.Run("ShouldRefuseAWrongClientSecret", func(t *testing.T) {
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

	t.Run("ShouldRefuseAReplayedCallback", func(t *testing.T) {
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

	t.Run("ShouldRefuseWhenDisabled", func(t *testing.T) {
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

	testCases := []struct {
		name     string
		mutate   func(*Settings)
		expected bool
	}{
		{name: "ShouldBeEnabledWhenFullyConfigured", mutate: func(*Settings) {}, expected: true},
		{name: "ShouldBeDisabledInLocalMode", mutate: func(s *Settings) { s.Mode = ModeLocal }},
		{name: "ShouldBeDisabledWithoutAnIssuer", mutate: func(s *Settings) { s.OIDC.Issuer = "" }},
		{name: "ShouldBeDisabledWithoutAClientID", mutate: func(s *Settings) { s.OIDC.ClientID = "" }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := full
			tc.mutate(&s)
			assert.Equal(t, tc.expected, s.OIDCEnabled())
		})
	}
}

func TestClaimStrings(t *testing.T) {
	testCases := []struct {
		name     string
		have     any
		expected []string
	}{
		{name: "ShouldSplitACommaString", have: "admins, users,,", expected: []string{"admins", "users"}},
		{name: "ShouldKeepTheStringsOfAnArray", have: []any{"admins", 7, "users", nil}, expected: []string{"admins", "users"}},
		{name: "ShouldKeepAnEmptyArrayEmpty", have: []any{}, expected: []string{}},
		{name: "ShouldReturnNilWhenMissing", have: nil},
		{name: "ShouldReturnNilForANumber", have: 42.0},
		{name: "ShouldReturnNilForAnObject", have: map[string]any{"admins": true}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, claimStrings(tc.have))
		})
	}
}

func TestFirstString(t *testing.T) {
	claims := map[string]any{"preferred_username": "  ", "email": 7, "name": "Household Admin"}

	testCases := []struct {
		name     string
		claims   map[string]any
		keys     []string
		expected string
	}{
		{name: "ShouldSkipBlankAndNonStringClaims", claims: claims, keys: userClaims, expected: "Household Admin"},
		{name: "ShouldReturnEmptyForAMissingClaim", claims: claims, keys: []string{"missing"}},
		{name: "ShouldReturnEmptyWithoutClaims", keys: userClaims},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, firstString(tc.claims, tc.keys...))
		})
	}
}

func TestInvalidClient(t *testing.T) {
	testCases := []struct {
		name     string
		have     error
		expected bool
	}{
		{name: "ShouldIgnoreANetworkError", have: errors.New("network down")},
		{name: "ShouldDetectAnInvalidClientCode", have: &oauth2.RetrieveError{ErrorCode: "invalid_client", Response: &http.Response{StatusCode: http.StatusBadRequest}}, expected: true},
		{name: "ShouldDetectAnUnauthorisedStatus", have: &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusUnauthorized}}, expected: true},
		{name: "ShouldIgnoreAnInvalidGrant", have: &oauth2.RetrieveError{ErrorCode: "invalid_grant", Response: &http.Response{StatusCode: http.StatusBadRequest}}},
		{name: "ShouldIgnoreAnInvalidGrantWithoutAResponse", have: &oauth2.RetrieveError{ErrorCode: "invalid_grant"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, invalidClient(tc.have))
		})
	}
}
