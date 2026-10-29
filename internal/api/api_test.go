package api

import (
	"net/http"
	"regexp"
	"slices"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNeedsAnApp(t *testing.T) {
	t.Parallel()

	a, err := New(t.Context(), Options{})
	require.EqualError(t, err, "api: App is required")
	assert.Nil(t, a)
}

func TestNewFillsInDefaults(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.Logger, o.Now, o.HTTPClient, o.Sources = nil, nil, nil, nil
		o.Heartbeat, o.StatusInterval = 0, 0
	}})

	assert.NotNil(t, h.api.Auth())
	assert.Equal(t, "/", h.api.BasePath())

	var st AuthState

	h.client().json(http.MethodGet, "/api/v1/auth/state", nil, http.StatusOK, &st)
	assert.True(t, st.SetupRequired)

	setup, err := h.api.Auth().SetupRequired(t.Context())
	require.NoError(t, err)
	assert.True(t, setup, "Auth is the service the API signs admins in with")
}

// TestRoutesNeedSignInUnlessPublic walks the OpenAPI document, so a new route is checked without being listed here.
func TestRoutesNeedSignInUnlessPublic(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.client()

	param := regexp.MustCompile(`\{[^}]+\}`)

	var public []string

	for path, item := range h.api.OpenAPI().Paths {
		for method, op := range map[string]*huma.Operation{http.MethodGet: item.Get, http.MethodPost: item.Post, http.MethodPatch: item.Patch, http.MethodPut: item.Put, http.MethodDelete: item.Delete} {
			if op == nil {
				continue
			}

			if op.Metadata[metaPublic] == true {
				public = append(public, op.OperationID)
				assert.Empty(t, op.Security, "%s is public, so documents no session", op.OperationID)

				continue
			}

			assert.Equal(t, []map[string][]string{{"session": {}}}, op.Security, "%s documents that it needs a session", op.OperationID)

			resp := c.do(method, param.ReplaceAllString(path, "1"), nil)
			assert.Equal(t, http.StatusUnauthorized, resp.status, "%s %s needs a signed-in admin", method, path)
		}
	}

	slices.Sort(public)
	assert.Equal(t, []string{"checkReachability", "createAdmin", "getAuthState", "login", "lwaCallback", "oidcCallback", "oidcLogin", "reportClientError"}, public, "only sign-in, setup, OAuth callbacks and error reports work signed out")
}
