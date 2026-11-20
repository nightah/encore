package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/store"
)

// testAPI is an API with only a logger, for the error mapping helpers.
func testAPI() (*API, *syncBuffer) {
	logs := &syncBuffer{}

	return &API{log: slog.New(slog.NewTextHandler(logs, nil))}, logs
}

// withRequest is a handler context for a request to path, as the middleware builds it.
func withRequest(ctx context.Context, method, path string) context.Context {
	return context.WithValue(ctx, ctxKey{}, &reqInfo{r: httptest.NewRequestWithContext(ctx, method, path, http.NoBody)})
}

func errorModel(t *testing.T, err error) *huma.ErrorModel {
	t.Helper()

	var m *huma.ErrorModel
	require.ErrorAs(t, err, &m)

	return m
}

func TestProblemFor(t *testing.T) {
	t.Parallel()

	teapot := huma.NewError(http.StatusTeapot, "short and stout")

	testCases := []struct {
		name   string
		have   error
		status int
		detail string
	}{
		{name: "ShouldReportNotFound", have: fmt.Errorf("room 3: %w", store.ErrNotFound), status: http.StatusNotFound, detail: "not found"},
		{name: "ShouldReportNoRuntimeAsNotFound", have: fmt.Errorf("kodi 3: %w", app.ErrNoRuntime), status: http.StatusNotFound, detail: "not found"},
		{name: "ShouldReportAConflictWithItsReason", have: fmt.Errorf("%w: “den” is already used by room 3", store.ErrConflict), status: http.StatusConflict, detail: "“den” is already used by room 3"},
		{name: "ShouldHideAConflictFromAConstraint", have: fmt.Errorf("%w: constraint failed: UNIQUE constraint failed: rooms.name", store.ErrConflict), status: http.StatusConflict, detail: "already exists"},
		{name: "ShouldReportABareConflict", have: store.ErrConflict, status: http.StatusConflict, detail: "already exists"},
		{name: "ShouldReportInvalid", have: fmt.Errorf("%w: kodi instance 99 does not exist", store.ErrInvalid), status: http.StatusUnprocessableEntity, detail: "invalid: kodi instance 99 does not exist"},
		{name: "ShouldReportCancelled", have: fmt.Errorf("listing: %w", context.Canceled), status: 499, detail: "request cancelled"},
		{name: "ShouldHideAnythingElse", have: errors.New("database is locked: /var/lib/encore/encore.db"), status: http.StatusInternalServerError, detail: "internal error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, _ := testAPI()

			m := errorModel(t, a.problemFor(t.Context(), tc.have))
			assert.Equal(t, tc.status, m.Status)
			assert.Equal(t, tc.detail, m.Detail)
		})
	}

	t.Run("ShouldReturnNilForNil", func(t *testing.T) {
		t.Parallel()

		a, _ := testAPI()
		assert.NoError(t, a.problemFor(t.Context(), nil))
	})

	t.Run("ShouldPassStatusErrorsThrough", func(t *testing.T) {
		t.Parallel()

		a, _ := testAPI()
		err := a.problemFor(t.Context(), fmt.Errorf("wrapped: %w", teapot))
		require.ErrorIs(t, err, teapot)
		assert.Equal(t, http.StatusTeapot, errorModel(t, err).Status)
	})
}

func TestProblemForLogsUnknownErrorsWithThePath(t *testing.T) {
	t.Parallel()

	a, logs := testAPI()

	err := a.problemFor(withRequest(t.Context(), http.MethodGet, "/api/v1/rooms"), errors.New("disk I/O error"))
	m := errorModel(t, err)
	assert.Equal(t, "internal error", m.Detail, "internals do not leak")
	assert.Contains(t, logs.String(), `msg="admin API request failed" path=/api/v1/rooms error="disk I/O error"`)

	a, logs = testAPI()
	_ = a.problemFor(t.Context(), errors.New("outside a request"))
	assert.Contains(t, logs.String(), `path="" error="outside a request"`)
}

func TestInfoOutsideARequest(t *testing.T) {
	t.Parallel()

	ri := info(t.Context())
	require.NotNil(t, ri, "handlers can always read it")
	assert.Nil(t, ri.r)
	assert.Nil(t, ri.principal)
}

func TestManagedError(t *testing.T) {
	t.Parallel()

	m := errorModel(t, managedError("storage.backups.keep"))
	assert.Equal(t, http.StatusConflict, m.Status)
	assert.Contains(t, m.Detail, "storage.backups.keep is managed by config")
}

func TestFieldError(t *testing.T) {
	t.Parallel()

	m := errorModel(t, fieldError("body.alias", "must contain a letter or digit", "!!"))
	assert.Equal(t, http.StatusUnprocessableEntity, m.Status)
	require.Len(t, m.Errors, 1)
	assert.Equal(t, &huma.ErrorDetail{Location: "body.alias", Message: "must contain a letter or digit", Value: "!!"}, m.Errors[0])
}

func TestResponsesAreNeverCached(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldNotCacheTheAuthState", have: "/api/v1/auth/state"},
		{name: "ShouldNotCacheSettings", have: "/api/v1/settings"},
		{name: "ShouldNotCacheAnUnknownRoute", have: "/api/v1/nope"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, "no-store", c.do(http.MethodGet, tc.have, nil).header.Get("Cache-Control"))
		})
	}

	assert.Equal(t, "no-store", h.client().do(http.MethodGet, "/api/v1/settings", nil).header.Get("Cache-Control"), "refusals too")
}

