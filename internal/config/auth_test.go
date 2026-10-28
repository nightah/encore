package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthDefaults(t *testing.T) {
	cfg, err := Load(Options{Environ: environ()})
	require.NoError(t, err)

	assert.Empty(t, cfg.Auth.Mode)
	assert.Equal(t, 168, cfg.Auth.SessionHours)
	assert.Equal(t, "Remote-User", cfg.Auth.Header.UserHeader)
	assert.Equal(t, []string{"openid", "profile", "email", "groups"}, cfg.Auth.OIDC.Scopes)
	assert.Empty(t, cfg.ManagedKeys())
}

func TestAuthFromEnv(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "oidc_secret")
	require.NoError(t, os.WriteFile(secret, []byte("s3cret\n"), 0o600))

	cfg, err := Load(Options{Environ: environ(
		"ENCORE_AUTH_MODE=oidc",
		"ENCORE_AUTH_OIDC_ISSUER=https://auth.example.net",
		"ENCORE_AUTH_OIDC_CLIENT_ID=encore",
		"ENCORE_AUTH_OIDC_CLIENT_SECRET_FILE="+secret,
		"ENCORE_AUTH_OIDC_SCOPES=openid, groups",
		"ENCORE_BREAK_GLASS=true",
	)})
	require.NoError(t, err)

	assert.Equal(t, AuthModeOIDC, cfg.Auth.Mode)
	assert.Equal(t, "s3cret", cfg.Auth.OIDC.ClientSecret)
	assert.Equal(t, []string{"openid", "groups"}, cfg.Auth.OIDC.Scopes)
	assert.True(t, cfg.Auth.BreakGlass)
	assert.True(t, cfg.Managed("auth.oidc"))
	assert.True(t, cfg.Managed("auth.oidc.client_secret"))
	assert.False(t, cfg.Managed("auth.header"))
}

func TestAuthValidation(t *testing.T) {
	for name, env := range map[string][]string{
		"unknown mode":              {"ENCORE_AUTH_MODE=ldap"},
		"oidc without client":       {"ENCORE_AUTH_MODE=oidc", "ENCORE_AUTH_OIDC_ISSUER=https://auth.example.net"},
		"header without proxies":    {"ENCORE_AUTH_MODE=header", "ENCORE_SERVER_TRUSTED_PROXIES="},
		"bad header name":           {"ENCORE_AUTH_HEADER_USER_HEADER=Remote User"},
		"zero session":              {"ENCORE_AUTH_SESSION_HOURS=0"},
		"issuer not a URL":          {"ENCORE_AUTH_OIDC_ISSUER=auth.example.net"},
		"redirect not a URL":        {"ENCORE_AUTH_OIDC_REDIRECT_URI=/cb"},
		"lockout attempts disabled": {"ENCORE_AUTH_LOCKOUT_ATTEMPTS=0"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(Options{Environ: environ(env...)})
			require.Error(t, err)
		})
	}

	_, err := Load(Options{Environ: environ("ENCORE_AUTH_MODE=header", "ENCORE_SERVER_TRUSTED_PROXIES=172.20.0.0/16")})
	require.NoError(t, err)
}

func TestAuthValidationMessages(t *testing.T) {
	for env, want := range map[string]string{
		"ENCORE_AUTH_HEADER_GROUPS_HEADER=Remote: Groups": `auth.header.groups_header "Remote: Groups" is not a header name`,
		"ENCORE_AUTH_OIDC_ISSUER=https://[::1":            "auth.oidc.issuer: is not a valid URL",
		"ENCORE_AUTH_OIDC_REDIRECT_URI=ftp://encore/cb":   `auth.oidc.redirect_uri: "ftp://encore/cb" must be an http or https URL`,
	} {
		_, err := Load(Options{Environ: environ(env)})
		require.ErrorContains(t, err, want, env)
	}

	_, err := Load(Options{Environ: environ("ENCORE_AUTH_HEADER_GROUPS_HEADER=Remote-Groups")})
	require.NoError(t, err, "a valid groups header")
}

func TestManagedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encore.yml")
	require.NoError(t, os.WriteFile(path, []byte("storage:\n  backend: local\nkodi:\n  - name: Lounge\n    url: http://kodi:8080/jsonrpc\n"), 0o600))

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("log.level", "info", "")
	flags.String("server.address", ":8080", "")
	require.NoError(t, flags.Parse([]string{"--log.level=debug"}))

	cfg, err := Load(Options{File: path, Flags: flags, Environ: environ("ENCORE_SERVER_TRUSTED_PROXIES=10.0.0.0/8")})
	require.NoError(t, err)

	assert.Equal(t, []string{"kodi", "log.level", "server.trusted_proxies", "storage.backend"}, cfg.ManagedKeys())
	assert.True(t, cfg.Managed("storage"))
	assert.False(t, cfg.Managed("server.address"), "an unset flag is not managed")
	def := Defaults()
	assert.False(t, def.Managed("storage"))
}
