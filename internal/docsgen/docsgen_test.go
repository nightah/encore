package docsgen

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the generated documentation pages")

const root = "../.."

// TestDocsDrift renders the reference pages, and with -update (`go generate ./internal/docsgen`) writes them into docs/ and removes ones no longer generated. The pages aren't committed; the docs site's scripts generate them before every build.
func TestDocsDrift(t *testing.T) {
	// The --config flag defaults to $ENCORE_CONFIG; keep the developer's environment out of the page.
	t.Setenv("ENCORE_CONFIG", "")

	files, err := Generate(root)
	require.NoError(t, err)

	if *update {
		for _, path := range staleGenerated(t, files) {
			require.NoError(t, os.Remove(filepath.Join(root, path)))
		}

		for path, b := range files {
			full := filepath.Join(root, path)
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
			require.NoError(t, os.WriteFile(full, b, 0o644))
		}

		return
	}

	for path, want := range files {
		require.Contains(t, string(want), GeneratedMarker, "%s must carry the generated marker", path)
	}
}

// staleGenerated returns the pages under the content directory that carry the generated marker but are no longer generated, relative to root. Hand-written pages never carry the marker.
func staleGenerated(t *testing.T, files map[string][]byte) []string {
	t.Helper()

	var stale []string

	err := filepath.WalkDir(filepath.Join(root, ContentDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)
		if _, ok := files[rel]; ok {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if bytes.Contains(b, []byte(GeneratedMarker)) {
			stale = append(stale, rel)
		}

		return nil
	})
	require.NoError(t, err)

	return stale
}

func TestVoicePagesEditLinks(t *testing.T) {
	files, err := Generate(root)
	require.NoError(t, err)

	page := func(lang string) string {
		b, ok := files[ContentDir+"/voice-commands/"+lang+".md"]
		require.True(t, ok, "no voice commands page for %s", lang)

		return string(b)
	}

	assert.Contains(t, page("en"), "editUrl: "+SourceEditURL+"\n", "English is edited in the repository")
	assert.Contains(t, page("de"), "editUrl: https://crowdin.com/project/encore/de\n", "translations are edited on Crowdin")
	assert.Contains(t, page("es"), "editUrl: https://crowdin.com/project/encore/es-ES\n", "Crowdin names Spanish es-ES")
}

func TestConfigSectionsCoverEveryKey(t *testing.T) {
	sections, err := ConfigSections(filepath.Join(root, "internal", "config"))
	require.NoError(t, err, "every config key needs a doc comment")

	keys := map[string]ConfigKey{}

	for _, s := range sections {
		for _, k := range s.Keys {
			keys[k.Key] = k
		}
	}

	assert.Equal(t, "ENCORE_SERVER_ADDRESS", keys["server.address"].Env)
	assert.Equal(t, `":8080"`, keys["server.address"].Default)
	assert.Equal(t, "[openid, profile, email, groups]", keys["auth.oidc.scopes"].Default)
	assert.Equal(t, []string{"ENCORE_BREAK_GLASS"}, keys["auth.break_glass"].EnvAliases)
	assert.Empty(t, keys["secrets.key_file"].Env, "ENCORE_SECRETS_KEY_FILE is the _FILE form of secrets.key")
	assert.Empty(t, keys["kodi[].url"].Env)
	assert.Empty(t, keys["kodi[].url"].Default)
	assert.Contains(t, keys["auth.oidc.admin_group"].Doc, "`auth.oidc.groups_claim`")
	assert.True(t, strings.HasPrefix(keys["server.address"].Doc, "`server.address` is"))
	assert.Contains(t, keys["storage.s3.sse"].Doc, "`storage.s3.sse_kms_key_id`")
}

func TestCode(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"":                 "``",
		"server.address":   "`server.address`",
		"a`b":              "``a`b``",
		"a``b":             "```a``b```",
		"`start":           "`` `start ``",
		"end`":             "`` end` ``",
		"{Movie} [please]": "`{Movie} [please]`",
	} {
		assert.Equal(t, want, code(in), "%q", in)
	}
}

func TestCell(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `a \| b`, cell("a | b"))
	assert.Equal(t, "one two", cell("one\ntwo"))
	assert.Equal(t, `\|\| x`, cell("||\nx"))
	assert.Empty(t, cell(""))
}

func TestProse(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "play &lt;title&gt; now", prose("play <title> now"))
	assert.Equal(t, "a & b", prose("a & b"), "only angle brackets start tags")
}

func TestGenerateWritesEveryPageUnderTheContentDir(t *testing.T) {
	t.Setenv("ENCORE_CONFIG", "")

	files, err := Generate(root)
	require.NoError(t, err)

	for _, path := range []string{ContentDir + "/reference/configuration.md", ContentDir + "/reference/cli.md", ContentDir + "/voice-commands/en.md"} {
		assert.Contains(t, files, path)
	}

	for path := range files {
		assert.True(t, strings.HasPrefix(path, ContentDir+"/"), path)
		assert.True(t, strings.HasSuffix(path, ".md"), path)
	}
}

func TestFlagDefaultsFromUsage(t *testing.T) {
	set := pflag.NewFlagSet("test", pflag.ContinueOnError)
	set.String("server.address", "", `listen address (default ":8080")`)
	set.String("log.format", "", `log format (default "auto": pretty on a terminal)`)
	set.StringSlice("locale", nil, "locales to build (default all)")
	set.String("plain", "x", "a flag with a registered default")
	set.String("none", "", "no default at all")

	got := map[string]string{}
	for _, f := range flags(set) {
		got[f.Name] = f.Default
	}

	assert.Equal(t, map[string]string{"server.address": ":8080", "log.format": "auto", "locale": "all", "plain": "x", "none": ""}, got)
}
