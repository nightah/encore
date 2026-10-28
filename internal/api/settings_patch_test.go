package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/config"
)

func TestBasePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"", "/"},
		{"/", "/"},
		{"  ", "/"},
		{"encore", "/encore"},
		{"encore/", "/encore"},
		{" /encore/ ", "/encore"},
		{"//encore//", "/encore"},
		{"/apps/encore", "/apps/encore"},
		{"/música", "/música"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, basePath(tt.in), "%q", tt.in)
	}
}

func TestPropBasePathIsTidy(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		// Spaces inside a path are refused by validation later, so only the ends get them here.
		in := rapid.StringMatching(` {0,2}/{0,2}([a-z]{1,6}(/[a-z]{1,6}){0,2})?/{0,2} {0,2}`).Draw(t, "in")
		got := basePath(in)

		assert.True(t, strings.HasPrefix(got, "/"), "leading slash: %q", got)
		assert.True(t, got == "/" || !strings.HasSuffix(got, "/"), "no trailing slash: %q", got)
		assert.Equal(t, got, basePath(got), "tidying twice changes nothing")
	})
}

// fillPatch sets every field of a settings patch, recursively, so no field can be left out of changes unnoticed.
func fillPatch(v reflect.Value) int {
	n := 0

	for i := range v.NumField() {
		f := v.Field(i)
		if !f.CanSet() {
			continue
		}

		switch e := f.Type().Elem(); {
		case e.Kind() == reflect.Struct:
			f.Set(reflect.New(e))
			n += fillPatch(f.Elem())
		case e.Kind() == reflect.String:
			f.Set(reflect.ValueOf(new(" value ")))
			n++
		case e.Kind() == reflect.Int:
			f.Set(reflect.ValueOf(new(5)))
			n++
		case e.Kind() == reflect.Bool:
			f.Set(reflect.ValueOf(new(true)))
			n++
		case e.Kind() == reflect.Slice:
			f.Set(reflect.ValueOf(&[]string{" a ", "", "  ", "b"}))
			n++
		}
	}

	return n
}

func TestSettingsPatchChanges(t *testing.T) {
	t.Parallel()

	var p SettingsPatch

	fields := fillPatch(reflect.ValueOf(&p).Elem())

	ch := p.changes()
	// The locale is saved outside the config overrides.
	assert.Len(t, ch, fields-2, "every patch field becomes a change")

	seen := map[string]bool{}

	for _, c := range ch {
		assert.True(t, isOverridable(c.Key), "%s is a key the web interface may change", c.Key)
		assert.False(t, seen[c.Key], "%s appears once", c.Key)
		seen[c.Key] = true

		switch v := c.Value.(type) {
		case string:
			if c.Key == "server.base_path" {
				assert.Equal(t, "/value", v)
			} else {
				assert.Equal(t, "value", v, "%s is trimmed", c.Key)
			}
		case []string:
			assert.Equal(t, []string{"a", "b"}, v, "%s entries are trimmed and blanks dropped", c.Key)
		case int:
			assert.Equal(t, 5, v)
		case bool:
			assert.True(t, v)
		default:
			t.Errorf("%s has an unexpected value %#v", c.Key, v)
		}
	}

	for _, key := range []string{"auth.break_glass", "server.trusted_proxies", "alexa.queue_limit", "log.level", "storage.s3.secret_key", "storage.webdav.password", "auth.session_hours"} {
		assert.True(t, seen[key], key)
	}
}

func TestSettingsPatchChangesOnlyWhatIsSent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, SettingsPatch{}.changes())
	assert.Empty(t, SettingsPatch{Auth: &AuthPatch{}, Storage: &StoragePatch{S3: &S3Patch{}}, Server: &ServerPatch{}}.changes(), "empty sections change nothing")

	empty := ""
	none := []string{}

	ch := SettingsPatch{
		Storage: &StoragePatch{WebDAV: &WebDAVPatch{Password: &empty}},
		Auth:    &AuthPatch{OIDC: &OIDCAuthPatch{Scopes: &none}},
	}.changes()
	assert.Equal(t, []Change{{Key: "auth.oidc.scopes", Value: []string{}}, {Key: "storage.webdav.password", Value: ""}}, ch, "an empty value is a change too: it clears a secret or a list")
}

func TestBackupTarget(t *testing.T) {
	t.Parallel()

	base := config.Storage{
		Backend: config.BackendLocal,
		Local:   config.StorageLocal{Path: "/srv/encore"},
		S3:      config.StorageS3{Endpoint: "https://s3.example.net", Region: "ap-southeast-2", Bucket: "encore", Prefix: "home"},
		WebDAV:  config.StorageWebDAV{URL: "https://dav.example.net", Prefix: "home"},
		Backups: config.Backups{Schedule: "daily@03:00", Keep: 7, Prefix: "Encore"},
	}

	tests := []struct {
		name  string
		edit  func(s *config.Storage)
		moved bool
	}{
		{"nothing", func(*config.Storage) {}, false},
		{"the schedule", func(s *config.Storage) { s.Backups.Schedule = "off" }, false},
		{"how many to keep", func(s *config.Storage) { s.Backups.Keep = 3 }, false},
		{"another backend's settings", func(s *config.Storage) { s.S3.Bucket = "other" }, false},
		{"the local path", func(s *config.Storage) { s.Local.Path = "/mnt/backups" }, true},
		{"the backups folder", func(s *config.Storage) { s.Backups.Prefix = "" }, true},
		{"the backend", func(s *config.Storage) { s.Backend = config.BackendS3 }, true},
	}

	for _, tt := range tests {
		s := base
		tt.edit(&s)
		assert.Equal(t, tt.moved, backupTarget(&s) != backupTarget(&base), tt.name)
	}

	s3 := base
	s3.Backend = config.BackendS3

	for name, edit := range map[string]func(s *config.Storage){
		"endpoint": func(s *config.Storage) { s.S3.Endpoint = "https://other.example.net" },
		"region":   func(s *config.Storage) { s.S3.Region = "us-east-1" },
		"bucket":   func(s *config.Storage) { s.S3.Bucket = "other" },
		"prefix":   func(s *config.Storage) { s.S3.Prefix = "other" },
	} {
		s := s3
		edit(&s)
		assert.NotEqual(t, backupTarget(&s3), backupTarget(&s), "s3 %s", name)
	}

	other := s3
	other.Local.Path = "/elsewhere"
	assert.Equal(t, backupTarget(&s3), backupTarget(&other), "the local path does not matter for s3")

	dav := base
	dav.Backend = config.BackendWebDAV

	for name, edit := range map[string]func(s *config.Storage){
		"url":    func(s *config.Storage) { s.WebDAV.URL = "https://other.example.net" },
		"prefix": func(s *config.Storage) { s.WebDAV.Prefix = "other" },
	} {
		s := dav
		edit(&s)
		assert.NotEqual(t, backupTarget(&dav), backupTarget(&s), "webdav %s", name)
	}
}

func TestGuardAuthChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     []string
		peer    string
		changes []Change
		refused bool
	}{
		{name: "not header mode", peer: "192.0.2.10:5000", changes: []Change{{Key: "auth.mode", Value: "oidc"}}},
		{name: "unrelated change", peer: "192.0.2.10:5000", changes: []Change{{Key: "storage.backups.keep", Value: 3}}},
		{name: "header mode from a trusted proxy", peer: "127.0.0.1:5000", changes: []Change{{Key: "auth.mode", Value: "header"}}},
		{name: "header mode from IPv6 loopback", peer: "[::1]:5000", changes: []Change{{Key: "auth.mode", Value: "header"}}},
		{name: "header mode from elsewhere", peer: "192.0.2.10:5000", changes: []Change{{Key: "auth.mode", Value: "header"}}, refused: true},
		{name: "header mode with the new proxy list", peer: "10.0.0.5:5000", changes: []Change{{Key: "auth.mode", Value: "header"}, {Key: "server.trusted_proxies", Value: []string{"10.0.0.0/8"}}}},
		{name: "an IPv4-mapped peer", peer: "[::ffff:10.0.0.5]:5000", changes: []Change{{Key: "auth.mode", Value: "header"}, {Key: "server.trusted_proxies", Value: []string{"10.0.0.0/8"}}}},
		{name: "invalid proxy entries match nothing", peer: "10.0.0.5:5000", changes: []Change{{Key: "auth.mode", Value: "header"}, {Key: "server.trusted_proxies", Value: []string{"10.0.0.0", "not a cidr"}}}, refused: true},
		{name: "dropping the proxy in header mode", env: []string{"ENCORE_AUTH_MODE=header"}, peer: "127.0.0.1:5000", changes: []Change{{Key: "server.trusted_proxies", Value: []string{"10.0.0.0/8"}}}, refused: true},
		{name: "leaving header mode", env: []string{"ENCORE_SERVER_TRUSTED_PROXIES=10.0.0.0/8"}, peer: "192.0.2.10:5000", changes: []Change{{Key: "auth.mode", Value: "local"}}},
		{name: "a peer that is not an address", peer: "@", changes: []Change{{Key: "auth.mode", Value: "header"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newPrefs(t, tt.env...)
			a := &API{prefs: f.prefs}

			r := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/settings", http.NoBody)
			r.RemoteAddr = tt.peer

			err := a.guardAuthChange(context.WithValue(t.Context(), ctxKey{}, &reqInfo{r: r}), tt.changes)
			if !tt.refused {
				require.NoError(t, err)

				return
			}

			m := errorModel(t, err)
			assert.Equal(t, http.StatusConflict, m.Status)
			assert.Contains(t, m.Detail, "lock you out")
		})
	}

	t.Run("outside a request", func(t *testing.T) {
		t.Parallel()

		a := &API{prefs: newPrefs(t).prefs}
		assert.NoError(t, a.guardAuthChange(t.Context(), []Change{{Key: "auth.mode", Value: "header"}}), "nothing to check against")
	})
}

func TestSetLogLevel(t *testing.T) {
	t.Parallel()

	level := new(slog.LevelVar)
	logs := &syncBuffer{}
	a := &API{opts: Options{LogLevel: level}, log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: level}))}

	steps := []struct {
		to     string
		want   slog.Level
		logged bool
	}{
		{to: "warn", want: slog.LevelWarn, logged: true},
		{to: "debug", want: slog.LevelDebug, logged: true},
		{to: "error", want: slog.LevelError, logged: true},
		{to: "warn", want: slog.LevelWarn, logged: false},
		{to: "warn", want: slog.LevelWarn, logged: false},
		{to: "trace", want: slog.LevelWarn, logged: false},
		{to: "info", want: slog.LevelInfo, logged: true},
	}

	for _, s := range steps {
		before := strings.Count(logs.String(), "log level changed")

		a.setLogLevel(&config.Config{Log: config.Log{Level: s.to}})
		assert.Equal(t, s.want, level.Level(), "to %s", s.to)
		assert.Equal(t, s.logged, strings.Count(logs.String(), "log level changed") > before, "to %s: recorded when either level shows info", s.to)
	}

	assert.Contains(t, logs.String(), `msg="log level changed" level=debug`)

	(&API{}).setLogLevel(&config.Config{Log: config.Log{Level: "debug"}})
}
