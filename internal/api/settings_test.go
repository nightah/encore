package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
)

func TestSettingsProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		detail string
	}{
		{name: "managed", err: fmt.Errorf("applying: %w", &managedKeyError{key: "storage.backups.keep"}), status: http.StatusConflict, detail: "storage.backups.keep is managed by config"},
		{name: "invalid", err: &invalidSettingsError{err: errors.New("storage.backups.keep 0 must be at least 1")}, status: http.StatusUnprocessableEntity, detail: "storage.backups.keep 0 must be at least 1"},
		{name: "not overridable", err: fmt.Errorf("%w: data_dir", errNotOverridable), status: http.StatusUnprocessableEntity, detail: "data_dir"},
		{name: "anything else", err: errors.New("disk full"), status: http.StatusInternalServerError, detail: "internal error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, _ := testAPI()

			m := errorModel(t, a.settingsProblem(t.Context(), tt.err))
			assert.Equal(t, tt.status, m.Status)
			assert.Contains(t, m.Detail, tt.detail)
		})
	}
}

func TestKeySource(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "generated", keySource(&config.Config{}))
	assert.Equal(t, "file", keySource(&config.Config{Secrets: config.Secrets{KeyFile: "/run/secrets/encore"}}))
	assert.Equal(t, "config", keySource(&config.Config{Secrets: config.Secrets{Key: "AAAA", KeyFile: "/run/secrets/encore"}}), "a key in config wins over a file")
}

func TestLocalStoragePath(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, filepath.Join(h.cfg.DataDir, blobstore.LocalDirName), s.Storage.Local.Path, "the default is shown, not left blank")
	assert.Equal(t, "local", s.Storage.Backend)
	assert.False(t, s.Storage.WebDAV.HasPassword)
	assert.NotEmpty(t, s.Auth.OIDC.RedirectURI, "derived from the request when not set")
	assert.True(t, strings.HasSuffix(s.Auth.OIDC.RedirectURI, "/api/v1/auth/oidc/callback"), s.Auth.OIDC.RedirectURI)
}

func TestRotateRefusedWithKeyFromAFileVariable(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "encore.key")
	require.NoError(t, os.WriteFile(keyFile, []byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"), 0o600))

	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_SECRETS_KEY_FILE=" + keyFile}})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "config", s.Secrets.KeySource, "ENCORE_SECRETS_KEY_FILE fills secrets.key")
	assert.Contains(t, s.Managed, "secrets.key_source")

	p := c.do(http.MethodPost, "/api/v1/settings/secrets/rotate", nil).problem(t)
	assert.Equal(t, http.StatusConflict, p.Status)
	assert.Contains(t, p.Detail, "ENCORE_SECRETS_KEY_FILE")

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Nil(t, s.Secrets.RotatedAt, "nothing was rotated")
}

func TestStorageFailures(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, nil, 0o600))

	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_STORAGE_LOCAL_PATH=" + notADir}})
	c := h.admin()

	var test StorageTest

	c.json(http.MethodPost, "/api/v1/settings/storage/test", nil, http.StatusOK, &test)
	assert.False(t, test.OK)
	assert.Contains(t, test.Error, "creating storage directory", "the reason is shown to the admin")

	c.json(http.MethodPost, "/api/v1/settings/backups/run", nil, http.StatusAccepted, nil)
	require.Eventually(t, func() bool { return strings.Contains(h.logs.String(), `msg="backup failed"`) }, 10*time.Second, 20*time.Millisecond)

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Nil(t, s.Storage.Backups.LastBackupAt, "a failed backup is not recorded as the last one")
}

func TestBackupToNewStorageFailureIsLogged(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, nil, 0o600))

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"storage": map[string]any{"local": map[string]any{"path": notADir}}}, http.StatusOK, nil)
	require.Eventually(t, func() bool { return strings.Contains(h.logs.String(), "backup to the new storage failed") }, 10*time.Second, 20*time.Millisecond)
}

