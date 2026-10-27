package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nightah/encore/internal/buildinfo"
)

func newVersionCommand() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Get()

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")

				return enc.Encode(info)
			}

			_, err := fmt.Fprintln(cmd.OutOrStdout(), info.String())

			return err
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")

	return cmd
}
