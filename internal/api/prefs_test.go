package api

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/auth"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

type prefsFixture struct {
	dir   string
	db    *store.DB
	sec   *secrets.Store
	prefs *Prefs
}

// newPrefs opens a settings store over a fresh data directory, configured by ENCORE_* variables.
func newPrefs(t *testing.T, env ...string) *prefsFixture {
	t.Helper()

	return openPrefs(t, t.TempDir(), env...)
}

// openPrefs opens the settings store in dir, as a restart with a different configuration would.
func openPrefs(t *testing.T, dir string, env ...string) *prefsFixture {
	t.Helper()

	env = append([]string{"ENCORE_DATA_DIR=" + dir}, env...)

	cfg, err := config.Load(config.Options{Environ: func() []string { return env }})
	require.NoError(t, err)

	db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(dir)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sec, err := secrets.Open(t.Context(), db.SecretRows(), secrets.Options{Key: cfg.Secrets.Key, DataDir: dir})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sec.Close() })

	return &prefsFixture{dir: dir, db: db, sec: sec, prefs: NewPrefs(cfg, db, sec)}
}

func TestPrefsEffectiveStartsFromConfig(t *testing.T) {
	t.Parallel()

	f := newPrefs(t, "ENCORE_STORAGE_BACKUPS_KEEP=4")

	assert.Same(t, f.prefs.cfg, f.prefs.Config())

	eff, err := f.prefs.Effective(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 4, eff.Storage.Backups.Keep)
	assert.Equal(t, "daily@03:00", eff.Storage.Backups.Schedule)
	assert.NotSame(t, f.prefs.Config(), eff, "the startup configuration is never modified")

	again, err := f.prefs.Effective(t.Context())
	require.NoError(t, err)
	assert.Same(t, eff, again, "the merged configuration is cached")
}

func TestPrefsApplySavesAndSurvivesRestart(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	ctx := t.Context()

	eff, err := f.prefs.Apply(ctx, []Change{{Key: "storage.backups.keep", Value: 9}, {Key: "auth.session_hours", Value: 24}})
	require.NoError(t, err)
	assert.Equal(t, 9, eff.Storage.Backups.Keep)
	assert.Equal(t, 24, eff.Auth.SessionHours)
	assert.Equal(t, 168, f.prefs.Config().Auth.SessionHours, "the startup configuration keeps its value")

	got, err := f.prefs.Effective(ctx)
	require.NoError(t, err)
	assert.Same(t, eff, got, "Apply replaces the cached configuration")

	require.NoError(t, f.db.Close())
	require.NoError(t, f.sec.Close())

	restarted := openPrefs(t, f.dir)

	eff, err = restarted.prefs.Effective(ctx)
	require.NoError(t, err)
	assert.Equal(t, 9, eff.Storage.Backups.Keep)
	assert.Equal(t, 24, eff.Auth.SessionHours)
}

func TestPrefsApplyIsAllOrNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		changes []Change
		check   func(t *testing.T, err error)
	}{
		{
			name:    "an invalid value",
			changes: []Change{{Key: "storage.backups.keep", Value: 9}, {Key: "storage.backups.schedule", Value: "fortnightly"}},
			check: func(t *testing.T, err error) {
				var invalid *invalidSettingsError
				require.ErrorAs(t, err, &invalid)
				assert.Contains(t, invalid.Error(), "storage.backups.schedule")
				assert.Error(t, errors.Unwrap(invalid), "the validation error is kept")
			},
		},
		{
			name:    "a key the web interface cannot change",
			changes: []Change{{Key: "storage.backups.keep", Value: 9}, {Key: "data_dir", Value: "/elsewhere"}},
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, errNotOverridable)
				assert.Contains(t, err.Error(), "data_dir")
			},
		},
		{
			name:    "a key managed by config",
			changes: []Change{{Key: "storage.backups.keep", Value: 9}, {Key: "storage.backups.prefix", Value: "Elsewhere"}},
			check: func(t *testing.T, err error) {
				var managed *managedKeyError
				require.ErrorAs(t, err, &managed)
				assert.Equal(t, "storage.backups.prefix is managed by config", managed.Error())
			},
		},
		{
			name:    "a secret with an invalid value beside it",
			changes: []Change{{Key: "storage.s3.secret_key", Value: "s3cret"}, {Key: "auth.session_hours", Value: 0}},
			check: func(t *testing.T, err error) {
				var invalid *invalidSettingsError
				require.ErrorAs(t, err, &invalid)
			},
		},
		{
			name:    "a value of the wrong type",
			changes: []Change{{Key: "storage.backups.keep", Value: "lots"}},
			check: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "applying saved settings")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newPrefs(t, "ENCORE_STORAGE_BACKUPS_PREFIX=Managed")
			ctx := t.Context()

			_, err := f.prefs.Apply(ctx, tt.changes)
			tt.check(t, err)

			o, err := f.prefs.Overrides(ctx)
			require.NoError(t, err)
			assert.Empty(t, o, "nothing is saved")

			eff, err := f.prefs.Effective(ctx)
			require.NoError(t, err)
			assert.Equal(t, 14, eff.Storage.Backups.Keep)
			assert.False(t, f.prefs.HasSecret(ctx, "storage.s3.secret_key"), "no secret is saved either")
		})
	}
}

