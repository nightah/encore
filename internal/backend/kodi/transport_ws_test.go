package kodi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// scriptedKodi serves JSON-RPC from hand-written handlers, for replies the fake Kodi never sends. WebSocket upgrades are refused while refuse is set, or when ws is nil.
type scriptedKodi struct {
	ws     func(ctx context.Context, c *websocket.Conn)
	post   http.HandlerFunc
	refuse atomic.Bool
	posts  atomic.Int32
	dials  atomic.Int32
}

func (k *scriptedKodi) start(t *testing.T) Config {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "" {
			k.dials.Add(1)

			if k.ws == nil || k.refuse.Load() {
				http.Error(w, "no websocket", http.StatusServiceUnavailable)

				return
			}

			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}

			defer func() { _ = c.CloseNow() }()

			k.ws(r.Context(), c)

			return
		}

		k.posts.Add(1)

		if k.post == nil {
			http.Error(w, "no", http.StatusNotFound)

			return
		}

		k.post(w, r)
	}))
	t.Cleanup(srv.Close)

	host, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)

	p, err := strconv.Atoi(port)
	require.NoError(t, err)

	return Config{Host: host, Port: p, WebSocketPort: p, Timeout: 2 * time.Second, HeartbeatInterval: time.Hour, ReconnectMin: 5 * time.Millisecond, ReconnectMax: 20 * time.Millisecond}
}

// readRequest reads the next request the client sends.
func readRequest(ctx context.Context, c *websocket.Conn) (request, json.RawMessage, error) {
	_, data, err := c.Read(ctx)
	if err != nil {
		return request{}, nil, err
	}

	var req struct {
		request

		ID json.RawMessage `json:"id"`
	}

	err = json.Unmarshal(data, &req)

	return req.request, req.ID, err
}

func write(ctx context.Context, c *websocket.Conn, msg string) {
	_ = c.Write(ctx, websocket.MessageText, []byte(msg))
}

func TestWebSocketSkipsWhatIsNotAReply(t *testing.T) {
	k := &scriptedKodi{ws: func(ctx context.Context, c *websocket.Conn) {
		for {
			_, id, err := readRequest(ctx, c)
			if err != nil {
				return
			}

			write(ctx, c, `not json`)
			write(ctx, c, `{"jsonrpc":"2.0","id":null}`)
			write(ctx, c, `{"jsonrpc":"2.0","id":"abc","result":"wrong"}`)
			write(ctx, c, `{"jsonrpc":"2.0","id":987654,"result":"nobody asked"}`)
			write(ctx, c, `{"jsonrpc":"2.0","method":"Player.OnPause","params":{"data":{}}}`)
			write(ctx, c, `{"jsonrpc":"2.0","id":`+string(id)+`,"result":"pong"}`)
		}
	}}
	tr := NewTransport(k.start(t), nil)

	var (
		mu    sync.Mutex
		heard []kodirpc.NotificationName
	)

	tr.Listen(Listener{Notification: func(n Notification) {
		mu.Lock()
		defer mu.Unlock()

		heard = append(heard, n.Method)
	}})
	runTransport(t, tr)

	require.NoError(t, ping(t.Context(), tr), "the reply is found among the noise")
	assert.True(t, tr.Connected(), "noise doesn't drop the connection")
	assert.Zero(t, k.posts.Load())

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(heard) == 1
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, []kodirpc.NotificationName{"Player.OnPause"}, heard, "only a message naming a method is a notification")
}

func TestWebSocketDroppedMidCallIsUnreachable(t *testing.T) {
	k := &scriptedKodi{ws: func(ctx context.Context, c *websocket.Conn) {
		req, _, err := readRequest(ctx, c)
		if err != nil || req.Method != "Player.Stop" {
			return
		}

		_ = c.CloseNow()
	}}
	tr := NewTransport(k.start(t), nil)
	runTransport(t, tr)

	err := tr.Call(t.Context(), "Player.Stop", map[string]int{"playerid": 1}, nil)

	require.ErrorIs(t, err, media.ErrUnreachable)
	assert.Contains(t, err.Error(), "websocket closed")
	assert.Zero(t, k.posts.Load(), "a call that may have been carried out isn't sent again over HTTP")
}

func TestWebSocketCallerGivesUpWhileWaiting(t *testing.T) {
	k := &scriptedKodi{ws: func(ctx context.Context, c *websocket.Conn) {
		for {
			if _, _, err := readRequest(ctx, c); err != nil {
				return
			}
		}
	}}
	tr := NewTransport(k.start(t), nil)
	runTransport(t, tr)

	ws := tr.conn()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()

	err := tr.Call(ctx, "Player.Stop", nil, nil)

	require.ErrorIs(t, err, media.ErrUnreachable, "an unanswered call times out as unreachable")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Same(t, ws, tr.conn(), "the connection stays up for other calls")

	ws.mu.Lock()
	defer ws.mu.Unlock()
	assert.Empty(t, ws.pending, "the abandoned call is forgotten")
}

func TestUnencodableParams(t *testing.T) {
	k := &scriptedKodi{ws: func(ctx context.Context, _ *websocket.Conn) { <-ctx.Done() }}
	cfg := k.start(t)

	for _, ws := range []bool{false, true} {
		cfg.DisableWebSocket = !ws
		tr := NewTransport(cfg, nil)

		if ws {
			runTransport(t, tr)
		}

		err := tr.Call(t.Context(), "Input.SendText", map[string]any{"text": make(chan int)}, nil)

		require.ErrorContains(t, err, "encoding params", "websocket=%v", ws)
		require.NotErrorIs(t, err, media.ErrUnreachable, "a bug here is not Kodi's absence")
	}

	assert.Zero(t, k.posts.Load())
}

func TestHTTPReplies(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		unreachable bool
		contains    string
	}{
		{"server error", http.StatusInternalServerError, "", true, "HTTP 500"},
		{"unauthorised", http.StatusUnauthorized, "", true, "unauthorized"},
		{"not JSON", http.StatusOK, "<html>", true, "decoding response"},
		{"wrong result shape", http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"not":"a string"}}`, false, "decoding result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := &scriptedKodi{post: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}}
			cfg := k.start(t)
			cfg.DisableWebSocket = true

			err := ping(t.Context(), NewTransport(cfg, nil))

			require.ErrorContains(t, err, tc.contains)
			assert.Equal(t, tc.unreachable, errors.Is(err, media.ErrUnreachable))

			if tc.status == http.StatusUnauthorized {
				require.ErrorIs(t, err, ErrUnauthorized)
			}
		})
	}
}

func TestRunRedialsUntilKodiAcceptsTheWebSocket(t *testing.T) {
	k := &scriptedKodi{ws: func(ctx context.Context, _ *websocket.Conn) { <-ctx.Done() }}
	k.refuse.Store(true)
	tr := NewTransport(k.start(t), nil)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = tr.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, func() bool { return k.dials.Load() >= 3 }, 2*time.Second, time.Millisecond, "a refused WebSocket is retried")
	assert.False(t, tr.Connected())

	k.refuse.Store(false)
	require.Eventually(t, tr.Connected, 2*time.Second, time.Millisecond, "and connects once Kodi accepts")
}

func TestRunWithoutWebSocketNeverDials(t *testing.T) {
	k := &scriptedKodi{ws: func(ctx context.Context, _ *websocket.Conn) { <-ctx.Done() }}
	cfg := k.start(t)
	cfg.DisableWebSocket = true
	tr := NewTransport(cfg, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	require.NoError(t, tr.Run(ctx))
	assert.Zero(t, k.dials.Load())
	assert.False(t, tr.Connected())
}
