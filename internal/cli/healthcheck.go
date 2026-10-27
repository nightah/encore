package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/nightah/encore/internal/server"
)

// newHealthcheckCommand checks a running server, for the container HEALTHCHECK and other probes; it reads the same config as serve, so it finds the right port.
func newHealthcheckCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Exit 0 if the local server is healthy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}

			url, err := healthURL(cfg.Server.Address)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
			if err != nil {
				return err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("checking %s: %w", url, err)
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("checking %s: status %d", url, resp.StatusCode)
			}

			return nil
		},
	}

	cmd.Flags().String("server.address", "", "address the server listens on (default \":8080\")")

	return cmd
}

// healthURL turns a listen address into a URL on the loopback interface. The server answers the health check without the base path, which the web interface can change.
func healthURL(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parsing server.address %q: %w", address, err)
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	return "http://" + net.JoinHostPort(host, port) + server.PathHealthz, nil
}