func TestPrefsNilValueClearsOverride(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	ctx := t.Context()

	_, err := f.prefs.Apply(ctx, []Change{{Key: "storage.backups.keep", Value: 2}})
	require.NoError(t, err)

	eff, err := f.prefs.Apply(ctx, []Change{{Key: "storage.backups.keep", Value: nil}})
	require.NoError(t, err)
	assert.Equal(t, 14, eff.Storage.Backups.Keep, "back to the default")

	o, err := f.prefs.Overrides(ctx)
	require.NoError(t, err)
	assert.NotContains(t, o, "storage.backups.keep")
}

func TestPrefsSecretsAreStoredApart(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	ctx := t.Context()

	for _, key := range []string{"auth.oidc.client_secret", "storage.s3.secret_key", "storage.webdav.password", "storage.s3.bucket"} {
		assert.False(t, f.prefs.HasSecret(ctx, key), key)
	}

	_, err := f.prefs.Apply(ctx, []Change{
		{Key: "auth.oidc.client_secret", Value: "oidc-secret"},
		{Key: "storage.s3.secret_key", Value: "s3-secret"},
		{Key: "storage.webdav.password", Value: "dav-secret"},
	})
	require.NoError(t, err)

	for _, key := range []string{"auth.oidc.client_secret", "storage.s3.secret_key", "storage.webdav.password"} {
		assert.True(t, f.prefs.HasSecret(ctx, key), key)
	}

	assert.False(t, f.prefs.HasSecret(ctx, "storage.s3.bucket"), "not a secret")

	o, err := f.prefs.Overrides(ctx)
	require.NoError(t, err)
	assert.Empty(t, o, "secrets are not saved with the other overrides")

	stored, err := f.sec.Get(ctx, "config/storage.s3.secret_key")
	require.NoError(t, err)
	assert.Equal(t, "s3-secret", string(stored), "but encrypted in the secrets store")

	eff, err := f.prefs.Effective(ctx)
	require.NoError(t, err)
	assert.Equal(t, "dav-secret", eff.Storage.WebDAV.Password)

	// An empty string and nil both remove a secret.
	_, err = f.prefs.Apply(ctx, []Change{{Key: "storage.s3.secret_key", Value: ""}, {Key: "storage.webdav.password", Value: nil}})
	require.NoError(t, err)
	assert.False(t, f.prefs.HasSecret(ctx, "storage.s3.secret_key"))
	assert.False(t, f.prefs.HasSecret(ctx, "storage.webdav.password"))
	assert.True(t, f.prefs.HasSecret(ctx, "auth.oidc.client_secret"), "others are kept")

	_, err = f.sec.Get(ctx, "config/storage.s3.secret_key")
	require.ErrorIs(t, err, secrets.ErrNotFound)

	// After a restart the saved secret is merged back in.
	require.NoError(t, f.db.Close())
	require.NoError(t, f.sec.Close())

	restarted := openPrefs(t, f.dir)
	eff, err = restarted.prefs.Effective(ctx)
	require.NoError(t, err)
	assert.Equal(t, "oidc-secret", eff.Auth.OIDC.ClientSecret)
}