func TestSafeMethodsNeedNoCSRFToken(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	c.csrf = ""

	testCases := []struct {
		name     string
		method   string
		path     string
		expected int
	}{
		{name: "ShouldAllowGETWithoutAToken", method: http.MethodGet, path: "/api/v1/rooms", expected: http.StatusOK},
		{name: "ShouldAllowHEADWithoutAToken", method: http.MethodHead, path: "/api/v1/rooms", expected: http.StatusOK},
		{name: "ShouldRefuseAnUnsafeMethodWithoutAToken", method: http.MethodDelete, path: "/api/v1/rooms/1", expected: http.StatusForbidden},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, c.do(tc.method, tc.path, nil).status)
		})
	}
}

// brokenStore is a harness signed in through a trusted proxy whose database then fails, so every handler's storage error path runs.
func brokenStore(t *testing.T) (*harness, *client) {
	t.Helper()

	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_AUTH_MODE=header", "ENCORE_SERVER_TRUSTED_PROXIES=127.0.0.0/8,::1/128"}})
	c := h.client()

	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/v1/rooms", nil, "Remote-User", "admin").status, "signed in by the proxy")
	require.NoError(t, h.app.DB().Close())

	return h, c
}

func TestStoreFailuresAreInternalErrors(t *testing.T) {
	h, c := brokenStore(t)

	testCases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{name: "ShouldFailGettingTheAuthState", method: http.MethodGet, path: "/api/v1/auth/state"},
		{name: "ShouldFailGettingSettings", method: http.MethodGet, path: "/api/v1/settings"},
		{name: "ShouldFailPatchingTheTimezone", method: http.MethodPatch, path: "/api/v1/settings", body: map[string]any{"locale": map[string]any{"timezone": "Australia/Melbourne"}}},
		{name: "ShouldFailPatchingBackups", method: http.MethodPatch, path: "/api/v1/settings", body: map[string]any{"storage": map[string]any{"backups": map[string]any{"keep": 3}}}},
		{name: "ShouldFailRotatingSecrets", method: http.MethodPost, path: "/api/v1/settings/secrets/rotate"},
		{name: "ShouldFailListingDevices", method: http.MethodGet, path: "/api/v1/devices"},
		{name: "ShouldFailPatchingADevice", method: http.MethodPatch, path: "/api/v1/devices/1", body: map[string]any{"name": "Kitchen Echo"}},
		{name: "ShouldFailDeletingADevice", method: http.MethodDelete, path: "/api/v1/devices/1"},
		{name: "ShouldFailListingRooms", method: http.MethodGet, path: "/api/v1/rooms"},
		{name: "ShouldFailCreatingARoom", method: http.MethodPost, path: "/api/v1/rooms", body: map[string]any{"name": "Kitchen"}},
		{name: "ShouldFailPatchingARoom", method: http.MethodPatch, path: "/api/v1/rooms/1", body: map[string]any{"name": "Kitchen"}},
		{name: "ShouldFailDeletingARoom", method: http.MethodDelete, path: "/api/v1/rooms/1"},
		{name: "ShouldFailAddingARoomAlias", method: http.MethodPost, path: "/api/v1/rooms/1/aliases", body: map[string]any{"alias": "cooking room"}},
		{name: "ShouldFailDeletingARoomAlias", method: http.MethodDelete, path: "/api/v1/rooms/1/aliases/1"},
		{name: "ShouldFailListingRequests", method: http.MethodGet, path: "/api/v1/requests"},
		{name: "ShouldFailGettingARequest", method: http.MethodGet, path: "/api/v1/requests/1"},
		{name: "ShouldFailReplayingARequest", method: http.MethodPost, path: "/api/v1/requests/1/replay"},
		{name: "ShouldFailDeletingATitleAlias", method: http.MethodDelete, path: "/api/v1/aliases/1"},
		{name: "ShouldFailExportingConfig", method: http.MethodGet, path: "/api/v1/config/export"},
		{name: "ShouldFailImportingConfig", method: http.MethodPost, path: "/api/v1/config/import", body: []byte("storage:\n  backups:\n    keep: 3\n")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hdr := []string{"Remote-User", "admin"}
			if _, ok := tc.body.([]byte); ok {
				hdr = append(hdr, "Content-Type", "application/yaml")
			}

			resp := c.do(tc.method, tc.path, tc.body, hdr...)
			require.Equal(t, http.StatusInternalServerError, resp.status, "a failing database never reads as empty or missing: %s", resp.body)

			p := resp.problem(t)
			assert.Equal(t, "internal error", p.Detail)
			assert.NotContains(t, string(resp.body), "sql", "internals do not leak")
		})
	}

	assert.Contains(t, h.logs.String(), "admin API request failed")
}

func TestSessionLookupFailureIsAnInternalError(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	require.NoError(t, h.app.DB().Close())

	resp := c.do(http.MethodGet, "/api/v1/rooms", nil)
	require.Equal(t, http.StatusInternalServerError, resp.status, "not a 401 that would send the admin to sign in again")
	assert.Equal(t, "internal error", resp.problem(t).Detail)
	assert.Contains(t, h.logs.String(), `msg="authenticating request" component=api path=/api/v1/rooms`)
}
