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

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldTreatEmptyAsTheRoot", expected: "/"},
		{name: "ShouldKeepTheRoot", have: "/", expected: "/"},
		{name: "ShouldTreatBlankAsTheRoot", have: "  ", expected: "/"},
		{name: "ShouldAddALeadingSlash", have: "encore", expected: "/encore"},
		{name: "ShouldDropATrailingSlash", have: "encore/", expected: "/encore"},
		{name: "ShouldTrimSpaces", have: " /encore/ ", expected: "/encore"},
		{name: "ShouldCollapseRepeatedSlashes", have: "//encore//", expected: "/encore"},
		{name: "ShouldKeepANestedPath", have: "/apps/encore", expected: "/apps/encore"},
		{name: "ShouldKeepUnicode", have: "/música", expected: "/música"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, basePath(tc.have))
		})
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

// fillPatch sets every field of a settings patch, recursively, to a value unique to its JSON path and records it in want, so no field can be left out of changes or saved under another field's key unnoticed.
func fillPatch(v reflect.Value, prefix string, want map[string]any) {
	for i := range v.NumField() {
		f := v.Field(i)
		if !f.CanSet() {
			continue
		}

		path := prefix + strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0]

		switch e := f.Type().Elem(); {
		case e.Kind() == reflect.Struct:
			f.Set(reflect.New(e))
			fillPatch(f.Elem(), path+".", want)
		case e.Kind() == reflect.String:
			f.Set(reflect.ValueOf(new(" " + path + " ")))
			want[path] = path
		case e.Kind() == reflect.Int:
			n := len(want) + 1
			f.Set(reflect.ValueOf(&n))
			want[path] = n
		case e.Kind() == reflect.Bool:
			b := len(want)%2 == 0
			f.Set(reflect.ValueOf(&b))
			want[path] = b
		case e.Kind() == reflect.Slice:
			f.Set(reflect.ValueOf(&[]string{" " + path + " ", "", "  ", "b"}))
			want[path] = []string{path, "b"}
		}
	}
}

func TestSettingsPatchChanges(t *testing.T) {
	t.Parallel()

	var p SettingsPatch

	want := map[string]any{}
	fillPatch(reflect.ValueOf(&p).Elem(), "", want)

	ch := p.changes()
	// The locale is saved outside the config overrides.
	assert.Len(t, ch, len(want)-2, "every patch field becomes a change")

	seen := map[string]bool{}

	for _, c := range ch {
		assert.True(t, isOverridable(c.Key), "%s is a key the web interface may change", c.Key)
		assert.False(t, seen[c.Key], "%s appears once", c.Key)
		seen[c.Key] = true

		// A patch names a field as the settings document does: the config key, unless settingPaths says otherwise.
		path := c.Key
		if p, ok := settingPaths[c.Key]; ok {
			path = p
		}

		w, ok := want[path]
		if !assert.True(t, ok, "%s comes from the patch field %s", c.Key, path) {
			continue
		}

		if c.Key == "server.base_path" {
			w = "/" + path
		}

		assert.Equal(t, w, c.Value, "%s takes its own field's value, trimmed, with blank list entries dropped", c.Key)
	}

	for _, key := range []string{"auth.break_glass", "server.trusted_proxies", "alexa.queue_limit", "log.level", "storage.s3.secret_key", "storage.webdav.password", "auth.session_hours", "auth.header.groups_header", "auth.header.admin_group"} {
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

	s3 := base
	s3.Backend = config.BackendS3

	dav := base
	dav.Backend = config.BackendWebDAV

	testCases := []struct {
		name  string
		base  config.Storage
		edit  func(s *config.Storage)
		moved bool
	}{
		{name: "ShouldNotMoveForNothing", base: base, edit: func(*config.Storage) {}},
		{name: "ShouldNotMoveForTheSchedule", base: base, edit: func(s *config.Storage) { s.Backups.Schedule = "off" }},
		{name: "ShouldNotMoveForHowManyToKeep", base: base, edit: func(s *config.Storage) { s.Backups.Keep = 3 }},
		{name: "ShouldNotMoveForAnotherBackendsSettings", base: base, edit: func(s *config.Storage) { s.S3.Bucket = "other" }},
		{name: "ShouldMoveForTheLocalPath", base: base, edit: func(s *config.Storage) { s.Local.Path = "/mnt/backups" }, moved: true},
		{name: "ShouldMoveForTheBackupsFolder", base: base, edit: func(s *config.Storage) { s.Backups.Prefix = "" }, moved: true},
		{name: "ShouldMoveForTheBackend", base: base, edit: func(s *config.Storage) { s.Backend = config.BackendS3 }, moved: true},
		{name: "ShouldMoveForTheS3Endpoint", base: s3, edit: func(s *config.Storage) { s.S3.Endpoint = "https://other.example.net" }, moved: true},
		{name: "ShouldMoveForTheS3Region", base: s3, edit: func(s *config.Storage) { s.S3.Region = "us-east-1" }, moved: true},
		{name: "ShouldMoveForTheS3Bucket", base: s3, edit: func(s *config.Storage) { s.S3.Bucket = "other" }, moved: true},
		{name: "ShouldMoveForTheS3Prefix", base: s3, edit: func(s *config.Storage) { s.S3.Prefix = "other" }, moved: true},
		{name: "ShouldNotMoveForTheLocalPathOnS3", base: s3, edit: func(s *config.Storage) { s.Local.Path = "/elsewhere" }},
		{name: "ShouldMoveForTheWebDAVURL", base: dav, edit: func(s *config.Storage) { s.WebDAV.URL = "https://other.example.net" }, moved: true},
		{name: "ShouldMoveForTheWebDAVPrefix", base: dav, edit: func(s *config.Storage) { s.WebDAV.Prefix = "other" }, moved: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.base
			tc.edit(&s)
			assert.Equal(t, tc.moved, backupTarget(&s) != backupTarget(&tc.base))
		})
	}
}