func TestScheduledBackups(t *testing.T) {
	dir := t.TempDir()

	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_STORAGE_BACKUPS_SCHEDULE=every@1h", "ENCORE_STORAGE_LOCAL_PATH=" + dir}, mutate: func(o *Options) {
		// The last backup ran before a restart, longer ago than the interval.
		require.NoError(t, o.App.DB().Settings().Set(t.Context(), lastBackupSetting, time.Now().Add(-2*time.Hour).UTC()))
	}})
	c := h.admin()

	require.Eventually(t, func() bool {
		found, _ := filepath.Glob(filepath.Join(dir, "Encore", "encore-*.db.gz"))

		return len(found) == 1
	}, 10*time.Second, 50*time.Millisecond, "an overdue interval runs straight away")

	var s Settings

	require.Eventually(t, func() bool {
		c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)

		return s.Storage.Backups.LastBackupAt != nil && time.Since(*s.Storage.Backups.LastBackupAt) < time.Minute
	}, 10*time.Second, 50*time.Millisecond)
	assert.Contains(t, h.logs.String(), "database backed up")
}

func TestScheduledBackupsWaitForTheNextDueTime(t *testing.T) {
	waits := make(chan time.Duration, 100)
	wake := make(chan time.Time)

	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_STORAGE_BACKUPS_SCHEDULE=every@1h", "ENCORE_STORAGE_LOCAL_PATH=" + t.TempDir()}, mutate: func(o *Options) {
		require.NoError(t, o.App.DB().Settings().Set(t.Context(), lastBackupSetting, time.Now().Add(-2*time.Hour).UTC()))
		o.after = func(d time.Duration) <-chan time.Time {
			waits <- d

			return wake
		}
	}})

	backups := func() int { return strings.Count(h.logs.String(), `msg="database backed up"`) }

	// Each step wakes the scheduler and waits for it to settle on its next wait, so every backup it decided on has finished.
	step := func() time.Duration {
		wake <- time.Now()

		return <-waits
	}

	assert.Equal(t, time.Second, <-waits, "an overdue backup is barely waited for")
	assert.Zero(t, backups())

	assert.InDelta(t, time.Hour, step(), float64(time.Minute), "after a backup the next is an interval away")
	assert.Equal(t, 1, backups())

	assert.InDelta(t, time.Hour, step(), float64(time.Minute), "woken early, it keeps waiting for the same due time")
	assert.Equal(t, 1, backups(), "nothing is due yet")

	h.skew.Store(int64(61 * time.Minute))

	assert.InDelta(t, time.Hour, step(), float64(time.Minute))
	assert.Equal(t, 2, backups(), "the next interval came due")
}

func TestScheduledBackupsFollowAChangedSchedule(t *testing.T) {
	waits := make(chan time.Duration, 100)
	wake := make(chan time.Time)

	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_STORAGE_LOCAL_PATH=" + t.TempDir()}, mutate: func(o *Options) {
		db := o.App.DB().Settings()
		require.NoError(t, db.Set(t.Context(), overridesSetting, map[string]any{"storage.backups.schedule": "every@24h"}))
		require.NoError(t, db.Set(t.Context(), lastBackupSetting, time.Now().Add(-2*time.Hour).UTC()))
		o.after = func(d time.Duration) <-chan time.Time {
			waits <- d

			return wake
		}
	}})
	c := h.admin()

	backups := func() int { return strings.Count(h.logs.String(), `msg="database backed up"`) }
	step := func() time.Duration {
		wake <- time.Now()

		return <-waits
	}
	schedule := func(s string) {
		c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"storage": map[string]any{"backups": map[string]any{"schedule": s}}}, http.StatusOK, nil)
	}

	assert.Equal(t, time.Hour, <-waits, "a backup a day away is checked on hourly, to pick up a changed schedule")

	schedule("every@1h")
	assert.Equal(t, time.Second, step(), "the new interval is measured from the last backup, so it is already overdue")
	assert.Zero(t, backups(), "the wake-up that noticed the change was not a due time")

	assert.InDelta(t, time.Hour, step(), float64(time.Minute))
	assert.Equal(t, 1, backups())

	schedule("off")
	h.skew.Store(int64(2 * time.Hour))
	assert.Equal(t, time.Hour, step(), "with backups off it still wakes hourly")
	assert.Equal(t, 1, backups(), "a due time from before backups were turned off is dropped")
}

