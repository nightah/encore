package lwa_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

func newClient(t *testing.T, fake *smapitest.Server) *lwa.Client {
	t.Helper()

	opts := fake.Options()

	c, err := lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: opts.ClientID, ClientSecret: opts.ClientSecret},
		RedirectURL: "https://encore.example.com/api/v1/oauth/lwa/callback",
		Endpoint:    fake.Endpoint(),
	})
	require.NoError(t, err)

	return c
}

func TestNewRequiresCredentials(t *testing.T) {
	_, err := lwa.New(lwa.Config{Credentials: lwa.Credentials{ClientID: "id"}})
	require.Error(t, err)
}

func TestAuthURL(t *testing.T) {
	c, err := lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: "my-client", ClientSecret: "secret"},
		RedirectURL: "https://encore.example.com/api/v1/oauth/lwa/callback",
	})
	require.NoError(t, err)

	pkce := lwa.NewPKCE()
	state := lwa.NewState()

	u, err := url.Parse(c.AuthURL(state, pkce))
	require.NoError(t, err)

	assert.Equal(t, "https://www.amazon.com/ap/oa", u.Scheme+"://"+u.Host+u.Path)

	q := u.Query()
	assert.Equal(t, "my-client", q.Get("client_id"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, state, q.Get("state"))
	assert.Equal(t, "https://encore.example.com/api/v1/oauth/lwa/callback", q.Get("redirect_uri"))
	assert.Equal(t, "alexa::ask:skills:readwrite alexa::ask:models:readwrite alexa::ask:skills:test", q.Get("scope"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, pkce.Challenge(), q.Get("code_challenge"))
	assert.NotContains(t, u.String(), "secret")
}

func TestStateAndPKCEAreUnique(t *testing.T) {
	s1, s2 := lwa.NewState(), lwa.NewState()
	assert.NotEqual(t, s1, s2)

	p1, p2 := lwa.NewPKCE(), lwa.NewPKCE()
	assert.NotEqual(t, p1.Verifier, p2.Verifier)
	assert.GreaterOrEqual(t, len(lwa.NewState()), 32)
}

func TestParseRedirectURL(t *testing.T) {
	code, state, err := lwa.ParseRedirectURL("  http://127.0.0.1:9090/cb?code=ANabc&scope=alexa%3A%3Aask%3Askills%3Areadwrite&state=xyz \n")
	require.NoError(t, err)
	assert.Equal(t, "ANabc", code)
	assert.Equal(t, "xyz", state)

	code, state, err = lwa.ParseRedirectURL("code=c1&state=s1")
	require.NoError(t, err)
	assert.Equal(t, "c1", code)
	assert.Equal(t, "s1", state)

	for name, in := range map[string]string{
		"empty":    "",
		"no code":  "http://127.0.0.1:9090/cb?state=xyz",
		"no state": "http://127.0.0.1:9090/cb?code=abc",
		"denied":   "http://127.0.0.1:9090/cb?error=access_denied&error_description=User+denied&state=xyz",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := lwa.ParseRedirectURL(in)
			require.Error(t, err)
		})
	}

	_, _, err = lwa.ParseRedirectURL("http://127.0.0.1:9090/cb?error=access_denied&state=xyz")
	require.ErrorContains(t, err, "access_denied")
}

func TestExchange(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	tok, err := c.Exchange(t.Context(), smapitest.DefaultAuthCode, lwa.NewPKCE().Verifier)
	require.NoError(t, err)
	assert.Equal(t, smapitest.DefaultRefreshToken, tok.RefreshToken)
	assert.NotEmpty(t, tok.AccessToken)
	assert.Equal(t, 1, fake.Exchanges())

	_, err = c.Exchange(t.Context(), "wrong", "verifier")
	require.ErrorIs(t, err, lwa.ErrReauthorise)
}

func TestInvalidClient(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})

	c, err := lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: "other", ClientSecret: "nope"},
		Endpoint:    fake.Endpoint(),
	})
	require.NoError(t, err)

	_, err = c.FromRefreshToken(t.Context(), smapitest.DefaultRefreshToken)
	require.ErrorIs(t, err, lwa.ErrInvalidClient)
	assert.NotContains(t, err.Error(), "nope")
}

func TestFromRefreshToken(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	tok, err := c.FromRefreshToken(t.Context(), " "+smapitest.DefaultRefreshToken+" ")
	require.NoError(t, err)
	// The fake, like LWA, omits refresh_token from refresh responses; the pasted one is kept.
	assert.Equal(t, smapitest.DefaultRefreshToken, tok.RefreshToken)
	assert.True(t, tok.Valid())

	_, err = c.FromRefreshToken(t.Context(), "Atzr|bogus")
	require.ErrorIs(t, err, lwa.ErrReauthorise)

	_, err = c.FromRefreshToken(t.Context(), "")
	require.Error(t, err)
}

type countingStore struct {
	lwa.MemoryStore

	saves   int
	failErr error
}

