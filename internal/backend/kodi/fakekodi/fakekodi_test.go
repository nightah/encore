package fakekodi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

func post(t *testing.T, s *Server, user, pass, method string, params any) (int, rpcResponse) {
	t.Helper()

	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL(), bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := s.Client().Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	var out rpcResponse
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}

	return resp.StatusCode, out
}

func TestHTTPRoundTrip(t *testing.T) {
	s := New(t, Options{})

	_, resp := post(t, s, "", "", "JSONRPC.Ping", nil)
	require.Nil(t, resp.Error)
	assert.JSONEq(t, `"pong"`, string(resp.Result))

	_, resp = post(t, s, "", "", "JSONRPC.Version", nil)
	assert.JSONEq(t, `{"version":{"major":13,"minor":5,"patch":0}}`, string(resp.Result))

	_, resp = post(t, s, "", "", "No.Such", nil)
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeMethodNotFound, resp.Error.Code)

	assert.Len(t, s.Calls(), 3)
}

func TestBasicAuth(t *testing.T) {
	s := New(t, Options{Username: "kodi", Password: "secret"})

	status, _ := post(t, s, "", "", "JSONRPC.Ping", nil)
	assert.Equal(t, http.StatusUnauthorized, status)

	status, _ = post(t, s, "kodi", "secret", "JSONRPC.Ping", nil)
	assert.Equal(t, http.StatusOK, status)
}

func TestLibraryQueries(t *testing.T) {
	s := New(t, Options{})

	_, resp := post(t, s, "", "", "VideoLibrary.GetMovies", map[string]any{
		"filter": map[string]any{"field": "title", "operator": "contains", "value": "dune"},
	})

	var movies struct {
		Movies []Movie        `json:"movies"`
		Limits map[string]int `json:"limits"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &movies))
	assert.Len(t, movies.Movies, 3)
	assert.Equal(t, 3, movies.Limits["total"])

	_, resp = post(t, s, "", "", "VideoLibrary.GetEpisodes", map[string]any{
		"tvshowid": 1,
		"filter":   map[string]any{"field": "playcount", "operator": "is", "value": "0"},
		"limits":   map[string]int{"start": 0, "end": 1},
	})

	var episodes struct {
		Episodes []Episode `json:"episodes"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &episodes))
	require.Len(t, episodes.Episodes, 1)
	assert.Equal(t, "Woe's Hollow", episodes.Episodes[0].Title, "next unwatched episode")
}

func TestNotFoundErrorDependsOnVersion(t *testing.T) {
	omega := New(t, Options{})
	_, resp := post(t, omega, "", "", "Player.Open", map[string]any{"item": map[string]int{"movieid": 999}})
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeInvalidParams, resp.Error.Code)

	piers := New(t, Options{Version: [3]int{13, 1000, 0}})
	_, resp = post(t, piers, "", "", "Player.Open", map[string]any{"item": map[string]int{"movieid": 999}})
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeNotFound, resp.Error.Code)
}

func TestHandleOverride(t *testing.T) {
	s := New(t, Options{})
	s.Handle("JSONRPC.Ping", func(json.RawMessage) (any, *Error) {
		return nil, &Error{Code: CodeUnavailable, Message: "Unavailable."}
	})

	_, resp := post(t, s, "", "", "JSONRPC.Ping", nil)
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeUnavailable, resp.Error.Code)
}

// dial connects to the fake's WebSocket.
func dial(t *testing.T, s *Server) *websocket.Conn {
	t.Helper()

	conn, resp, err := websocket.Dial(t.Context(), s.WebSocketURL(), &websocket.DialOptions{HTTPClient: s.Client()})
	require.NoError(t, err)

	if resp.Body != nil {
		require.NoError(t, resp.Body.Close())
	}

	t.Cleanup(func() { _ = conn.CloseNow() })

	return conn
}

func TestWebSocketCallsAndNotifications(t *testing.T) {
	s := New(t, Options{})
	ctx := t.Context()
	conn := dial(t, s)

	require.NoError(t, conn.Write(ctx, websocket.MessageText,
		[]byte(`{"jsonrpc":"2.0","id":7,"method":"Player.Open","params":{"item":{"movieid":3}}}`)))

	// Kodi sends the notification and the response on the same socket, in either order; collect both.
	var gotResponse, gotNotification bool

	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	for !gotResponse || !gotNotification {
		_, data, err := conn.Read(readCtx)
		require.NoError(t, err)

		var msg map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &msg))

		switch {
		case msg["id"] != nil:
			assert.JSONEq(t, `"OK"`, string(msg["result"]))

			gotResponse = true
		case string(msg["method"]) == `"Player.OnPlay"`:
			assert.Contains(t, string(msg["params"]), `"id":3`)

			gotNotification = true
		}
	}

	_, resp := post(t, s, "", "", "Player.GetItem", nil)
	assert.Contains(t, string(resp.Result), "Dune: Part Two")
}

// A client counts itself connected once it has the handshake response, which can be before the server has registered it; a notification sent straight away must still reach it.
func TestNotifyReachesAClientStillBeingRegistered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewNetwork().New(t, Options{})
		s.afterAccept = func() { time.Sleep(100 * time.Millisecond) }

		conn := dial(t, s)
		s.Notify("VideoLibrary.OnScanFinished", nil)

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		_, data, err := conn.Read(ctx)
		require.NoError(t, err, "the notification was lost")
		assert.Contains(t, string(data), `"method":"VideoLibrary.OnScanFinished"`)
		assert.Equal(t, 1, s.WebSocketClients())
	})
}