func TestScheduledBackupFailureIsLogged(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, nil, 0o600))

	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_STORAGE_BACKUPS_SCHEDULE=every@1h", "ENCORE_STORAGE_LOCAL_PATH=" + notADir}, mutate: func(o *Options) {
		require.NoError(t, o.App.DB().Settings().Set(t.Context(), lastBackupSetting, time.Now().Add(-2*time.Hour).UTC()))
	}})

	require.Eventually(t, func() bool { return strings.Contains(h.logs.String(), "scheduled backup failed") }, 10*time.Second, 20*time.Millisecond)
}

func TestSettings(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_STORAGE_BACKUPS_KEEP=3", "ENCORE_SERVER_TRUSTED_PROXIES=10.0.0.0/8"}})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "local", s.Auth.Mode)
	assert.Equal(t, "admin", s.Auth.Local.Username)
	assert.Equal(t, "generated", s.Secrets.KeySource)
	assert.True(t, s.Features.DeepSearch)
	assert.Equal(t, []string{"auth.header.trusted_proxies", "storage.backups.keep"}, s.Managed, "managed keys use the settings document's paths")
	assert.Equal(t, 3, s.Storage.Backups.Keep)

	assert.Equal(t, http.StatusConflict, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"storage": map[string]any{"backups": map[string]any{"keep": 9}}}).problem(t).Status)
	assert.Equal(t, http.StatusConflict, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"auth": map[string]any{"header": map[string]any{"trusted_proxies": []string{"0.0.0.0/0"}}}}).status)

	p := c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"storage": map[string]any{"backups": map[string]any{"schedule": "fortnightly"}}}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Contains(t, p.Detail, "storage.backups.schedule")

	p = c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"locale": map[string]any{"timezone": "Mars/Olympus"}, "storage": map[string]any{"backups": map[string]any{"schedule": "hourly"}}}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "daily@03:00", s.Storage.Backups.Schedule, "a refused patch changes nothing")

	blobs := filepath.Join(t.TempDir(), "blobs")

	// The whole document can be sent back; read-only fields are ignored.
	s.Storage.Local.Path = blobs
	s.Storage.Backups.Schedule = "weekly@sun@04:00"
	s.Locale.Timezone = "Australia/Melbourne"
	s.Features.ShutdownQuits = true

	doc := map[string]any{"storage": map[string]any{"local": s.Storage.Local, "backups": map[string]any{"schedule": s.Storage.Backups.Schedule}, "s3": map[string]any{"secret_key": "s3cret", "has_secret_key": false}}, "locale": s.Locale, "features": s.Features, "managed": s.Managed}
	c.json(http.MethodPatch, "/api/v1/settings", doc, http.StatusOK, &s)
	assert.Equal(t, blobs, s.Storage.Local.Path)
	assert.Equal(t, "weekly@sun@04:00", s.Storage.Backups.Schedule)
	assert.True(t, s.Storage.S3.HasSecretKey, "secrets are write-only")
	assert.Equal(t, "Australia/Melbourne", s.Locale.Timezone)
	assert.True(t, s.Features.ShutdownQuits)
	assert.NotContains(t, string(c.do(http.MethodGet, "/api/v1/settings", nil).body), "s3cret")

	var test StorageTest

	c.json(http.MethodPost, "/api/v1/settings/storage/test", nil, http.StatusOK, &test)
	assert.True(t, test.OK, test.Error)

	c.json(http.MethodPost, "/api/v1/settings/backups/run", nil, http.StatusAccepted, nil)
	require.Eventually(t, func() bool {
		c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)

		return s.Storage.Backups.LastBackupAt != nil
	}, 10*time.Second, 50*time.Millisecond)

	keyID := s.Secrets.KeyID

	c.json(http.MethodPost, "/api/v1/settings/secrets/rotate", nil, http.StatusNoContent, nil)
	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.NotEqual(t, keyID, s.Secrets.KeyID)
	assert.NotNil(t, s.Secrets.RotatedAt)
	assert.True(t, s.Storage.S3.HasSecretKey, "secrets survive rotation")
}

