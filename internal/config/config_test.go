package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func environ(vars ...string) func() []string {
	return func() []string { return vars }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(Options{Environ: environ()})
	require.NoError(t, err)

	assert.Equal(t, Defaults().Server.Address, cfg.Server.Address)
	assert.Equal(t, "/", cfg.Server.BasePath)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, DefaultDataDir(), cfg.DataDir, "the per-user directory, when nothing sets data_dir")
}

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "encore.yml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  address: \":8000\"\nlog:\n  level: warn\n  format: json\n"), 0o600))

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("log.level", "info", "")
	flags.String("server.address", ":8080", "")
	require.NoError(t, flags.Parse([]string{"--log.level=debug"}))

	cfg, err := Load(Options{
		File:    path,
		Flags:   flags,
		Environ: environ("ENCORE_SERVER_ADDRESS=:7000", "ENCORE_LOG_FORMAT=text", "UNRELATED=1"),
	})
	require.NoError(t, err)

	assert.Equal(t, ":7000", cfg.Server.Address, "env beats file, and an unset flag must not override")
	assert.Equal(t, "text", cfg.Log.Format, "env beats file")
	assert.Equal(t, "debug", cfg.Log.Level, "a set flag beats everything")
}

func TestLoadFileSuffix(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "skill_id")
	require.NoError(t, os.WriteFile(secret, []byte("amzn1.ask.skill.test\n"), 0o600))

	cfg, err := Load(Options{Environ: environ("ENCORE_ALEXA_SKILL_ID_FILE=" + secret)})
	require.NoError(t, err)

	assert.Equal(t, "amzn1.ask.skill.test", cfg.Alexa.SkillID)
}

func TestLoadFileSuffixMissing(t *testing.T) {
	_, err := Load(Options{Environ: environ("ENCORE_ALEXA_SKILL_ID_FILE=/nonexistent/skill_id")})
	assert.ErrorContains(t, err, "ENCORE_ALEXA_SKILL_ID_FILE")
}

func TestLoadTrustedProxiesFromEnv(t *testing.T) {
	cfg, err := Load(Options{Environ: environ("ENCORE_SERVER_TRUSTED_PROXIES=172.20.0.0/16, 10.0.10.5/32")})
	require.NoError(t, err)

	assert.Equal(t, []string{"172.20.0.0/16", "10.0.10.5/32"}, cfg.Server.TrustedProxies)
}

func TestValidateReportsAllErrors(t *testing.T) {
	cfg := Defaults()
	cfg.Server.BasePath = "encore"
	cfg.Server.TrustedProxies = []string{"not-a-cidr"}
	cfg.Log.Level = "loud"
	cfg.Log.Format = "xml"

	err := cfg.Validate()
	require.Error(t, err)

	for _, want := range []string{"base_path", "trusted_proxies", "log.level", "log.format"} {
		assert.ErrorContains(t, err, want)
	}
}

func TestLoadKodiInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encore.yml")
	require.NoError(t, os.WriteFile(path, []byte(`
kodi:
  - name: Lounge
    url: http://192.168.1.20:8080/jsonrpc
    websocket_url: ws://192.168.1.20:9090/jsonrpc
    mac: DC:A6:32:4B:19:0E
    default: true
  - name: Bedroom
    url: http://192.168.1.31:8080/jsonrpc
    language: de
    library_group: upstairs
`), 0o600))

	cfg, err := Load(Options{File: path, Environ: environ()})
	require.NoError(t, err)
	require.Len(t, cfg.Kodi, 2)

	assert.Equal(t, "Lounge", cfg.Kodi[0].Name)
	assert.True(t, cfg.Kodi[0].Default)
	assert.Equal(t, "ws://192.168.1.20:9090/jsonrpc", cfg.Kodi[0].WebSocketURL)
	assert.Equal(t, "de", cfg.Kodi[1].Language)
	assert.Equal(t, "upstairs", cfg.Kodi[1].LibraryGroup)
	assert.Empty(t, cfg.Kodi[0].LibraryGroup, "empty lets encore detect sharing")
}

