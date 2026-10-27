package api

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/auth/oidctest"
)

func TestProtectedRoutesNeedAnAdmin(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.client()

	for _, path := range []string{"/api/v1/overview", "/api/v1/kodi/instances", "/api/v1/settings", "/api/v1/skill", "/api/v1/requests", "/api/v1/events", "/api/v1/devices"} {
		resp := c.do(http.MethodGet, path, nil)
		require.Equal(t, http.StatusUnauthorized, resp.status, path)
		assert.Equal(t, http.StatusUnauthorized, resp.problem(t).Status)
	}

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/v1/nope", nil).problem(t).Status, "unknown routes are problem documents too")
	assert.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/v1/openapi.json", nil).status)
}

func TestSetupLoginAndLogout(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.client()

	var st AuthState

	c.json(http.MethodGet, "/api/v1/auth/state", nil, http.StatusOK, &st)
	assert.True(t, st.SetupRequired)
	assert.False(t, st.Authenticated)
	assert.Nil(t, st.User.V)
	assert.Equal(t, "local", st.Mode)
	assert.NotEmpty(t, st.CSRFToken)
	assert.Nil(t, st.OIDCLoginURL)

	p := c.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{"setup_token": "wrong", "username": "admin", "password": testPassword}).problem(t)
	assert.Equal(t, http.StatusForbidden, p.Status)

	p = c.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{"setup_token": h.setupToken(), "username": "admin", "password": "short"}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	require.NotEmpty(t, p.Errors)
	assert.Equal(t, "body.password", p.Errors[0].Location)

	c.json(http.MethodPost, "/api/v1/setup/admin", map[string]string{"setup_token": h.setupToken(), "username": "admin", "password": testPassword}, http.StatusNoContent, nil)

	c.json(http.MethodGet, "/api/v1/auth/state", nil, http.StatusOK, &st)
	assert.False(t, st.SetupRequired)
	assert.True(t, st.Authenticated)
	require.NotNil(t, st.User.V)
	assert.Equal(t, "admin", st.User.V.Name)

	assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{"setup_token": h.setupToken(), "username": "x", "password": testPassword}).status, "409 once an admin exists")

	c.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, nil)
	c.json(http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent, nil)
	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/v1/overview", nil).status)

	other := h.client()
	assert.Equal(t, http.StatusUnauthorized, other.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong password"}).problem(t).Status)
	other.json(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}, http.StatusNoContent, nil)
	other.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, nil)
}

func TestCSRF(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	body := map[string]string{"name": "Kitchen"}

	token := c.csrf
	c.csrf = ""
	assert.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/v1/rooms", body).problem(t).Status, "no token")

	c.csrf = "forged"
	assert.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/v1/rooms", body).status, "wrong token")

	c.csrf = token
	assert.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/v1/rooms", body, "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.example").status, "cross-origin requests are refused by CrossOriginProtection")

	c.json(http.MethodPost, "/api/v1/rooms", body, http.StatusCreated, nil)

	other := h.admin()
	c.csrf = other.csrf
	assert.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/v1/rooms", map[string]string{"name": "Den"}).status, "another browser's token does not work with this cookie")
}

func TestLoginLockoutOverHTTP(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_AUTH_LOCKOUT_ATTEMPTS=2"}})
	h.admin()

	c := h.client()

	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "nope nope nope"}).status)

	resp := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "nope nope nope"})
	require.Equal(t, http.StatusTooManyRequests, resp.status)

	secs, err := strconv.Atoi(resp.header.Get("Retry-After"))
	require.NoError(t, err)
	assert.Equal(t, 15*60, secs)

	assert.Equal(t, http.StatusTooManyRequests, c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}).status, "locked even with the right password")
}

