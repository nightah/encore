package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
)

func loadSources(t *testing.T) *skillmodel.Sources {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	return src
}

func TestGenLintReportsEveryFinding(t *testing.T) {
	src := loadSources(t)
	findings := skillmodel.Lint(src)

	out, _, err := runCLI(t, "gen", "lint")

	if skillmodel.HasErrors(findings) {
		require.ErrorIs(t, err, errSilent, "errors fail the lint")
	} else {
		require.NoError(t, err, "warnings alone pass without --strict")
	}

	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, len(findings)+1)

	for i, f := range findings {
		assert.Equal(t, f.String(), lines[i])
	}

	assert.Equal(t, fmt.Sprintf("%d finding(s) across %d locale(s)", len(findings), len(src.Locales)), lines[len(lines)-1])

	_, _, err = runCLI(t, "gen", "lint", "--strict")

	if len(findings) > 0 {
		require.ErrorIs(t, err, errSilent, "--strict fails on warnings too")
	} else {
		require.NoError(t, err)
	}
}

func TestGenModelPrintsOneLocale(t *testing.T) {
	out, _, err := runCLI(t, "gen", "model", "--locale", "en-AU", "--invocation", "lounge room")
	require.NoError(t, err)

	var model skillmodel.InteractionModel
	require.NoError(t, json.Unmarshal([]byte(out), &model))
	assert.Equal(t, "lounge room", model.InteractionModel.LanguageModel.InvocationName)
	assert.NotEmpty(t, model.InteractionModel.LanguageModel.Intents)
	assert.True(t, strings.HasSuffix(out, "}\n"), "ends with a newline")

	want, err := skillmodel.Build(loadSources(t), "en-AU", skillmodel.BuildOptions{Invocation: "lounge room"})
	require.NoError(t, err)

	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantJSON), out)
}

func TestGenModelUsesTheLocaleInvocationByDefault(t *testing.T) {
	src := loadSources(t)

	out, _, err := runCLI(t, "gen", "model", "-l", "fr-FR")
	require.NoError(t, err)

	var model skillmodel.InteractionModel
	require.NoError(t, json.Unmarshal([]byte(out), &model))
	assert.Equal(t, src.Locales["fr-FR"].Invocation, model.InteractionModel.LanguageModel.InvocationName)
}

func TestGenModelNeedsOneLocaleWithoutOut(t *testing.T) {
	for _, args := range [][]string{
		{"gen", "model"},
		{"gen", "model", "--locale", "en-AU,en-GB"},
	} {
		_, _, err := runCLI(t, args...)
		require.Error(t, err, args)
		assert.Contains(t, err.Error(), "choose one --locale", args)
		assert.Contains(t, err.Error(), "en-AU", "the error lists the locales")
	}
}

func TestGenModelRejectsAnUnknownLocale(t *testing.T) {
	_, _, err := runCLI(t, "gen", "model", "--locale", "xx-XX")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xx-XX")
}

func TestGenModelWritesEveryLocaleToOut(t *testing.T) {
	src := loadSources(t)
	dir := filepath.Join(t.TempDir(), "skill-package")

	out, stderr, err := runCLI(t, "gen", "model", "--out", dir)
	require.NoError(t, err)
	assert.Empty(t, out, "nothing is printed to stdout")

	for _, locale := range src.LocaleNames() {
		path := filepath.Join(dir, "interactionModels", "custom", locale+".json")
		assert.Contains(t, stderr, "wrote "+path)

		b, err := os.ReadFile(path)
		require.NoError(t, err, locale)

		var model skillmodel.InteractionModel
		require.NoError(t, json.Unmarshal(b, &model), locale)
		assert.Equal(t, src.Locales[locale].Invocation, model.InteractionModel.LanguageModel.InvocationName, locale)
	}
}

func TestGenModelWritesOnlyTheChosenLocalesToOut(t *testing.T) {
	dir := t.TempDir()

	_, _, err := runCLI(t, "gen", "model", "-o", dir, "-l", "de-DE", "-l", "it-IT")
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(dir, "interactionModels", "custom"))
	require.NoError(t, err)

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	assert.Equal(t, []string{"de-DE.json", "it-IT.json"}, names)
}

func TestGenModelReportsWriteFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, _, err := runCLI(t, "gen", "model", "--out", file, "--locale", "en-AU")
	require.Error(t, err, "the output directory can't be created under a file")

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "interactionModels", "custom", "en-AU.json"), 0o700))

	_, _, err = runCLI(t, "gen", "model", "--out", dir, "--locale", "en-AU")
	require.Error(t, err, "a directory is in the file's place")
}