func TestKodiNoLibraryGroup(t *testing.T) {
	for group, want := range map[string]bool{"": false, "upstairs": false, "none": true, "None": true} {
		k := KodiInstance{LibraryGroup: group}
		assert.Equal(t, want, k.NoLibraryGroup(), group)
	}
}

func TestValidateKodi(t *testing.T) {
	cfg := Defaults()
	cfg.Kodi = []KodiInstance{
		{Name: "Lounge", URL: "http://kodi:pw@kodi/jsonrpc", Default: true},
		{Name: "Lounge", URL: "ftp://kodi", WebSocketURL: "http://kodi:9090", MAC: "nope", Default: true},
		{URL: "http://kodi/jsonrpc", LibraryGroup: " upstairs"},
	}

	err := cfg.Validate()
	require.Error(t, err)

	for _, want := range []string{"used twice", "kodi[1].url", "websocket_url", "kodi[1].mac", "only one", "kodi[2].name", "kodi[2].library_group"} {
		require.ErrorContains(t, err, want)
	}

	assert.NotContains(t, err.Error(), "pw", "credentials are redacted")
}

func TestAlexaLocales(t *testing.T) {
	cfg, err := Load(Options{Environ: environ("ENCORE_ALEXA_LOCALES=en-AU, en-GB")})
	require.NoError(t, err)
	assert.Equal(t, []string{"en-AU", "en-GB"}, cfg.Alexa.Locales)

	cfg, err = Load(Options{Environ: environ()})
	require.NoError(t, err)
	assert.Empty(t, cfg.Alexa.Locales, "empty means every shipped locale")

	_, err = Load(Options{Environ: environ("ENCORE_ALEXA_LOCALES=australian")})
	require.ErrorContains(t, err, "alexa.locales")
}

func TestStatelessValidation(t *testing.T) {
	cfg := Defaults()
	cfg.Stateless = true

	err := cfg.Validate()
	require.ErrorContains(t, err, "storage.backend")
	require.ErrorContains(t, err, "secrets.key")

	cfg.Storage.Backend = "s3"
	cfg.Storage.S3 = StorageS3{Bucket: "encore", AccessKey: "a", SecretKey: "b", Region: "us-east-1"}
	cfg.Secrets.Key = "c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0MTI="

	err = cfg.Validate()
	if err != nil {
		assert.NotContains(t, err.Error(), "stateless")
	}
}

func TestRoomsAndDevicesValidation(t *testing.T) {
	cfg := Defaults()
	cfg.Kodi = []KodiInstance{{Name: "Lounge", URL: "http://kodi/jsonrpc"}}
	cfg.Rooms = []Room{{Name: "Lounge", Kodi: "Lounge", Aliases: []string{"living room"}}, {Name: "Lounge", Kodi: "Attic"}}
	cfg.Devices = []Device{{ID: "amzn1.ask.device.A", Kodi: "Lounge", Room: "Lounge"}, {ID: "amzn1.ask.device.A", Room: "Garage"}, {Kodi: "Lounge"}}

	err := cfg.Validate()
	require.Error(t, err)

	for _, want := range []string{"rooms[1].name", "rooms[1].kodi", "devices[1].id is used twice", "devices[1].room", "devices[2].id"} {
		require.ErrorContains(t, err, want)
	}
}

func TestAlexaContent(t *testing.T) {
	cfg, err := Load(Options{Environ: environ()})
	require.NoError(t, err)
	assert.Equal(t, []string{"movies", "tv", "music"}, cfg.Alexa.Content, "everything by default")
	assert.False(t, cfg.Managed("alexa.content"), "a default the web interface can change")

	cfg, err = Load(Options{Environ: environ("ENCORE_ALEXA_CONTENT=movies,tv")})
	require.NoError(t, err)
	assert.Equal(t, []string{"movies", "tv"}, cfg.Alexa.Content)

	_, err = Load(Options{Environ: environ("ENCORE_ALEXA_CONTENT=movies,podcasts")})
	require.ErrorContains(t, err, `"podcasts" is not one of movies, tv or music`)
}

