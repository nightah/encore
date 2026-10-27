package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/config"
)

func TestDefaultConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENCORE_DATA_DIR", dir)

	assert.Empty(t, defaultConfigFile(), "no encore.yml means no default file")

	path := filepath.Join(dir, "encore.yml")
	require.NoError(t, os.WriteFile(path, []byte("log:\n  level: debug\n"), 0o600))

	assert.Equal(t, path, defaultConfigFile())

	cmd := NewRootCommand(os.Stdout, os.Stderr)
	cmd.SetArgs([]string{"version"})
	require.NoError(t, cmd.Execute())

	cfg, err := loadConfig(cmd)
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Log.Level, "encore.yml in the data directory is loaded without --config")
}

func TestLogFormats(t *testing.T) {
	logLine := func(format string) string {
		t.Helper()

		var buf bytes.Buffer

		log, _, err := newLogger(&buf, config.Log{Level: "info", Format: format})
		require.NoError(t, err)
		log.Info("library synced", "instance", "Lounge", "error", errors.New("boom"))

		return buf.String()
	}

	assert.Contains(t, logLine("text"), `msg="library synced" instance=Lounge error=boom`)
	assert.Contains(t, logLine("json"), `"msg":"library synced","instance":"Lounge","error":"boom"`)

	pretty := logLine("pretty")
	assert.Contains(t, pretty, "\x1b[", "pretty is coloured")
	assert.Contains(t, pretty, "library synced")

	t.Setenv("NO_COLOR", "1")
	assert.NotContains(t, logLine("pretty"), "\x1b[", "NO_COLOR turns colour off")

	assert.Contains(t, logLine("auto"), `msg="library synced"`, "auto writes plain text when not on a terminal")
}

func TestLogFormatResolvesAuto(t *testing.T) {
	assert.Equal(t, "text", logFormat("auto", &bytes.Buffer{}))
	assert.Equal(t, "json", logFormat("json", &bytes.Buffer{}))
	assert.Equal(t, "pretty", logFormat("pretty", &bytes.Buffer{}), "pretty when asked, even off a terminal")

	f, err := os.CreateTemp(t.TempDir(), "log")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	assert.Equal(t, "text", logFormat("auto", f), "a file isn't a terminal")
}