func TestTrustedHeaders(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{
		"ENCORE_AUTH_MODE=header",
		"ENCORE_SERVER_TRUSTED_PROXIES=172.20.0.0/16",
		"ENCORE_AUTH_HEADER_ADMIN_GROUP=encore-admins",
	}})

	hdr := map[string]string{"Remote-User": "household-admin", "Remote-Groups": "users,encore-admins"}

	rec := h.handlerRequest(http.MethodGet, "/api/v1/overview", "172.20.0.9", hdr)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = h.handlerRequest(http.MethodGet, "/api/v1/overview", "192.0.2.50", hdr)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "spoofed headers from an untrusted peer are ignored")

	rec = h.handlerRequest(http.MethodGet, "/api/v1/overview", "172.20.0.9", map[string]string{"Remote-User": "household-member", "Remote-Groups": "users"})
	assert.Equal(t, http.StatusForbidden, rec.Code, "a user outside the admin group is refused")

	rec = h.handlerRequest(http.MethodGet, "/api/v1/auth/state", "172.20.0.9", hdr)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"user":{"name":"household-admin"}`)
	assert.Contains(t, rec.Body.String(), `"mode":"header"`)
	assert.Contains(t, rec.Body.String(), `"setup_required":false`, "no local setup in header mode")
}

func TestOIDCSignInOverHTTP(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{Username: "household-admin", Groups: []string{"encore-admins"}})

	h := newHarness(t, harnessOpts{noKodi: true, env: []string{
		"ENCORE_AUTH_MODE=oidc",
		"ENCORE_AUTH_OIDC_ISSUER=" + idp.URL,
		"ENCORE_AUTH_OIDC_CLIENT_ID=" + oidctest.DefaultClientID,
		"ENCORE_AUTH_OIDC_CLIENT_SECRET=" + oidctest.DefaultClientSecret,
		"ENCORE_AUTH_OIDC_ADMIN_GROUP=encore-admins",
	}})

	c := h.client()

	var st AuthState

	c.json(http.MethodGet, "/api/v1/auth/state", nil, http.StatusOK, &st)
	require.NotNil(t, st.OIDCLoginURL)
	assert.Equal(t, "/api/v1/auth/oidc/login", *st.OIDCLoginURL)
	assert.False(t, st.LocalLogin)
	assert.False(t, st.SetupRequired)

	assert.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}).status, "password sign-in is off without break glass")

	resp := c.do(http.MethodGet, *st.OIDCLoginURL, nil)
	require.Equal(t, http.StatusFound, resp.status)

	loc, err := url.Parse(resp.header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, idp.URL+"/authorize", loc.Scheme+"://"+loc.Host+loc.Path)
	assert.Equal(t, h.server.URL+"/api/v1/auth/oidc/callback", loc.Query().Get("redirect_uri"))

	// The provider approves and sends the browser back.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, loc.String(), http.NoBody)
	require.NoError(t, err)

	idpResp, err := c.http.Do(req)
	require.NoError(t, err)
	require.NoError(t, idpResp.Body.Close())
	require.Equal(t, http.StatusFound, idpResp.StatusCode)

	cb, err := url.Parse(idpResp.Header.Get("Location"))
	require.NoError(t, err)

	resp = c.do(http.MethodGet, cb.RequestURI(), nil)
	require.Equal(t, http.StatusFound, resp.status, string(resp.body))
	assert.Equal(t, "/", resp.header.Get("Location"))

	c.json(http.MethodGet, "/api/v1/auth/state", nil, http.StatusOK, &st)
	assert.True(t, st.Authenticated)
	assert.Equal(t, "household-admin", st.User.V.Name)

	// Replaying the callback fails: the state was used up.
	resp = c.do(http.MethodGet, cb.RequestURI(), nil)
	require.Equal(t, http.StatusFound, resp.status)
	assert.Contains(t, resp.header.Get("Location"), "/login?error=")
}

func TestTOTPAndPasswordChange(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var enroll struct {
		Secret     string `json:"secret"`
		OTPAuthURL string `json:"otpauth_url"`
	}

	c.json(http.MethodPost, "/api/v1/auth/totp/enroll", nil, http.StatusOK, &enroll)
	assert.NotEmpty(t, enroll.Secret)
	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodPost, "/api/v1/auth/totp/confirm", map[string]string{"code": "000000"}).status)

	assert.Equal(t, http.StatusUnauthorized, c.do(http.MethodPut, "/api/v1/auth/password", map[string]string{"current_password": "wrong password", "new_password": "another long one"}).status)
	c.json(http.MethodPut, "/api/v1/auth/password", map[string]string{"current_password": testPassword, "new_password": "another long one"}, http.StatusNoContent, nil)
	c.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, nil)

	other := h.client()
	assert.Equal(t, http.StatusUnauthorized, other.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}).status)
	other.json(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "another long one"}, http.StatusNoContent, nil)
}
