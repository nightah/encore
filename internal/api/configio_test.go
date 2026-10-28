package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goyaml "go.yaml.in/yaml/v3"
)

func importYAML(t *testing.T, c *client, yml string, status int) (ImportResult, response) {
	t.Helper()

	var res ImportResult

	resp := c.do(http.MethodPost, "/api/v1/config/import", []byte(yml), "Content-Type", "application/yaml")
	require.Equal(t, status, resp.status, string(resp.body))

	if status == http.StatusOK {
		require.NoError(t, goyaml.Unmarshal(resp.body, &res))
	}

	return res, resp
}

func TestImportRefusesMalformedFiles(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	tests := []struct {
		name     string
		yml      string
		location string
	}{
		{name: "not YAML", yml: "storage: [unclosed\n", location: "body"},
		{name: "kodi is not a list", yml: "kodi: Lounge\n", location: "body.kodi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, resp := importYAML(t, c, tt.yml, http.StatusUnprocessableEntity)

			p := resp.problem(t)
			require.Len(t, p.Errors, 1)
			assert.Equal(t, tt.location, p.Errors[0].Location)
		})
	}

	_, resp := importYAML(t, c, "storage:\n  backups:\n    prefix: \""+strings.Repeat("x", maxImportBytes)+"\"\n", http.StatusRequestEntityTooLarge)
	assert.NotEmpty(t, resp.body)

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "Encore", s.Storage.Backups.Prefix, "a refused import changes nothing")
}

func TestImportAFileWithNothingInIt(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	res, _ := importYAML(t, c, "# nothing here yet\n", http.StatusOK)
	assert.Equal(t, ImportResult{Applied: []string{}, Skipped: []string{}}, res)

	assert.Equal(t, http.StatusBadRequest, c.do(http.MethodPost, "/api/v1/config/import", []byte{}, "Content-Type", "application/yaml").status, "an empty body is not a file")
}

func TestImportSkipsManagedKeysAndInstances(t *testing.T) {
	h := newHarness(t, harnessOpts{env: []string{"ENCORE_STORAGE_BACKUPS_KEEP=3"}})
	c := h.admin()

	yml := strings.Join([]string{
		"storage:",
		"  backups:",
		"    keep: 9",
		"    prefix: Kodi backups",
		"kodi:",
		"  - name: Lounge",
		"    url: http://192.0.2.1:8080/jsonrpc",
		"  - url: http://192.0.2.2:8080/jsonrpc",
		"  - name: Garage",
		"    url: ftp://192.0.2.3/",
		"",
	}, "\n")

	res, _ := importYAML(t, c, yml, http.StatusOK)
	assert.Equal(t, []string{"storage.backups.prefix"}, res.Applied)
	require.Len(t, res.Skipped, 4)
	assert.Equal(t, "kodi : has no name", res.Skipped[0])
	assert.True(t, strings.HasPrefix(res.Skipped[1], "kodi Garage: validation failed; body.url must be an http(s) URL"), "a refused instance says why in one line: %s", res.Skipped[1])
	assert.Equal(t, "kodi Lounge: managed by config", res.Skipped[2])
	assert.Equal(t, "storage.backups.keep: managed by config", res.Skipped[3])

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, 3, s.Storage.Backups.Keep)
	assert.Equal(t, "Kodi backups", s.Storage.Backups.Prefix)

	var list []Instance

	c.json(http.MethodGet, "/api/v1/kodi/instances", nil, http.StatusOK, &list)
	require.Len(t, list, 1)
	assert.Equal(t, h.kodi.URL(), list[0].URL, "the managed instance is untouched")
}

func TestImportUpdatesInstancesByName(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var bedroom, den Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Bedroom", "url": h.kodi.URL(), "language": "de"}, http.StatusCreated, &bedroom)
	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Den", "url": h.kodi.URL()}, http.StatusCreated, &den)
	require.True(t, bedroom.DefaultForUnlinked, "the first instance answers unlinked Echos")

	yml := strings.Join([]string{
		"kodi:",
		"  - name: bedroom",
		"    url: " + h.kodi.URL(),
		"    password: kodipw",
		"    shutdown_quits: true",
		"  - name: Den",
		"    url: " + h.kodi.URL(),
		"    default: true",
		"    timezone: Australia/Melbourne",
		"",
	}, "\n")

	res, _ := importYAML(t, c, yml, http.StatusOK)
	assert.Equal(t, []string{"kodi Den (updated)", "kodi bedroom (updated)"}, res.Applied, "names match whatever their case")
	assert.Empty(t, res.Skipped)

	res, _ = importYAML(t, c, "kodi:\n  - name: Den\n    url: ftp://192.0.2.3/\n", http.StatusOK)
	assert.Empty(t, res.Applied)
	require.Len(t, res.Skipped, 1)
	assert.True(t, strings.HasPrefix(res.Skipped[0], "kodi Den: validation failed; body.url"), res.Skipped[0])

	var list []Instance

	c.json(http.MethodGet, "/api/v1/kodi/instances", nil, http.StatusOK, &list)
	require.Len(t, list, 2, "nothing is added twice")

	byName := map[string]Instance{}
	for _, in := range list {
		byName[in.Name] = in
	}

	assert.Equal(t, "en", byName["bedroom"].Language, "a missing language is English")
	assert.Equal(t, "quit", byName["bedroom"].Shutdown)
	assert.True(t, byName["bedroom"].HasPassword)
	assert.False(t, byName["bedroom"].DefaultForUnlinked)
	assert.True(t, byName["Den"].DefaultForUnlinked, "default moves to the instance the file marks")
	assert.Equal(t, "Australia/Melbourne", byName["Den"].Timezone)
	assert.Equal(t, "inherit", byName["Den"].Shutdown)
}