func TestPrefsSecretFromConfigIsManaged(t *testing.T) {
	t.Parallel()

	f := newPrefs(t, "ENCORE_STORAGE_S3_SECRET_KEY=from-env")
	ctx := t.Context()

	assert.True(t, f.prefs.HasSecret(ctx, "storage.s3.secret_key"), "a secret from config counts")

	_, err := f.prefs.Apply(ctx, []Change{{Key: "storage.s3.secret_key", Value: "other"}})

	var managed *managedKeyError
	require.ErrorAs(t, err, &managed)
	assert.Equal(t, "storage.s3.secret_key", managed.key)
}

func TestPrefsShorterListReplacesLonger(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	ctx := t.Context()

	eff, err := f.prefs.Effective(ctx)
	require.NoError(t, err)
	require.Len(t, eff.Auth.OIDC.Scopes, 4)

	tests := []struct {
		key  string
		want []string
		got  func(*config.Config) []string
	}{
		{"auth.oidc.scopes", []string{"openid"}, func(c *config.Config) []string { return c.Auth.OIDC.Scopes }},
		{"server.trusted_proxies", []string{"10.0.0.0/8"}, func(c *config.Config) []string { return c.Server.TrustedProxies }},
		{"alexa.locales", []string{"en-AU"}, func(c *config.Config) []string { return c.Alexa.Locales }},
		{"alexa.content", []string{"movies"}, func(c *config.Config) []string { return c.Alexa.Content }},
	}

	startup := map[string][]string{}
	for _, tt := range tests {
		startup[tt.key] = slices.Clone(tt.got(f.prefs.Config()))
	}

	for _, tt := range tests {
		eff, err := f.prefs.Apply(ctx, []Change{{Key: tt.key, Value: tt.want}})
		require.NoError(t, err, tt.key)
		assert.Equal(t, tt.want, tt.got(eff), tt.key)
		assert.Equal(t, startup[tt.key], tt.got(f.prefs.Config()), "%s: the startup configuration's list is not written through", tt.key)
	}
}

func TestPrefsIgnoresStaleOverrides(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	ctx := t.Context()

	_, err := f.prefs.Apply(ctx, []Change{{Key: "storage.backups.keep", Value: 9}})
	require.NoError(t, err)

	// A key saved by an older version, or not changeable here, is never applied.
	require.NoError(t, f.db.Settings().Set(ctx, overridesSetting, map[string]any{"storage.backups.keep": 9, "data_dir": "/elsewhere"}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.sec.Close())

	restarted := openPrefs(t, f.dir, "ENCORE_STORAGE_BACKUPS_KEEP=3")

	eff, err := restarted.prefs.Effective(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, eff.Storage.Backups.Keep, "config wins over a value saved before it was managed")
	assert.Equal(t, f.dir, eff.DataDir)
}

func TestPrefsOverridesIsACopy(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	ctx := t.Context()

	_, err := f.prefs.Apply(ctx, []Change{{Key: "storage.backups.prefix", Value: "Kodi"}})
	require.NoError(t, err)

	o, err := f.prefs.Overrides(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"storage.backups.prefix": "Kodi"}, o)

	o["storage.backups.prefix"] = "changed"
	o["auth.mode"] = "header"

	again, err := f.prefs.Overrides(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"storage.backups.prefix": "Kodi"}, again)
}

func TestPrefsConcurrentApply(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	ctx := t.Context()

	keys := []string{"storage.backups.prefix", "storage.s3.bucket", "storage.s3.region", "storage.webdav.prefix", "auth.oidc.admin_group", "auth.header.admin_group"}

	var wg sync.WaitGroup

	for i, key := range keys {
		wg.Go(func() {
			_, err := f.prefs.Apply(ctx, []Change{{Key: key, Value: fmt.Sprintf("v%d", i)}})
			assert.NoError(t, err)
		})
	}

	wg.Wait()

	o, err := f.prefs.Overrides(ctx)
	require.NoError(t, err)
	assert.Len(t, o, len(keys), "no change is lost to another")
}

