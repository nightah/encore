package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/basepath"
	"github.com/nightah/encore/internal/store"
)

func TestStartSessionReplacesThePreviousOne(t *testing.T) {
	f := newFixture(t, nil)
	planted := f.setup(t)

	rec := httptest.NewRecorder()
	require.NoError(t, f.svc.StartSession(t.Context(), rec, request(http.MethodPost, "/", planted), "admin"))

	fresh := cookie(rec, SessionCookie)
	require.NotNil(t, fresh)
	assert.NotEqual(t, planted.Value, fresh.Value, "a new token is issued, never the old one reused")
	assert.Equal(t, int(time.Hour.Seconds()), fresh.MaxAge)

	sess, err := f.db.Sessions().Get(t.Context(), hashToken(fresh.Value))
	require.NoError(t, err)
	assert.Equal(t, f.clock().Add(time.Hour), sess.ExpiresAt, "the server-side expiry matches the cookie's")
	assert.Equal(t, "admin", sess.Username)

	_, err = f.db.Sessions().Get(t.Context(), hashToken(planted.Value))
	require.ErrorIs(t, err, store.ErrNotFound, "the previous session on this browser ends")
}

func TestSessionCookieAttributes(t *testing.T) {
	f := newFixture(t, nil)

	r := request(http.MethodPost, "http://encore.lan/")
	r = r.WithContext(basepath.With(r.Context(), "/encore/"))

	rec := httptest.NewRecorder()
	require.NoError(t, f.svc.StartSession(t.Context(), rec, r, "admin"))

	c := cookie(rec, SessionCookie)
	require.NotNil(t, c)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/encore/", c.Path)
	assert.False(t, c.Secure, "plain HTTP installs still work")
	assert.GreaterOrEqual(t, len(c.Value), 40, "the token carries at least 32 random bytes")

	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "192.0.2.66:1"

	rec = httptest.NewRecorder()
	require.NoError(t, f.svc.StartSession(t.Context(), rec, r, "admin"))
	assert.False(t, cookie(rec, SessionCookie).Secure, "an untrusted peer cannot claim HTTPS")
}

func TestSessionTouchIsThrottled(t *testing.T) {
	f := newFixture(t, nil)
	c := f.setup(t)

	f.advance(30 * time.Second)

	rec := httptest.NewRecorder()
	p, err := f.svc.Authenticate(rec, request(http.MethodGet, "/", c))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Nil(t, cookie(rec, SessionCookie), "recent activity is not written back")

	f.advance(30 * time.Second)

	rec = httptest.NewRecorder()
	_, err = f.svc.Authenticate(rec, request(http.MethodGet, "/", c))
	require.NoError(t, err)

	refreshed := cookie(rec, SessionCookie)
	require.NotNil(t, refreshed, "after a minute the expiry slides and the cookie is refreshed")
	assert.Equal(t, c.Value, refreshed.Value)

	sess, err := f.db.Sessions().Get(t.Context(), hashToken(c.Value))
	require.NoError(t, err)
	assert.Equal(t, f.clock().Add(time.Hour), sess.ExpiresAt)
}

func TestAuthenticateWithoutASession(t *testing.T) {
	f := newFixture(t, nil)

	testCases := []struct {
		name    string
		have    *http.Request
		cleared bool
	}{
		{name: "ShouldHaveNothingToClearWithoutACookie", have: request(http.MethodGet, "/")},
		{name: "ShouldHaveNothingToClearWithAnEmptyCookie", have: request(http.MethodGet, "/", &http.Cookie{Name: SessionCookie, Value: ""})},
		{name: "ShouldClearAnUnknownToken", have: request(http.MethodGet, "/", &http.Cookie{Name: SessionCookie, Value: "forged"}), cleared: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			p, err := f.svc.Authenticate(rec, tc.have)
			require.NoError(t, err)
			assert.Nil(t, p)

			if tc.cleared {
				assert.Equal(t, -1, cookie(rec, SessionCookie).MaxAge)
			} else {
				assert.Nil(t, cookie(rec, SessionCookie))
			}
		})
	}
}

func TestSessionStorageErrors(t *testing.T) {
	f := newFixture(t, nil)
	c := f.setup(t)
	require.NoError(t, f.db.Close())

	_, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
	require.Error(t, err, "a broken database is not a signed-out browser")

	require.Error(t, f.svc.StartSession(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin"))
	require.Error(t, f.svc.Logout(httptest.NewRecorder(), request(http.MethodPost, "/", c)))
	require.Error(t, f.svc.EndOtherSessions(t.Context(), nil))
}

func TestLogoutWithoutASession(t *testing.T) {
	f := newFixture(t, nil)

	rec := httptest.NewRecorder()
	require.NoError(t, f.svc.Logout(rec, request(http.MethodPost, "/")))
	assert.Equal(t, -1, cookie(rec, SessionCookie).MaxAge, "the cookie is cleared anyway")
}

func TestEndOtherSessionsWithoutARequestEndsAll(t *testing.T) {
	f := newFixture(t, nil)
	c := f.setup(t)

	require.NoError(t, f.svc.EndOtherSessions(t.Context(), nil))

	p, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", c))
	require.NoError(t, err)
	assert.Nil(t, p)
}