func TestExportRoundTrip(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Bedroom", "url": h.kodi.URL(), "password": "kodipw", "timezone": "Australia/Melbourne"}, http.StatusCreated, nil)
	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{
		"auth":     map[string]any{"oidc": map[string]any{"scopes": []string{"openid", "email"}, "client_secret": "oidcpw"}},
		"storage":  map[string]any{"backups": map[string]any{"keep": 4, "schedule": "weekly@sun@04:00"}},
		"features": map[string]any{"deep_search": false},
	}, http.StatusOK, nil)

	resp := c.do(http.MethodGet, "/api/v1/config/export?include_secrets=true", nil)
	require.Equal(t, http.StatusOK, resp.status)
	assert.Equal(t, `attachment; filename="encore.yml"`, resp.header.Get("Content-Disposition"))
	assert.True(t, strings.HasPrefix(string(resp.body), "# Encore configuration exported "), "the file says when it was made")
	assert.Contains(t, string(resp.body), "This file contains secrets; keep it safe.")

	plain := c.do(http.MethodGet, "/api/v1/config/export", nil)
	assert.Contains(t, string(plain.body), "Secrets are not included.")
	assert.NotContains(t, string(plain.body), "oidcpw")
	assert.NotContains(t, string(plain.body), "client_secret")

	fresh := newHarness(t, harnessOpts{noKodi: true})
	fc := fresh.admin()

	res, _ := importYAML(t, fc, string(resp.body), http.StatusOK)
	assert.Contains(t, res.Applied, "kodi Bedroom (added)")
	assert.Empty(t, res.Skipped, "an export imports cleanly")

	var before, after Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &before)
	fc.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &after)
	assert.Equal(t, before.Storage.Backups.Keep, after.Storage.Backups.Keep)
	assert.Equal(t, before.Storage.Backups.Schedule, after.Storage.Backups.Schedule)
	assert.Equal(t, []string{"openid", "email"}, after.Auth.OIDC.Scopes)
	assert.True(t, after.Auth.OIDC.HasClientSecret)
	assert.True(t, after.Features.DeepSearch, "features are outside the export's documented scope (auth, storage, alexa.locales, trusted proxies and Kodi instances)")

	var list []Instance

	fc.json(http.MethodGet, "/api/v1/kodi/instances", nil, http.StatusOK, &list)
	require.Len(t, list, 1)
	assert.True(t, list[0].HasPassword)
	assert.Equal(t, "Australia/Melbourne", list[0].Timezone)
}

func TestExportSecretsThroughATrustedProxy(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_AUTH_MODE=header", "ENCORE_SERVER_TRUSTED_PROXIES=127.0.0.0/8,::1/128"}})
	c := h.client()

	h.skew.Store(int64(24 * 60 * 60 * 1e9))

	resp := c.do(http.MethodGet, "/api/v1/config/export?include_secrets=true", nil, "Remote-User", "admin")
	assert.Equal(t, http.StatusOK, resp.status, "the proxy signs every request in afresh")
}

func TestImportErr(t *testing.T) {
	t.Parallel()

	plain := errors.New("disk full")
	assert.Same(t, plain, importErr(plain), "other errors pass through")

	err := importErr(huma.Error422UnprocessableEntity("validation failed",
		&huma.ErrorDetail{Location: "body.url", Message: "must be an http(s) URL"},
		&huma.ErrorDetail{Location: "body.name", Message: "must not be empty"}))
	require.EqualError(t, err, "validation failed; body.url must be an http(s) URL; body.name must not be empty")

	require.EqualError(t, importErr(huma.Error409Conflict(`a Kodi instance named "Lounge" is declared in config`)), `a Kodi instance named "Lounge" is declared in config`)
}

func TestFlatValuesAreOverridable(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)

	for _, withSecrets := range []bool{false, true} {
		keys := []string{}

		for k := range flatValues(f.prefs.Config(), withSecrets) {
			assert.True(t, isOverridable(k), "%s is exported, so it must import", k)
			keys = append(keys, k)
		}

		hasSecret := slices.Contains(keys, "storage.s3.secret_key")
		assert.Equal(t, withSecrets, hasSecret)
	}
}