func TestEnvNames(t *testing.T) {
	cfg, err := Load(Options{Environ: environ(
		"ENCORE_SERVER_ADDRESS=:7000",
		"ENCORE_SERVER_TRUSTED_PROXIES=10.0.0.0/8,127.0.0.1/32",
		"ENCORE_STORAGE_BACKUPS_PREFIX=Kodi",
		"ENCORE_AUTH_OIDC_GROUPS_CLAIM=roles",
		"ENCORE_CONTRACT_KODI_URL=http://elsewhere", // another tool's variable
	)})
	require.NoError(t, err)

	assert.Equal(t, ":7000", cfg.Server.Address)
	assert.Equal(t, []string{"10.0.0.0/8", "127.0.0.1/32"}, cfg.Server.TrustedProxies)
	assert.Equal(t, "Kodi", cfg.Storage.Backups.Prefix)
	assert.Equal(t, "roles", cfg.Auth.OIDC.GroupsClaim)
}

func TestEnvNamesAreUnique(t *testing.T) {
	seen := map[string]string{}

	for key := range defaultsMap() {
		name := EnvName(key)
		if other, ok := seen[name]; ok {
			t.Errorf("%s and %s share the variable %s", key, other, name)
		}

		seen[name] = key
	}
}

func TestLogFormat(t *testing.T) {
	cfg, err := Load(Options{Environ: environ()})
	require.NoError(t, err)
	assert.Equal(t, "auto", cfg.Log.Format, "pretty on a terminal, text otherwise")

	for _, f := range []string{"auto", "pretty", "text", "json"} {
		_, err := Load(Options{Environ: environ("ENCORE_LOG_FORMAT=" + f)})
		require.NoError(t, err, f)
	}

	_, err = Load(Options{Environ: environ("ENCORE_LOG_FORMAT=fancy")})
	require.ErrorContains(t, err, `log.format "fancy" must be auto, pretty, text or json`)
}

func TestBasePathValidation(t *testing.T) {
	for base, ok := range map[string]bool{
		"/": true, "/encore": true, "/encore/": true, "/media/encore": true, "/v1.2_x~y-z": true,
		"": false, "encore": false, "/en core": false, "/../x": false, "/./x": false, "//x": false, "/a?b": false,
		"/kodi": false, "/Settings/x": false, "/api": false, "/static": false, "/kodix": true,
	} {
		cfg := Defaults()
		cfg.Server.BasePath = base

		if ok {
			assert.NoError(t, cfg.Validate(), base)
		} else {
			assert.ErrorContains(t, cfg.Validate(), "server.base_path", base)
		}
	}
}

// TestReservedBasePathsCoverRoutes keeps ReservedBasePaths in step with the web interface's top-level pages and public folders.
func TestReservedBasePathsCoverRoutes(t *testing.T) {
	var want []string

	for _, dir := range []string{"../../web/src/routes", "../../web/src/routes/_shell"} {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)

		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".tsx")
			if !e.IsDir() && !strings.HasPrefix(name, "_") && name != "index" {
				want = append(want, name)
			}
		}
	}

	entries, err := os.ReadDir("../../web/public")
	require.NoError(t, err)

	for _, e := range entries {
		if e.IsDir() {
			want = append(want, e.Name())
		}
	}

	for _, name := range want {
		assert.Contains(t, ReservedBasePaths, name)
	}

	assert.Contains(t, ReservedBasePaths, "api")
	assert.Contains(t, ReservedBasePaths, "static", "Vite's build.assetsDir")
}
