package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSRFReplacesAWeakCookie(t *testing.T) {
	f := newFixture(t, nil)

	weak := &http.Cookie{Name: csrfCookie, Value: strings.Repeat("a", 31)}

	rec := httptest.NewRecorder()
	token := f.svc.CSRFToken(rec, request(http.MethodGet, "/", weak))

	fresh := cookie(rec, csrfCookie)
	require.NotNil(t, fresh, "a cookie too short to be ours is replaced")
	assert.NotEqual(t, weak.Value, fresh.Value)
	assert.Equal(t, int(csrfCookieTTL.Seconds()), fresh.MaxAge)

	r := request(http.MethodPost, "/", weak)
	r.Header.Set(CSRFHeader, f.svc.csrfFor(weak.Value))
	assert.False(t, f.svc.CheckCSRF(r), "a short cookie is never accepted, even with a matching token")

	r = request(http.MethodPost, "/", fresh)
	r.Header.Set(CSRFHeader, token)
	assert.True(t, f.svc.CheckCSRF(r))
}

func TestCSRFTokensAreBoundToTheServerKey(t *testing.T) {
	a, b := newFixture(t, nil), newFixture(t, nil)

	rec := httptest.NewRecorder()
	token := a.svc.CSRFToken(rec, request(http.MethodGet, "/"))
	c := cookie(rec, csrfCookie)

	r := request(http.MethodPost, "/", c)
	r.Header.Set(CSRFHeader, token)
	assert.True(t, a.svc.CheckCSRF(r))
	assert.False(t, b.svc.CheckCSRF(r), "another server's key gives another token")

	other := &http.Cookie{Name: csrfCookie, Value: c.Value + "x"}
	r = request(http.MethodPost, "/", other)
	r.Header.Set(CSRFHeader, token)
	assert.False(t, a.svc.CheckCSRF(r), "a token only matches its own cookie")
}
