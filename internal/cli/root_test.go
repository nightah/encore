package cli

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/buildinfo"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/skillmodel"
)

// runCLI runs the command line against an empty data directory, unless the test chose one, and returns what it wrote.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	t.Setenv("ENCORE_CONFIG", "")

	if os.Getenv("ENCORE_DATA_DIR") == "" {
		t.Setenv("ENCORE_DATA_DIR", t.TempDir())
	}

	var out, errOut bytes.Buffer

	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(""))
	err = cmd.ExecuteContext(t.Context())

	return out.String(), errOut.String(), err
}

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

func TestDefaultConfigFileIgnoresADirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENCORE_DATA_DIR", dir)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "encore.yml"), 0o700))

	assert.Empty(t, defaultConfigFile(), "only a regular file is a config file")
}

func TestDefaultConfigFileUsesTheDefaultDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ENCORE_DATA_DIR", "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))

	require.NoError(t, os.MkdirAll(config.DefaultDataDir(), 0o700))
	assert.Empty(t, defaultConfigFile())

	path := filepath.Join(config.DefaultDataDir(), "encore.yml")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))
	assert.Equal(t, path, defaultConfigFile())
}

func TestLoadConfigPrefersTheConfigFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENCORE_CONFIG", "")
	t.Setenv("ENCORE_DATA_DIR", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "encore.yml"), []byte("log:\n  level: debug\n"), 0o600))

	explicit := filepath.Join(t.TempDir(), "other.yml")
	require.NoError(t, os.WriteFile(explicit, []byte("log:\n  level: warn\n"), 0o600))

	cmd := NewRootCommand(io.Discard, io.Discard)
	cmd.SetArgs([]string{"version", "--config", explicit})
	require.NoError(t, cmd.Execute())

	cfg, err := loadConfig(cmd)
	require.NoError(t, err)
	assert.Equal(t, "warn", cfg.Log.Level, "--config wins over encore.yml in the data directory")
}

func TestLoadConfigReadsTheConfigEnvironmentVariable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.yml")
	require.NoError(t, os.WriteFile(path, []byte("log:\n  level: error\n"), 0o600))
	t.Setenv("ENCORE_CONFIG", path)
	t.Setenv("ENCORE_DATA_DIR", t.TempDir())

	cmd := NewRootCommand(io.Discard, io.Discard)
	cmd.SetArgs([]string{"version"})
	require.NoError(t, cmd.Execute())

	cfg, err := loadConfig(cmd)
	require.NoError(t, err)
	assert.Equal(t, "error", cfg.Log.Level)
}

func TestLoadConfigReportsAMissingOrInvalidFile(t *testing.T) {
	_, _, err := runCLI(t, "healthcheck", "--config", filepath.Join(t.TempDir(), "missing.yml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing.yml")

	bad := filepath.Join(t.TempDir(), "bad.yml")
	require.NoError(t, os.WriteFile(bad, []byte("log:\n  level: loud\n"), 0o600))

	_, _, err = runCLI(t, "healthcheck", "--config", bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loud")
}

func TestRootVersionFlag(t *testing.T) {
	out, _, err := runCLI(t, "--version")
	require.NoError(t, err)
	assert.Equal(t, buildinfo.Get().String()+"\n", out)
}

func TestRootRejectsUnknownCommandsAndFlags(t *testing.T) {
	testCases := []struct {
		name string
		have []string
		err  string
	}{
		{name: "ShouldRejectAnUnknownCommand", have: []string{"frobnicate"}, err: `unknown command "frobnicate"`},
		{name: "ShouldRejectAnUnknownFlag", have: []string{"version", "--nope"}, err: "unknown flag: --nope"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runCLI(t, tc.have...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.err)
		})
	}
}

func TestLogFormats(t *testing.T) {
	testCases := []struct {
		name       string
		have       string
		noColor    bool
		expected   []string
		unexpected string
	}{
		{name: "ShouldWriteText", have: "text", expected: []string{`msg="library synced" instance=Lounge error=boom`}},
		{name: "ShouldWriteJSON", have: "json", expected: []string{`"msg":"library synced","instance":"Lounge","error":"boom"`}},
		{name: "ShouldColourPretty", have: "pretty", expected: []string{"\x1b[", "library synced"}},
		{name: "ShouldTurnColourOffForNO_COLOR", have: "pretty", noColor: true, unexpected: "\x1b["},
		{name: "ShouldWritePlainTextForAutoWhenNotOnATerminal", have: "auto", noColor: true, expected: []string{`msg="library synced"`}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.noColor {
				t.Setenv("NO_COLOR", "1")
			}

			var buf bytes.Buffer

			log, _, err := newLogger(&buf, config.Log{Level: "info", Format: tc.have})
			require.NoError(t, err)
			log.Info("library synced", "instance", "Lounge", "error", errors.New("boom"))

			for _, s := range tc.expected {
				assert.Contains(t, buf.String(), s)
			}

			if tc.unexpected != "" {
				assert.NotContains(t, buf.String(), tc.unexpected)
			}
		})
	}
}