func TestSettingsAuthModeChange(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	other := h.admin()

	p := c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"auth": map[string]any{"mode": "oidc"}}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status, "oidc needs an issuer and client")

	assert.Equal(t, http.StatusConflict, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"auth": map[string]any{"mode": "header", "header": map[string]any{"trusted_proxies": []string{"172.20.0.0/16"}}}}).status, "switching to header mode from outside the proxy would lock the admin out")

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"auth": map[string]any{"mode": "oidc", "oidc": map[string]any{"issuer": "https://auth.example.net", "client_id": "encore", "client_secret": "x"}}}, http.StatusOK, nil)
	assert.Equal(t, http.StatusUnauthorized, other.do(http.MethodGet, "/api/v1/overview", nil).status, "other sessions end when the mode changes")
	c.json(http.MethodGet, "/api/v1/overview", nil, http.StatusOK, nil)
}

func TestRotateRefusedWithOperatorKey(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_SECRETS_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}})
	c := h.admin()

	p := c.do(http.MethodPost, "/api/v1/settings/secrets/rotate", nil).problem(t)
	assert.Equal(t, http.StatusConflict, p.Status)
	assert.Contains(t, p.Detail, "secrets.key")
}

func TestConfigExportImport(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Bedroom", "url": h.kodi.URL(), "password": "kodipw", "language": "de", "timezone": "Australia/Melbourne", "shutdown": "quit"}, http.StatusCreated, nil)
	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"storage": map[string]any{"webdav": map[string]any{"password": "davpw"}}}, http.StatusOK, nil)

	resp := c.do(http.MethodGet, "/api/v1/config/export", nil)
	require.Equal(t, http.StatusOK, resp.status)
	assert.Equal(t, "application/yaml", resp.header.Get("Content-Type"))

	out := string(resp.body)
	assert.True(t, contains(out, "name: Bedroom", "language: de", "timezone: Australia/Melbourne", "shutdown_quits: true", "backups:", "schedule: daily@03:00"), out)
	assert.NotContains(t, out, "kodipw")
	assert.NotContains(t, out, "davpw")

	resp = c.do(http.MethodGet, "/api/v1/config/export?include_secrets=true", nil)
	require.Equal(t, http.StatusOK, resp.status, "a fresh sign-in may export secrets")
	assert.True(t, contains(string(resp.body), "kodipw", "davpw"))

	h.skew.Store(int64(time.Hour))
	assert.Equal(t, http.StatusForbidden, c.do(http.MethodGet, "/api/v1/config/export?include_secrets=true", nil).status, "an old sign-in may not")
	h.skew.Store(0)

	yml := strings.Join([]string{
		"auth:",
		"  session_hours: 24",
		"storage:",
		"  backups:",
		"    keep: 5",
		"data_dir: /elsewhere",
		"kodi:",
		"  - name: Garage",
		"    url: " + h.kodi.URL(),
		"    language: fr",
		"    shutdown_quits: false",
		"",
	}, "\n")

	var res ImportResult

	c.json(http.MethodPost, "/api/v1/config/import", []byte(yml), http.StatusOK, &res, "Content-Type", "application/yaml")
	assert.Equal(t, []string{"auth.session_hours", "kodi Garage (added)", "storage.backups.keep"}, res.Applied)
	assert.Equal(t, []string{"data_dir: cannot be changed in the web interface"}, res.Skipped)

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, 24, s.Auth.Local.SessionHours)
	assert.Equal(t, 5, s.Storage.Backups.Keep)

	var list []Instance

	c.json(http.MethodGet, "/api/v1/kodi/instances", nil, http.StatusOK, &list)

	i := slices.IndexFunc(list, func(in Instance) bool { return in.Name == "Garage" })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "fr", list[i].Language)
	assert.Equal(t, "power_off", list[i].Shutdown)
	assert.Empty(t, list[i].Timezone)

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/config/import", []byte("storage:\n  backend: tape\n"), "Content-Type", "application/yaml").status)
}