func TestPrefsAuthSettings(t *testing.T) {
	t.Parallel()

	f := newPrefs(t,
		"ENCORE_AUTH_SESSION_HOURS=12",
		"ENCORE_AUTH_LOCKOUT_ATTEMPTS=3",
		"ENCORE_AUTH_LOCKOUT_MINUTES=20",
		"ENCORE_SERVER_TRUSTED_PROXIES=10.1.2.3/8, 2001:db8::1/32",
		"ENCORE_AUTH_HEADER_ADMIN_GROUP=encore-admins",
	)

	s, err := f.prefs.AuthSettings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, auth.ModeLocal, s.Mode)
	assert.Equal(t, 12*time.Hour, s.SessionTTL)
	assert.Equal(t, 3, s.LockoutAttempts)
	assert.Equal(t, 20*time.Minute, s.LockoutWindow)
	assert.Equal(t, "encore-admins", s.Header.AdminGroup)
	assert.Equal(t, []string{"openid", "profile", "email", "groups"}, s.OIDC.Scopes)
	assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8::/32")}, s.TrustedProxies, "proxies are masked to their network")
}

func TestPrefsAuthSettingsDefaultsToLocal(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	f.prefs.cfg.Auth.Mode = ""

	s, err := f.prefs.AuthSettings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, auth.ModeLocal, s.Mode, "no mode means password sign-in")
}

func TestPrefsManagedList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{name: "nothing managed", want: []string{}},
		{
			name: "keys shown under another path",
			env:  []string{"ENCORE_AUTH_SESSION_HOURS=12", "ENCORE_ALEXA_DEEP_SEARCH=false", "ENCORE_LOG_LEVEL=warn", "ENCORE_BREAK_GLASS=true"},
			want: []string{"auth.local.session_hours", "auth.oidc.break_glass", "features.deep_search", "logging.level"},
		},
		{
			name: "auth and storage keys keep their path",
			env:  []string{"ENCORE_AUTH_OIDC_ISSUER=https://auth.example.net", "ENCORE_STORAGE_S3_BUCKET=encore"},
			want: []string{"auth.oidc.issuer", "storage.s3.bucket"},
		},
		{
			name: "keys the document does not show are left out",
			env:  []string{"ENCORE_SERVER_ADDRESS=:9000", "ENCORE_LOG_FORMAT=json"},
			want: []string{},
		},
		{
			name: "the secrets key shows as its source",
			env:  []string{"ENCORE_SECRETS_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
			want: []string{"secrets.key_source"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newPrefs(t, tt.env...)
			assert.Equal(t, tt.want, f.prefs.ManagedList())

			for _, path := range tt.want {
				assert.NotEmpty(t, path)
			}
		})
	}
}

func TestPrefsWhenTheStoreFails(t *testing.T) {
	t.Parallel()

	f := newPrefs(t, "ENCORE_SERVER_BASE_PATH=/encore")
	ctx := t.Context()

	require.NoError(t, f.db.Close())

	_, err := f.prefs.Effective(ctx)
	require.Error(t, err)

	_, err = f.prefs.AuthSettings(ctx)
	require.Error(t, err)

	_, err = f.prefs.Overrides(ctx)
	require.Error(t, err)

	_, err = f.prefs.Apply(ctx, []Change{{Key: "storage.backups.keep", Value: 2}})
	require.Error(t, err)

	assert.False(t, f.prefs.HasSecret(ctx, "storage.s3.secret_key"))
	assert.Equal(t, "/encore", f.prefs.BasePath(), "the startup base path keeps the interface reachable")
}

func TestPrefsBasePathFollowsSavedValue(t *testing.T) {
	t.Parallel()

	f := newPrefs(t)
	assert.Equal(t, "/", f.prefs.BasePath())

	_, err := f.prefs.Apply(t.Context(), []Change{{Key: "server.base_path", Value: "/encore"}})
	require.NoError(t, err)
	assert.Equal(t, "/encore", f.prefs.BasePath())
}
