package docsgen

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/cli"
)

func noop(*cobra.Command, []string) error { return nil }

func testTree() *cobra.Command {
	root := &cobra.Command{Use: "encore", Short: "Control Kodi with Alexa"}
	root.PersistentFlags().StringP("config", "c", "", "path to a YAML config file")

	serve := &cobra.Command{Use: "serve", Short: "Run the server", Long: "Runs the server.\nKeeps running.\n\n\n\nStops on a signal.", Aliases: []string{"run"}, RunE: noop}
	serve.Flags().String("server.address", "", `listen address (default ":8080")`)
	serve.Flags().Bool("debug", false, "verbose logging")
	serve.Flags().StringSlice("tag", nil, "tags")
	serve.Flags().String("secret", "", "not documented")
	_ = serve.Flags().MarkHidden("secret")

	gen := &cobra.Command{Use: "gen", Short: "Developer tools"}
	gen.AddCommand(&cobra.Command{Use: "lint", Short: "Check the model", RunE: noop})

	root.AddCommand(serve, gen, &cobra.Command{Use: "internal", Hidden: true, RunE: noop})
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	return root
}

func TestCommandsFlattensTheTree(t *testing.T) {
	cmds := Commands(testTree())

	var paths []string
	for _, c := range cmds {
		paths = append(paths, c.Path)
	}

	assert.Equal(t, []string{"encore gen", "encore gen lint", "encore serve"}, paths, "depth first, hidden, help and completion left out")

	gen, lint, serve := cmds[0], cmds[1], cmds[2]
	assert.False(t, gen.Runnable, "a group of subcommands isn't runnable")
	assert.True(t, lint.Runnable)
	assert.Equal(t, "encore serve [flags]", serve.Use)
	assert.Equal(t, []string{"run"}, serve.Aliases)
	assert.Equal(t, []string{"Runs the server. Keeps running.", "Stops on a signal."}, serve.Long, "paragraphs are joined lines, blank runs dropped")
	assert.Nil(t, lint.Long)

	require.Len(t, serve.Flags, 3, "the hidden flag is left out, and persistent flags belong to the root")
	assert.Equal(t, Flag{Name: "debug", Type: "bool", Usage: "verbose logging"}, serve.Flags[0], "false isn't shown as a bool's default")
	assert.Equal(t, Flag{Name: "server.address", Type: "string", Default: ":8080", Usage: `listen address (default ":8080")`}, serve.Flags[1])
	assert.Equal(t, Flag{Name: "tag", Type: "stringSlice", Usage: "tags"}, serve.Flags[2], "an empty list has no default")
}

func TestCLIReference(t *testing.T) {
	page, err := CLIReference(testTree())
	require.NoError(t, err)

	s := string(page)
	assert.Contains(t, s, GeneratedMarker)
	assert.Contains(t, s, "encore gen lint")
	assert.Contains(t, s, "Stops on a signal.")
	assert.Contains(t, s, "--config")
	assert.NotContains(t, s, "--secret")
	assert.NotContains(t, s, "encore internal")
}

func TestCLIReferenceCoversEncoresCommands(t *testing.T) {
	t.Setenv("ENCORE_CONFIG", "")

	page, err := CLIReference(cli.NewRootCommand(io.Discard, io.Discard))
	require.NoError(t, err)

	for _, cmd := range []string{"encore serve", "encore restore", "encore healthcheck", "encore version", "encore gen lint", "encore gen model"} {
		assert.Contains(t, string(page), cmd)
	}

	assert.NotContains(t, string(page), "encore completion")
}

func TestParagraphs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   \n\n  ", nil},
		{"one", []string{"one"}},
		{"one\ntwo", []string{"one two"}},
		{"  one  \n   two\n\nthree  four", []string{"one two", "three four"}},
		{"one\n\n\n\ntwo", []string{"one", "two"}},
	} {
		assert.Equal(t, tc.want, paragraphs(tc.in), "%q", tc.in)
	}
}

func TestFlagsKeepARegisteredDefaultOverTheUsageText(t *testing.T) {
	set := pflag.NewFlagSet("test", pflag.ContinueOnError)
	set.String("level", "warn", `log level (default "info")`)
	set.Int("limit", 0, "limit")
	set.Bool("yes", true, "assume yes")

	got := map[string]string{}
	for _, f := range flags(set) {
		got[f.Name] = f.Default
	}

	assert.Equal(t, map[string]string{"level": "warn", "limit": "0", "yes": "true"}, got)
	assert.True(t, strings.HasPrefix(flags(set)[0].Usage, "log level"))
}
