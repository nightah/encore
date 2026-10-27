package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/nightah/encore/internal/buildinfo"
	"github.com/nightah/encore/internal/config"
)

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := NewRootCommand(os.Stdout, os.Stderr)
	if err := cmd.ExecuteContext(ctx); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}

		return 1
	}

	return 0
}

// NewRootCommand builds the command tree writing to the given streams.
func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "encore",
		Short:         "Control Kodi with Alexa",
		Long:          "Encore is a self-hosted Alexa skill server for Kodi, with a web interface for setup.",
		Version:       buildinfo.Get().String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetVersionTemplate("{{ .Version }}\n")

	root.PersistentFlags().StringP("config", "c", os.Getenv(config.EnvPrefix+"CONFIG"),
		"path to a YAML config file (env "+config.EnvPrefix+"CONFIG)")

	root.AddCommand(
		newServeCommand(),
		newRestoreCommand(),
		newHealthcheckCommand(),
		newVersionCommand(),
		newGenCommand(),
	)

	return root
}

// loadConfig loads configuration for cmd, honouring --config and any of cmd's flags named after config keys (e.g. --server.address). Without --config it uses encore.yml in the data directory when that file exists, which is how the container image finds /config/encore.yml.
func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, err
	}

	if path == "" {
		path = defaultConfigFile()
	}

	return config.Load(config.Options{File: path, Flags: cmd.Flags()})
}

// defaultConfigFile returns encore.yml in the data directory if it exists. The data directory comes from ENCORE_DATA_DIR or the default, since the config file itself can't be consulted yet.
func defaultConfigFile() string {
	dir := os.Getenv(config.EnvPrefix + "DATA_DIR")
	if dir == "" {
		dir = config.Defaults().DataDir
	}

	path := filepath.Join(dir, config.DefaultFileName)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() { //nolint:gosec // the operator chooses the data directory
		return path
	}

	return ""
}

// newLogger returns the logger and its level, which the settings page can change while Encore runs.
func newLogger(w io.Writer, cfg config.Log) (*slog.Logger, *slog.LevelVar, error) {
	parsed, err := config.ParseLevel(cfg.Level)
	if err != nil {
		return nil, nil, err
	}

	level := new(slog.LevelVar)
	level.Set(parsed)

	opts := &slog.HandlerOptions{Level: level}

	switch logFormat(cfg.Format, w) {
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), level, nil
	case "pretty":
		return slog.New(tint.NewTextHandler(w, &tint.Options{
			Level:      level,
			TimeFormat: "15:04:05.000",
			NoColor:    os.Getenv("NO_COLOR") != "",
			// Errors in red, so failures stand out among the debug lines.
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == "error" && a.Value.Kind() == slog.KindAny && a.Value.Any() != nil {
					return tint.Attr(9, a)
				}

				return a
			},
		})), level, nil
	default:
		return slog.New(slog.NewTextHandler(w, opts)), level, nil
	}
}

// logFormat resolves log.format: auto is pretty when w is a terminal and text otherwise, so containers, systemd and files get plain lines.
func logFormat(format string, w io.Writer) string {
	if format != "auto" {
		return format
	}

	if f, ok := w.(*os.File); ok && (isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())) {
		return "pretty"
	}

	return "text"
}