func TestLogFormatResolvesAuto(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	testCases := []struct {
		name     string
		have     string
		out      io.Writer
		expected string
	}{
		{name: "ShouldResolveAutoToTextOffATerminal", have: "auto", out: &bytes.Buffer{}, expected: "text"},
		{name: "ShouldKeepJSON", have: "json", out: &bytes.Buffer{}, expected: "json"},
		{name: "ShouldKeepPrettyEvenOffATerminal", have: "pretty", out: &bytes.Buffer{}, expected: "pretty"},
		{name: "ShouldResolveAutoToTextForAFileAsItIsntATerminal", have: "auto", out: f, expected: "text"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, logFormat(tc.have, tc.out))
		})
	}
}

func TestNewLoggerLevels(t *testing.T) {
	var buf bytes.Buffer

	log, level, err := newLogger(&buf, config.Log{Level: "warn", Format: "text"})
	require.NoError(t, err)
	assert.Equal(t, slog.LevelWarn, level.Level())

	log.Info("hidden")
	log.Warn("shown")
	assert.NotContains(t, buf.String(), "hidden")
	assert.Contains(t, buf.String(), "shown")

	level.Set(slog.LevelDebug)
	log.Debug("now shown")
	assert.Contains(t, buf.String(), "now shown", "changing the level takes effect without a new logger")

	_, _, err = newLogger(&buf, config.Log{Level: "loud", Format: "text"})
	require.Error(t, err)
}

func TestPrettyLogHighlightsOnlyErrors(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	var buf bytes.Buffer

	log, _, err := newLogger(&buf, config.Log{Level: "info", Format: "pretty"})
	require.NoError(t, err)

	log.Info("plain", "error", nil, "instance", "Lounge")
	assert.NotContains(t, buf.String(), "91m", "a nil error isn't highlighted")

	buf.Reset()
	log.Info("failed", "error", errors.New("boom"))
	assert.Contains(t, buf.String(), "91merror=", "an error is bright red")
}

// withProcessIO points os.Args, os.Stdout and os.Stderr at test values for Execute, which uses the process's own.
func withProcessIO(t *testing.T, args ...string) (stdout, stderr *os.File) {
	t.Helper()

	t.Setenv("ENCORE_CONFIG", "")
	t.Setenv("ENCORE_DATA_DIR", t.TempDir())

	dir := t.TempDir()

	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	require.NoError(t, err)

	stderr, err = os.Create(filepath.Join(dir, "stderr"))
	require.NoError(t, err)

	oldArgs, oldOut, oldErr := os.Args, os.Stdout, os.Stderr
	os.Args, os.Stdout, os.Stderr = append([]string{"encore"}, args...), stdout, stderr

	t.Cleanup(func() {
		os.Args, os.Stdout, os.Stderr = oldArgs, oldOut, oldErr
		_ = stdout.Close()
		_ = stderr.Close()
	})

	return stdout, stderr
}

func contents(t *testing.T, f *os.File) string {
	t.Helper()

	b, err := os.ReadFile(f.Name())
	require.NoError(t, err)

	return string(b)
}

func TestExecuteExitCodes(t *testing.T) {
	t.Run("ShouldExitZeroOnSuccess", func(t *testing.T) {
		stdout, stderr := withProcessIO(t, "version")

		assert.Equal(t, 0, Execute())
		assert.Equal(t, buildinfo.Get().String()+"\n", contents(t, stdout))
		assert.Empty(t, contents(t, stderr))
	})

	t.Run("ShouldReportAFailure", func(t *testing.T) {
		_, stderr := withProcessIO(t, "frobnicate")

		assert.Equal(t, 1, Execute())
		assert.Contains(t, contents(t, stderr), `error: unknown command "frobnicate"`)
	})

	t.Run("ShouldNotReportASilentFailureTwice", func(t *testing.T) {
		src, err := skillmodel.Load(i18n.FS)
		require.NoError(t, err)

		stdout, stderr := withProcessIO(t, "gen", "lint", "--strict")

		if len(skillmodel.Lint(src)) == 0 {
			assert.Equal(t, 0, Execute(), "a model without findings passes --strict")

			return
		}

		assert.Equal(t, 1, Execute())
		assert.Contains(t, contents(t, stdout), "finding(s)")
		assert.Empty(t, contents(t, stderr), "lint already printed its findings")
	})
}
