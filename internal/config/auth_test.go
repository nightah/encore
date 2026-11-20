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
	testCases := []struct {
		name string
		have []string
		err  bool
	}{
		{name: "ShouldRejectAnUnknownMode", have: []string{"ENCORE_AUTH_MODE=ldap"}, err: true},
		{name: "ShouldRejectOIDCWithoutAClient", have: []string{"ENCORE_AUTH_MODE=oidc", "ENCORE_AUTH_OIDC_ISSUER=https://auth.example.net"}, err: true},
		{name: "ShouldRejectHeaderModeWithoutProxies", have: []string{"ENCORE_AUTH_MODE=header", "ENCORE_SERVER_TRUSTED_PROXIES="}, err: true},
		{name: "ShouldRejectABadHeaderName", have: []string{"ENCORE_AUTH_HEADER_USER_HEADER=Remote User"}, err: true},
		{name: "ShouldRejectAZeroSession", have: []string{"ENCORE_AUTH_SESSION_HOURS=0"}, err: true},
		{name: "ShouldRejectAnIssuerThatIsNotAURL", have: []string{"ENCORE_AUTH_OIDC_ISSUER=auth.example.net"}, err: true},
		{name: "ShouldRejectARedirectThatIsNotAURL", have: []string{"ENCORE_AUTH_OIDC_REDIRECT_URI=/cb"}, err: true},
		{name: "ShouldRejectDisabledLockoutAttempts", have: []string{"ENCORE_AUTH_LOCKOUT_ATTEMPTS=0"}, err: true},
		{name: "ShouldAcceptHeaderModeWithProxies", have: []string{"ENCORE_AUTH_MODE=header", "ENCORE_SERVER_TRUSTED_PROXIES=172.20.0.0/16"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(Options{Environ: environ(tc.have...)})

			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAuthValidationMessages(t *testing.T) {
	testCases := []struct {
		name string
		have string
		err  string
	}{
		{name: "ShouldRejectAGroupsHeaderThatIsNotAHeaderName", have: "ENCORE_AUTH_HEADER_GROUPS_HEADER=Remote: Groups", err: `auth.header.groups_header "Remote: Groups" is not a header name`},
		{name: "ShouldRejectAnInvalidIssuerURL", have: "ENCORE_AUTH_OIDC_ISSUER=https://[::1", err: "auth.oidc.issuer: is not a valid URL"},
		{name: "ShouldRejectARedirectURIThatIsNotHTTP", have: "ENCORE_AUTH_OIDC_REDIRECT_URI=ftp://encore/cb", err: `auth.oidc.redirect_uri: "ftp://encore/cb" must be an http or https URL`},
		{name: "ShouldAcceptAValidGroupsHeader", have: "ENCORE_AUTH_HEADER_GROUPS_HEADER=Remote-Groups"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(Options{Environ: environ(tc.have)})

			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
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