func (s *countingStore) Save(ctx context.Context, tok *oauth2.Token) error {
	s.saves++
	if s.failErr != nil {
		return s.failErr
	}

	return s.MemoryStore.Save(ctx, tok)
}

func TestTokenSourceRefreshesAndPersists(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	store := &countingStore{}
	require.NoError(t, store.MemoryStore.Save(t.Context(), &oauth2.Token{
		AccessToken:  "stale",
		RefreshToken: smapitest.DefaultRefreshToken,
		Expiry:       time.Now().Add(-time.Minute),
	}))

	ts := c.TokenSource(store)

	tok, err := ts.TokenContext(t.Context())
	require.NoError(t, err)
	assert.NotEqual(t, "stale", tok.AccessToken)
	assert.Equal(t, 1, fake.Refreshes())
	assert.Equal(t, 1, store.saves)

	saved, err := store.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, tok.AccessToken, saved.AccessToken)
	assert.Equal(t, smapitest.DefaultRefreshToken, saved.RefreshToken)

	again, err := ts.Token()
	require.NoError(t, err)
	assert.Equal(t, tok.AccessToken, again.AccessToken)
	assert.Equal(t, 1, fake.Refreshes())
}

func TestTokenSourceSaveFailureIsTolerated(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	store := &countingStore{}
	require.NoError(t, store.MemoryStore.Save(t.Context(), &oauth2.Token{RefreshToken: smapitest.DefaultRefreshToken}))
	store.failErr = errors.New("disk full")

	tok, err := c.TokenSource(store).TokenContext(t.Context())
	require.NoError(t, err)
	assert.True(t, tok.Valid())
}

func TestTokenSourceNotAuthorised(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})

	_, err := newClient(t, fake).TokenSource(&lwa.MemoryStore{}).TokenContext(t.Context())
	require.ErrorIs(t, err, lwa.ErrNotAuthorised)
}

func TestTokenSourceRevoked(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{RefreshToken: smapitest.DefaultRefreshToken}))

	fake.RevokeRefreshToken()

	ts := c.TokenSource(store)

	_, err := ts.TokenContext(t.Context())
	require.ErrorIs(t, err, lwa.ErrReauthorise)
	assert.Contains(t, err.Error(), "invalid_grant")
	assert.NotContains(t, err.Error(), smapitest.DefaultRefreshToken)
}

// stubToken returns a client whose token endpoint always answers status with body.
func stubToken(t *testing.T, status int, body string) *lwa.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c, err := lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: "id", ClientSecret: "client-secret"},
		Endpoint:    &oauth2.Endpoint{AuthURL: srv.URL + "/ap/oa", TokenURL: srv.URL + "/auth/o2/token"},
	})
	require.NoError(t, err)

	return c
}

func TestExchangeRequiresRefreshToken(t *testing.T) {
	c := stubToken(t, http.StatusOK, `{"access_token":"Atza|a","token_type":"bearer","expires_in":3600}`)

	_, err := c.Exchange(t.Context(), "code", "verifier")
	require.ErrorContains(t, err, "no refresh token")
}

func TestExchangeRequiresVerifier(t *testing.T) {
	_, err := newClient(t, smapitest.New(t, smapitest.Options{})).Exchange(t.Context(), smapitest.DefaultAuthCode, "")
	require.ErrorContains(t, err, "invalid_request")
	require.NotErrorIs(t, err, lwa.ErrReauthorise)
}

func TestTokenErrorMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		status   int
		body     string
		sentinel error
		contains string
	}{
		"invalid grant":       {status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"revoked"}`, sentinel: lwa.ErrReauthorise, contains: "invalid_grant: revoked"},
		"invalid client":      {status: http.StatusUnauthorized, body: `{"error":"invalid_client"}`, sentinel: lwa.ErrInvalidClient, contains: "(invalid_client)"},
		"unauthorized client": {status: http.StatusBadRequest, body: `{"error":"unauthorized_client","error_description":"not allowed"}`, sentinel: lwa.ErrInvalidClient, contains: "unauthorized_client: not allowed"},
		"other OAuth error":   {status: http.StatusBadRequest, body: `{"error":"invalid_scope","error_description":"bad scope"}`, contains: "lwa: refreshing access token: invalid_scope: bad scope"},
		"no OAuth error code": {status: http.StatusBadGateway, body: `<html>bad gateway</html>`, contains: "lwa: refreshing access token: HTTP 502"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := stubToken(t, tc.status, tc.body).FromRefreshToken(t.Context(), "Atzr|secret-refresh")
			require.Error(t, err)

			if tc.sentinel != nil {
				require.ErrorIs(t, err, tc.sentinel)
			} else {
				require.NotErrorIs(t, err, lwa.ErrReauthorise)
				require.NotErrorIs(t, err, lwa.ErrInvalidClient)
			}

			assert.Contains(t, err.Error(), tc.contains)
			assert.NotContains(t, err.Error(), "secret-refresh", "the token request is never quoted")
			assert.NotContains(t, err.Error(), "client-secret")
		})
	}
}

func TestTransportErrorIsWrapped(t *testing.T) {
	c, err := lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: "id", ClientSecret: "secret"},
		Endpoint:    &oauth2.Endpoint{TokenURL: "http://127.0.0.1:1/token"},
	})
	require.NoError(t, err)

	_, err = c.FromRefreshToken(t.Context(), "Atzr|x")
	require.ErrorContains(t, err, "lwa: refreshing access token:")
	require.NotErrorIs(t, err, lwa.ErrReauthorise)
}

type countingTransport struct {
	n atomic.Int32
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)

	return http.DefaultTransport.RoundTrip(r)
}

func TestConfiguredHTTPClientIsUsed(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	opts := fake.Options()
	rt := &countingTransport{}

	c, err := lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: opts.ClientID, ClientSecret: opts.ClientSecret},
		Endpoint:    fake.Endpoint(),
		HTTPClient:  &http.Client{Transport: rt},
	})
	require.NoError(t, err)

	_, err = c.FromRefreshToken(t.Context(), smapitest.DefaultRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, int32(1), rt.n.Load())
}

func TestParseRedirectURLMalformed(t *testing.T) {
	for name, in := range map[string]string{
		"bad URL":          "http://[::1/cb?code=a&state=b",
		"bad query":        "code=%zz&state=b",
		"denied, no state": "?error=access_denied",
		"whitespace only":  " \t\n",
	} {
		t.Run(name, func(t *testing.T) {
			code, state, err := lwa.ParseRedirectURL(in)
			require.Error(t, err)
			assert.Empty(t, code)
			assert.Empty(t, state)
		})
	}

	_, _, err := lwa.ParseRedirectURL("?error=access_denied&error_description=User+denied")
	require.ErrorContains(t, err, "access_denied: User denied")
}

func TestParseRedirectURLIgnoresFragment(t *testing.T) {
	code, state, err := lwa.ParseRedirectURL("https://encore.example.com/cb?code=c%2B1&state=s#frag")
	require.NoError(t, err)
	assert.Equal(t, "c+1", code)
	assert.Equal(t, "s", state)
}

type failingStore struct {
	loadErr error
}

func (s failingStore) Load(context.Context) (*oauth2.Token, error) { return nil, s.loadErr }
func (failingStore) Save(context.Context, *oauth2.Token) error     { return nil }

func TestTokenSourceLoadError(t *testing.T) {
	boom := errors.New("database locked")

	_, err := newClient(t, smapitest.New(t, smapitest.Options{})).TokenSource(failingStore{loadErr: boom}).TokenContext(t.Context())
	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, "lwa: loading token")
}

func TestTokenSourceStoredTokenWithoutRefreshToken(t *testing.T) {
	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{AccessToken: "only-access", Expiry: time.Now().Add(time.Hour)}))

	_, err := newClient(t, smapitest.New(t, smapitest.Options{})).TokenSource(store).TokenContext(t.Context())
	require.ErrorIs(t, err, lwa.ErrNotAuthorised)
}

func TestTokenSourceRotatedTokenMustBeSaved(t *testing.T) {
	c := stubToken(t, http.StatusOK, `{"access_token":"Atza|new","refresh_token":"Atzr|rotated","token_type":"bearer","expires_in":3600}`)

	store := &countingStore{}
	require.NoError(t, store.MemoryStore.Save(t.Context(), &oauth2.Token{RefreshToken: "Atzr|old"}))
	store.failErr = errors.New("disk full")

	_, err := c.TokenSource(store).TokenContext(t.Context())
	require.ErrorContains(t, err, "saving rotated refresh token")
	require.ErrorIs(t, err, store.failErr)
}

func TestTokenSourceResetReloadsFromStore(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{AccessToken: "first", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}))

	ts := c.TokenSource(store)

	tok, err := ts.TokenContext(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "first", tok.AccessToken)

	require.NoError(t, store.Save(t.Context(), &oauth2.Token{AccessToken: "second", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}))

	tok, err = ts.TokenContext(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "first", tok.AccessToken, "the cached token is used until Reset")

	ts.Reset()

	tok, err = ts.TokenContext(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "second", tok.AccessToken)
	assert.Equal(t, 0, fake.Refreshes())
}

func TestTokenSourceConcurrentCallersRefreshOnce(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{RefreshToken: smapitest.DefaultRefreshToken}))

	ts := c.TokenSource(store)

	var wg sync.WaitGroup

	errs := make([]error, 8)

	for i := range errs {
		wg.Go(func() {
			_, errs[i] = ts.TokenContext(t.Context())
		})
	}

	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, 1, fake.Refreshes())
}

func TestMemoryStoreCopies(t *testing.T) {
	store := &lwa.MemoryStore{}

	tok, err := store.Load(t.Context())
	require.NoError(t, err)
	assert.Nil(t, tok)

	orig := &oauth2.Token{AccessToken: "a"}
	require.NoError(t, store.Save(t.Context(), orig))

	orig.AccessToken = "mutated"

	got, err := store.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "a", got.AccessToken)

	got.AccessToken = "changed by caller"

	again, err := store.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "a", again.AccessToken)
}
