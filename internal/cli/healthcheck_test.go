package cli

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/server"
)

func TestHealthURL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     string
		expected string
		err      string
	}{
		{name: "ShouldUseLoopbackForAnEmptyHost", have: ":8080", expected: "http://127.0.0.1:8080/api/v1/healthz"},
		{name: "ShouldUseLoopbackForTheIPv4Wildcard", have: "0.0.0.0:8080", expected: "http://127.0.0.1:8080/api/v1/healthz"},
		{name: "ShouldUseLoopbackForTheIPv6Wildcard", have: "[::]:9000", expected: "http://127.0.0.1:9000/api/v1/healthz"},
		{name: "ShouldKeepIPv4Loopback", have: "127.0.0.1:8080", expected: "http://127.0.0.1:8080/api/v1/healthz"},
		{name: "ShouldKeepAnIPv4Address", have: "192.0.2.10:80", expected: "http://192.0.2.10:80/api/v1/healthz"},
		{name: "ShouldKeepIPv6Loopback", have: "[::1]:8080", expected: "http://[::1]:8080/api/v1/healthz"},
		{name: "ShouldKeepAHostName", have: "encore.local:8443", expected: "http://encore.local:8443/api/v1/healthz"},
		{name: "ShouldRejectAnEmptyAddress", have: "", err: "parsing server.address"},
		{name: "ShouldRejectAPortWithoutAColon", have: "8080", err: "parsing server.address"},
		{name: "ShouldRejectAHostWithoutAPort", have: "localhost", err: "parsing server.address"},
		{name: "ShouldRejectAnUnclosedIPv6Address", have: "[::1", err: "parsing server.address"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := healthURL(tc.have)

			if tc.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

// healthServer answers the health check with status and records the paths it was asked for.
func healthServer(t *testing.T, status int) (address string, paths *[]string) {
	t.Helper()

	var seen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "http://"), &seen
}

func TestHealthcheckHealthy(t *testing.T) {
	address, paths := healthServer(t, http.StatusOK)

	_, _, err := runCLI(t, "healthcheck", "--server.address", address)
	require.NoError(t, err)
	assert.Equal(t, []string{"GET " + server.PathHealthz}, *paths)
}

func TestHealthcheckUnhealthy(t *testing.T) {
	testCases := []struct {
		name string
		have int
	}{
		{name: "ShouldFailOnNoContent", have: http.StatusNoContent},
		{name: "ShouldFailOnFound", have: http.StatusFound},
		{name: "ShouldFailOnNotFound", have: http.StatusNotFound},
		{name: "ShouldFailOnInternalServerError", have: http.StatusInternalServerError},
		{name: "ShouldFailOnServiceUnavailable", have: http.StatusServiceUnavailable},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			address, _ := healthServer(t, tc.have)

			_, _, err := runCLI(t, "healthcheck", "--server.address", address)
			require.Error(t, err, "only 200 is healthy")
			assert.Contains(t, err.Error(), fmt.Sprintf("status %d", tc.have))
		})
	}
}

func TestHealthcheckNothingListening(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := l.Addr().String()
	require.NoError(t, l.Close())

	_, _, err = runCLI(t, "healthcheck", "--server.address", address)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking http://"+address+server.PathHealthz)
}

func TestHealthcheckUsesTheConfiguredAddress(t *testing.T) {
	address, paths := healthServer(t, http.StatusOK)
	t.Setenv("ENCORE_SERVER_ADDRESS", address)

	_, _, err := runCLI(t, "healthcheck")
	require.NoError(t, err)
	assert.Len(t, *paths, 1)
}

func TestHealthcheckRejectsAnUnparsableAddress(t *testing.T) {
	_, _, err := runCLI(t, "healthcheck", "--server.address", "localhost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.address")

	// A host and port, but no host a URL can name.
	_, _, err = runCLI(t, "healthcheck", "--server.address", "encore box:8080")
	require.ErrorContains(t, err, `invalid character " " in host name`)
	assert.NotContains(t, err.Error(), "checking", "nothing is requested")
}

func TestHealthcheckTakesNoArguments(t *testing.T) {
	_, _, err := runCLI(t, "healthcheck", "now")
	require.Error(t, err)
}
