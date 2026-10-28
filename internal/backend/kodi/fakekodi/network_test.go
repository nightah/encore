package fakekodi

import (
	"context"
	"net/http"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetwork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := NewNetwork(t)
		lounge := n.New(Options{MAC: "DC:A6:32:00:00:01"})
		bedroom := n.New(Options{MAC: "DC:A6:32:00:00:02"})

		assert.Equal(t, "http://kodi1.test:8080/jsonrpc", lounge.URL())
		assert.Equal(t, "ws://kodi1.test:9090/jsonrpc", lounge.WebSocketURL())
		assert.Equal(t, "http://kodi2.test:8080/jsonrpc", bedroom.URL())
		assert.Same(t, n.Client(), lounge.Client(), "one client reaches every fake")

		// Each host reaches its own fake.
		for s, mac := range map[*Server]string{lounge: "DC:A6:32:00:00:01", bedroom: "DC:A6:32:00:00:02"} {
			_, resp := post(t, s, "", "", "XBMC.GetInfoLabels", map[string]any{"labels": []string{"Network.MacAddress"}})
			assert.Contains(t, string(resp.Result), mac)
		}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://kodi9.test:8080/jsonrpc", http.NoBody)
		require.NoError(t, err)

		_, err = n.Client().Do(req) //nolint:bodyclose // there is no response
		require.ErrorIs(t, err, syscall.ECONNREFUSED, "a host without a fake refuses the connection")
	})
}

func TestNetworkRestartAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewNetwork(t).New(Options{})
		conn := dial(t, s)

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		read := make(chan error, 1)

		go func() {
			_, _, err := conn.Read(ctx)
			read <- err
		}()

		s.Restart(t)
		synctest.Wait()

		require.Len(t, read, 1, "a restart drops the WebSocket")
		require.Error(t, <-read)
		assert.Zero(t, s.WebSocketClients())

		status, resp := post(t, s, "", "", "JSONRPC.Ping", nil)
		assert.Equal(t, http.StatusOK, status, "and serves again straight away")
		assert.JSONEq(t, `"pong"`, string(resp.Result))

		s.Close()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL(), http.NoBody)
		require.NoError(t, err)

		_, err = s.Client().Do(req) //nolint:bodyclose // there is no response
		require.ErrorIs(t, err, syscall.ECONNREFUSED, "a closed fake refuses connections")
	})
}
