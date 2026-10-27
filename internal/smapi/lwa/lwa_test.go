package lwa_test

import (
	"context"
	"errors"
	"net/url"
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