func TestServerAndQueueLimitSettings(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "/", s.Server.BasePath)
	assert.Equal(t, 100, s.Features.QueueLimit)

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"server": map[string]any{"base_path": " encore/ "}, "features": map[string]any{"queue_limit": 25}}, http.StatusOK, &s)
	assert.Equal(t, "/encore", s.Server.BasePath, "a typed path is tidied")
	assert.Equal(t, "/encore", h.api.BasePath(), "and applies straight away")
	assert.Equal(t, 25, s.Features.QueueLimit)

	eff, err := h.api.prefs.Effective(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 25, eff.Alexa.QueueLimit)

	p := c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"server": map[string]any{"base_path": "/kodi"}}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Contains(t, p.Detail, "own routes")

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"server": map[string]any{"base_path": "/a b"}}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"features": map[string]any{"queue_limit": 0}}).status)

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"server": map[string]any{"base_path": "/"}}, http.StatusOK, &s)
	assert.Equal(t, "/", s.Server.BasePath)
}

func TestBasePathManagedByConfig(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_SERVER_BASE_PATH=/encore"}})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "/encore", s.Server.BasePath)
	assert.Contains(t, s.Managed, "server.base_path")
	assert.Equal(t, http.StatusConflict, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"server": map[string]any{"base_path": "/other"}}).status)
}

func TestBackupOnNewStorage(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "Encore", s.Storage.Backups.Prefix, "backups go to an Encore folder by default")
	assert.Nil(t, s.Storage.Backups.LastBackupAt)

	// Changing only the schedule is not a new place for backups.
	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"storage": map[string]any{"backups": map[string]any{"schedule": "off"}}}, http.StatusOK, &s)

	dir := t.TempDir()
	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"storage": map[string]any{"local": map[string]any{"path": dir}, "backups": map[string]any{"prefix": "Kodi backups"}}}, http.StatusOK, &s)
	assert.Equal(t, "Kodi backups", s.Storage.Backups.Prefix)

	require.Eventually(t, func() bool {
		found, _ := filepath.Glob(filepath.Join(dir, "Kodi backups", "encore-*.db.gz"))

		return len(found) == 1
	}, 10*time.Second, 50*time.Millisecond, "a new storage location gets a backup straight away")

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.NotNil(t, s.Storage.Backups.LastBackupAt)

	// An empty folder is the top of the storage location, not the default.
	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"storage": map[string]any{"backups": map[string]any{"prefix": ""}}}, http.StatusOK, &s)
	assert.Empty(t, s.Storage.Backups.Prefix)

	require.Eventually(t, func() bool {
		found, _ := filepath.Glob(filepath.Join(dir, "encore-*.db.gz"))

		return len(found) == 1
	}, 10*time.Second, 50*time.Millisecond, "the backup lands at the top")
}

func TestTrustedProxiesDefaultIsEditable(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, []string{"127.0.0.1/32", "::1/128"}, s.Auth.Header.TrustedProxies, "loopback is trusted by default")
	assert.NotContains(t, s.Managed, "auth.header.trusted_proxies", "a default, not config, so the web interface can change it")

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"auth": map[string]any{"header": map[string]any{"trusted_proxies": []string{"127.0.0.1/32", "10.10.50.2/32"}}}}, http.StatusOK, &s)
	assert.Equal(t, []string{"127.0.0.1/32", "10.10.50.2/32"}, s.Auth.Header.TrustedProxies)
}

