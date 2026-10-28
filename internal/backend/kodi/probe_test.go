package kodi_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
)

func TestProbeVersion(t *testing.T) {
	// The fake Kodi requires a password over HTTP, as a real one usually does; the WebSocket answers without one.
	srv := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "s3cret"})

	u, err := url.Parse(srv.WebSocketURL())
	require.NoError(t, err)

	host, port, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)

	p, err := strconv.Atoi(port)
	require.NoError(t, err)

	v, err := kodi.ProbeVersion(t.Context(), host, p)
	require.NoError(t, err)
	assert.Equal(t, "21.3", v)

	_, err = kodi.ProbeVersion(t.Context(), host, 0)
	assert.Error(t, err, "no WebSocket port, nothing to ask")
}

// probeServer answers a version probe's WebSocket with replies, one per message, then closes.
func probeServer(t *testing.T, replies ...string) (string, int) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = c.CloseNow() }()

		if _, _, err := c.Read(r.Context()); err != nil {
			return
		}

		for _, reply := range replies {
			if err := c.Write(r.Context(), websocket.MessageText, []byte(reply)); err != nil {
				return
			}
		}

		_ = c.Close(websocket.StatusNormalClosure, "")
	}))
	t.Cleanup(srv.Close)

	host, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)

	p, err := strconv.Atoi(port)
	require.NoError(t, err)

	return host, p
}

func TestProbeVersionReplies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replies []string
		want    string
		err     string
	}{
		{"skips notifications and noise", []string{
			`{"jsonrpc":"2.0","method":"Player.OnPlay","params":{}}`,
			`garbage`,
			`{"jsonrpc":"2.0","id":2,"result":{"version":{"major":1,"minor":0}}}`,
			`{"jsonrpc":"2.0","id":1,"result":{"version":{"major":22,"minor":0,"tag":"beta","tagversion":"3"}}}`,
		}, "22.0-beta3", ""},
		{"no version", []string{`{"jsonrpc":"2.0","id":1,"result":{}}`}, "", "no version in the reply"},
		{"closes without replying", nil, "", "kodi:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, port := probeServer(t, tc.replies...)

			v, err := kodi.ProbeVersion(t.Context(), host, port)

			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, v)
		})
	}
}

func TestProbeVersionOfAPortWithoutWebSocket(t *testing.T) {
	// Something else answers on the port: a web server that refuses the upgrade.
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	addr := srv.Listener.Addr().(*net.TCPAddr)

	_, err := kodi.ProbeVersion(t.Context(), addr.IP.String(), addr.Port)
	require.ErrorContains(t, err, "kodi:")
	assert.ErrorContains(t, err, "404", "the refusal says what answered")
}

func TestProbeVersionNothingListening(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())

	_, err = kodi.ProbeVersion(t.Context(), "127.0.0.1", port)
	require.ErrorContains(t, err, "kodi:")
}