func TestGuardAuthChange(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		env     []string
		peer    string
		changes []Change
		refused bool
	}{
		{name: "ShouldAllowAModeOtherThanHeader", peer: "192.0.2.10:5000", changes: []Change{{Key: "auth.mode", Value: "oidc"}}},
		{name: "ShouldAllowAnUnrelatedChange", peer: "192.0.2.10:5000", changes: []Change{{Key: "storage.backups.keep", Value: 3}}},
		{name: "ShouldAllowHeaderModeFromATrustedProxy", peer: "127.0.0.1:5000", changes: []Change{{Key: "auth.mode", Value: "header"}}},
		{name: "ShouldAllowHeaderModeFromIPv6Loopback", peer: "[::1]:5000", changes: []Change{{Key: "auth.mode", Value: "header"}}},
		{name: "ShouldRefuseHeaderModeFromElsewhere", peer: "192.0.2.10:5000", changes: []Change{{Key: "auth.mode", Value: "header"}}, refused: true},
		{name: "ShouldAllowHeaderModeWithTheNewProxyList", peer: "10.0.0.5:5000", changes: []Change{{Key: "auth.mode", Value: "header"}, {Key: "server.trusted_proxies", Value: []string{"10.0.0.0/8"}}}},
		{name: "ShouldAllowAnIPv4MappedPeer", peer: "[::ffff:10.0.0.5]:5000", changes: []Change{{Key: "auth.mode", Value: "header"}, {Key: "server.trusted_proxies", Value: []string{"10.0.0.0/8"}}}},
		{name: "ShouldRefuseWhenInvalidProxyEntriesMatchNothing", peer: "10.0.0.5:5000", changes: []Change{{Key: "auth.mode", Value: "header"}, {Key: "server.trusted_proxies", Value: []string{"10.0.0.0", "not a cidr"}}}, refused: true},
		{name: "ShouldRefuseDroppingTheProxyInHeaderMode", env: []string{"ENCORE_AUTH_MODE=header"}, peer: "127.0.0.1:5000", changes: []Change{{Key: "server.trusted_proxies", Value: []string{"10.0.0.0/8"}}}, refused: true},
		{name: "ShouldAllowLeavingHeaderMode", env: []string{"ENCORE_SERVER_TRUSTED_PROXIES=10.0.0.0/8"}, peer: "192.0.2.10:5000", changes: []Change{{Key: "auth.mode", Value: "local"}}},
		{name: "ShouldAllowAPeerThatIsNotAnAddress", peer: "@", changes: []Change{{Key: "auth.mode", Value: "header"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newPrefs(t, tc.env...)
			a := &API{prefs: f.prefs}

			r := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/settings", http.NoBody)
			r.RemoteAddr = tc.peer

			err := a.guardAuthChange(context.WithValue(t.Context(), ctxKey{}, &reqInfo{r: r}), tc.changes)
			if !tc.refused {
				require.NoError(t, err)

				return
			}

			m := errorModel(t, err)
			assert.Equal(t, http.StatusConflict, m.Status)
			assert.Contains(t, m.Detail, "lock you out")
		})
	}

	t.Run("ShouldAllowAnythingOutsideARequest", func(t *testing.T) {
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