func TestDefaultLocaleFollowsEnabledLocales(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_ALEXA_LOCALES=en-AU"}})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "en-AU", s.Locale.DefaultLocale, "a single enabled locale is the default")

	p := c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"locale": map[string]any{"default_locale": "en-US"}}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Contains(t, p.Errors[0].Message, "enabled locales")

	p = c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"locale": map[string]any{"default_locale": "tlh-QO"}}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Equal(t, "body.locale.default_locale", p.Errors[0].Location)
	assert.Contains(t, p.Errors[0].Message, "not a locale Encore ships")

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"locale": map[string]any{"default_locale": " en-AU "}}, http.StatusOK, &s)
	assert.Equal(t, "en-AU", s.Locale.DefaultLocale, "a chosen locale is trimmed and saved")

	p = c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"locale": map[string]any{"timezone": "  "}}).problem(t)
	assert.Equal(t, "body.locale.timezone", p.Errors[0].Location, "a blank time zone is not UTC")
}

func TestLogLevelSetting(t *testing.T) {
	level := new(slog.LevelVar)
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.LogLevel = level }})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "info", s.Logging.Level)
	assert.NotContains(t, s.Managed, "logging.level")

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"logging": map[string]any{"level": "debug"}}, http.StatusOK, &s)
	assert.Equal(t, "debug", s.Logging.Level)
	assert.Equal(t, slog.LevelDebug, level.Level(), "applies to the running logger")
	assert.Contains(t, h.logs.String(), "log level changed")

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"logging": map[string]any{"level": "trace"}}).status)
	assert.Equal(t, slog.LevelDebug, level.Level())
}

func TestLogLevelSetByConfig(t *testing.T) {
	level := new(slog.LevelVar)
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_LOG_LEVEL=warn"}, mutate: func(o *Options) { o.LogLevel = level }})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.Equal(t, "warn", s.Logging.Level)
	assert.Contains(t, s.Managed, "logging.level")
	assert.Equal(t, slog.LevelWarn, level.Level(), "the configured level applies at startup")
	assert.Equal(t, http.StatusConflict, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"logging": map[string]any{"level": "debug"}}).status)
}

func TestBreakGlassSetting(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var s Settings

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"auth": map[string]any{"mode": "oidc", "oidc": map[string]any{"issuer": "https://auth.example.net", "client_id": "encore", "client_secret": "x", "break_glass": true}}}, http.StatusOK, &s)
	assert.True(t, s.Auth.OIDC.BreakGlass)
	assert.NotContains(t, s.Managed, "auth.oidc.break_glass")

	other := h.client()

	var st AuthState

	other.json(http.MethodGet, "/api/v1/auth/state", nil, http.StatusOK, &st)
	assert.True(t, st.LocalLogin, "the password form stays available")
	other.json(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}, http.StatusNoContent, nil)

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"auth": map[string]any{"oidc": map[string]any{"break_glass": false}}}, http.StatusOK, &s)
	assert.False(t, s.Auth.OIDC.BreakGlass)
	assert.Equal(t, http.StatusForbidden, h.client().do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}).status)
}

func TestBreakGlassSetByEnvironment(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, env: []string{"ENCORE_BREAK_GLASS=true"}})
	c := h.admin()

	var s Settings

	c.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK, &s)
	assert.True(t, s.Auth.OIDC.BreakGlass)
	assert.Contains(t, s.Managed, "auth.oidc.break_glass")
	assert.Equal(t, http.StatusConflict, c.do(http.MethodPatch, "/api/v1/settings", map[string]any{"auth": map[string]any{"oidc": map[string]any{"break_glass": false}}}).status)
}
