package cli

import (
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

	for _, tc := range []struct {
		address, want string
	}{
		{":8080", "http://127.0.0.1:8080/api/v1/healthz"},
		{"0.0.0.0:8080", "http://127.0.0.1:8080/api/v1/healthz"},
		{"[::]:9000", "http://127.0.0.1:9000/api/v1/healthz"},
		{"127.0.0.1:8080", "http://127.0.0.1:8080/api/v1/healthz"},
		{"192.0.2.10:80", "http://192.0.2.10:80/api/v1/healthz"},
		{"[::1]:8080", "http://[::1]:8080/api/v1/healthz"},
		{"encore.local:8443", "http://encore.local:8443/api/v1/healthz"},
	} {
		got, err := healthURL(tc.address)
		require.NoError(t, err, tc.address)
		assert.Equal(t, tc.want, got, tc.address)
	}

	for _, bad := range []string{"", "8080", "localhost", "[::1"} {
		_, err := healthURL(bad)
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), "parsing server.address", bad)
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
	address, _ := healthServer(t, http.StatusServiceUnavailable)

	_, _, err := runCLI(t, "healthcheck", "--server.address", address)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 503")
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
}

func TestHealthcheckTakesNoArguments(t *testing.T) {
	_, _, err := runCLI(t, "healthcheck", "now")
	require.Error(t, err)
}
