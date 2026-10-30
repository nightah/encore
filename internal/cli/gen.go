package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
)

func newGenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Developer tools for the interaction model",
	}

	cmd.AddCommand(newGenLintCommand(), newGenModelCommand(), newGenNLUCommand(), newGenOpenCommand())

	return cmd
}

func newGenLintCommand() *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Check the interaction model and response sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, err := skillmodel.Load(i18n.FS)
			if err != nil {
				return err
			}

			findings := skillmodel.Lint(src)
			for _, f := range findings {
				fmt.Fprintln(cmd.OutOrStdout(), f)
			}

			failed := skillmodel.HasErrors(findings) || (strict && len(findings) > 0)
			fmt.Fprintf(cmd.OutOrStdout(), "%d finding(s) across %d locale(s)\n", len(findings), len(src.Locales))

			if failed {
				return errSilent
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "fail on warnings too")

	return cmd
}

func newGenModelCommand() *cobra.Command {
	var (
		locales    []string
		invocation string
		outDir     string
	)

	cmd := &cobra.Command{
		Use:   "model",
		Short: "Write interaction model JSON for one or more locales",
		Long: "Builds interaction models from the embedded sources without library slot values.\n" +
			"With --out, writes interactionModels/custom/<locale>.json under that directory\n" +
			"(the skill package layout); otherwise prints a single locale to stdout.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, err := skillmodel.Load(i18n.FS)
			if err != nil {
				return err
			}

			if len(locales) == 0 {
				locales = src.LocaleNames()
			}

			if outDir == "" && len(locales) != 1 {
				return fmt.Errorf("choose one --locale to print, or use --out (available: %v)", src.LocaleNames())
			}

			for _, locale := range locales {
				model, err := skillmodel.Build(src, locale, skillmodel.BuildOptions{Invocation: invocation})
				if err != nil {
					return err
				}

				b, err := json.MarshalIndent(model, "", "  ")
				if err != nil {
					return err
				}

				b = append(b, '\n')

				if outDir == "" {
					_, err = cmd.OutOrStdout().Write(b)

					return err
				}

				path := filepath.Join(outDir, "interactionModels", "custom", locale+".json")
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					return err
				}

				if err := os.WriteFile(path, b, 0o644); err != nil { //nolint:gosec // generated JSON, not a secret
					return err
				}

				fmt.Fprintln(cmd.ErrOrStderr(), "wrote", path)
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&locales, "locale", "l", nil, "locales to build (default all)")
	cmd.Flags().StringVar(&invocation, "invocation", "", "override the invocation name")
	cmd.Flags().StringVarP(&outDir, "out", "o", "", "write a skill package interactionModels tree here")

	return cmd
}
