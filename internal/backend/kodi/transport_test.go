package kodi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

func testConfig(t *testing.T, s *fakekodi.Server) Config {
	t.Helper()

	cfg, err := ConfigFromURLs(s.URL(), s.WebSocketURL())
	require.NoError(t, err)

	cfg.Timeout = 2 * time.Second
	cfg.HeartbeatInterval = time.Hour
	cfg.ReconnectMin = 10 * time.Millisecond
	cfg.ReconnectMax = 100 * time.Millisecond

	return cfg
}

// Waits for the WebSocket to connect.
func runTransport(t *testing.T, tr *Transport) {
	t.Helper()

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

	require.Eventually(t, tr.Connected, 5*time.Second, 5*time.Millisecond, "websocket never connected")
}

func ping(ctx context.Context, c kodirpc.Caller) error {
	var pong string
	if err := c.Call(ctx, kodirpc.MethodJSONRPCPing, nil, &pong); err != nil {
		return err
	}

	if pong != "pong" {
		return fmt.Errorf("ping answered %q", pong)
	}

	return nil
}

func TestConfigFromURLs(t *testing.T) {
	cfg, err := ConfigFromURLs("https://kodi:secret@media.lan:8443/jsonrpc", "wss://media.lan:9443/jsonrpc") // trufflehog:ignore
	require.NoError(t, err)

	assert.Equal(t, "https://media.lan:8443/jsonrpc", cfg.HTTPURL())
	assert.Equal(t, "wss://media.lan:9443/jsonrpc", cfg.WebSocketURL())
	assert.Equal(t, "kodi", cfg.Username)
	assert.Equal(t, "secret", cfg.Password)

	cfg, err = ConfigFromURLs("http://192.168.1.20", "")
	require.NoError(t, err)
	assert.Equal(t, "http://192.168.1.20:8080/jsonrpc", cfg.HTTPURL())
	assert.Equal(t, "ws://192.168.1.20:9090/jsonrpc", cfg.WebSocketURL())

	assert.Equal(t, "http://[fe80::1]:8080/jsonrpc", Config{Host: "fe80::1"}.HTTPURL())

	_, err = ConfigFromURLs("http://kodi:port/", "")
	require.Error(t, err)
}

func TestHTTPOnly(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "secret"})
	cfg := testConfig(t, s)
	cfg.DisableWebSocket = true
	cfg.Username, cfg.Password = "kodi", "secret"
	tr := NewTransport(cfg, nil)

	require.NoError(t, ping(t.Context(), tr))

	calls := s.Calls()
	require.Len(t, calls, 1)
	assert.False(t, calls[0].WebSocket)

	cfg.Password = "wrong"
	err := ping(t.Context(), NewTransport(cfg, nil))
	require.ErrorIs(t, err, media.ErrUnreachable)
	require.ErrorIs(t, err, ErrUnauthorized)
	assert.NotContains(t, err.Error(), "wrong", "credentials must not leak into errors")
}

func TestWebSocketConcurrentCalls(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	s.Handle("Test.Echo", func(params json.RawMessage) (any, *fakekodi.Error) {
		time.Sleep(time.Duration(rand.IntN(20)) * time.Millisecond)

		var p struct {
			N int `json:"n"`
		}
		_ = json.Unmarshal(params, &p)

		return p.N, nil
	})

	tr := NewTransport(testConfig(t, s), nil)
	runTransport(t, tr)

	const n = 64

	var (
		wg     sync.WaitGroup
		failed atomic.Int32
	)

	for i := range n {
		wg.Go(func() {
			var got int
			if err := tr.Call(t.Context(), "Test.Echo", map[string]int{"n": i}, &got); err != nil || got != i {
				failed.Add(1)
			}
		})
	}

	wg.Wait()
	assert.Zero(t, failed.Load(), "every response must reach its own caller")

	for _, c := range s.CallsTo("Test.Echo") {
		assert.True(t, c.WebSocket, "calls go over the websocket while it is up")
	}
}

func TestNotifications(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	tr := NewTransport(testConfig(t, s), nil)

	got := make(chan Notification, 4)
	stop := tr.Listen(Listener{Notification: func(n Notification) { got <- n }})

	runTransport(t, tr)

	s.Notify("VideoLibrary.OnUpdate", map[string]any{"item": map[string]any{"id": 7, "type": "movie"}})

	select {
	case n := <-got:
		assert.Equal(t, kodirpc.NotificationVideoLibraryOnUpdate, n.Method)
		assert.JSONEq(t, `{"sender":"xbmc","data":{"item":{"id":7,"type":"movie"}}}`, string(n.Params))
	case <-time.After(2 * time.Second):
		t.Fatal("no notification")
	}

	stop()
	s.Notify("VideoLibrary.OnUpdate", map[string]any{})
	require.NoError(t, ping(t.Context(), tr), "a round trip after the notification guarantees it was read")
	assert.Empty(t, got, "an unregistered listener gets nothing")
}

func TestReconnectAfterRestart(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	tr := NewTransport(testConfig(t, s), nil)

	var ups, downs atomic.Int32

	tr.Listen(Listener{State: func(up bool) {
		if up {
			ups.Add(1)
		} else {
			downs.Add(1)
		}
	}})

	runTransport(t, tr)
	s.Restart(t)

	require.Eventually(t, func() bool { return downs.Load() >= 1 && ups.Load() >= 2 && tr.Connected() },
		5*time.Second, 5*time.Millisecond, "transport must reconnect after Kodi restarts")

	before := len(s.Calls())
	require.NoError(t, ping(t.Context(), tr))
	assert.True(t, s.Calls()[before].WebSocket)
}

func TestHTTPFallbackWhileWebSocketDown(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	tr := NewTransport(testConfig(t, s), nil)
	runTransport(t, tr)

	s.SetWebSocket(false)
	require.Eventually(t, func() bool { return !tr.Connected() }, 2*time.Second, 5*time.Millisecond)

	before := len(s.Calls())
	require.NoError(t, ping(t.Context(), tr))
	assert.False(t, s.Calls()[before].WebSocket, "falls back to HTTP")

	s.SetWebSocket(true)
	require.Eventually(t, tr.Connected, 2*time.Second, 5*time.Millisecond, "reconnects once the websocket is back")
}

func TestHeartbeatDropsDeadConnection(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})

	var hang atomic.Bool

	s.Handle(kodirpc.MethodJSONRPCPing, func(json.RawMessage) (any, *fakekodi.Error) {
		if hang.Load() {
			time.Sleep(300 * time.Millisecond)
		}

		return "pong", nil
	})

	cfg := testConfig(t, s)
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.Timeout = 100 * time.Millisecond
	tr := NewTransport(cfg, nil)

	var downs atomic.Int32

	tr.Listen(Listener{State: func(up bool) {
		if !up {
			downs.Add(1)
		}
	}})

	runTransport(t, tr)

	require.Eventually(t, func() bool { return len(s.CallsTo(kodirpc.MethodJSONRPCPing)) >= 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Zero(t, downs.Load(), "healthy heartbeats keep the connection")

	hang.Store(true)
	require.Eventually(t, func() bool { return downs.Load() >= 1 }, 2*time.Second, 10*time.Millisecond, "an unanswered heartbeat drops the socket")
}

func TestRunIsShared(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	tr := NewTransport(testConfig(t, s), nil)

	ctx1, cancel1 := context.WithCancel(t.Context())
	ctx2, cancel2 := context.WithCancel(t.Context())

	done1, done2 := make(chan struct{}), make(chan struct{})

	go func() { _ = tr.Run(ctx1); close(done1) }()
	go func() { _ = tr.Run(ctx2); close(done2) }()

	require.Eventually(t, tr.Connected, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return s.WebSocketClients() == 1 }, 2*time.Second, 5*time.Millisecond)

	cancel1()
	<-done1
	assert.True(t, tr.Connected(), "the connection stays while another Run is active")

	cancel2()
	<-done2
	assert.False(t, tr.Connected())
}

func TestRunRestartKeepsNewConnection(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	tr := NewTransport(testConfig(t, s), nil)

	// The first connection's loop is held right after it connects, so it tears down only after the next Run has started.
	held, release := make(chan struct{}), make(chan struct{})
	reconnected := make(chan struct{}, 1)

	var ups atomic.Int32

	tr.Listen(Listener{State: func(up bool) {
		if !up {
			return
		}

		switch ups.Add(1) {
		case 1:
			close(held)
			<-release
		default:
			select {
			case reconnected <- struct{}{}:
			default:
			}
		}
	}})

	ctx1, cancel1 := context.WithCancel(t.Context())
	done1 := make(chan struct{})

	go func() { _ = tr.Run(ctx1); close(done1) }()

	<-held
	cancel1()

	require.Eventually(t, func() bool {
		tr.mu.Lock()
		defer tr.mu.Unlock()

		return tr.runs == 0
	}, 2*time.Second, time.Millisecond)

	ctx2, cancel2 := context.WithCancel(t.Context())
	done2 := make(chan struct{})

	t.Cleanup(func() { cancel2(); <-done2 })

	go func() { _ = tr.Run(ctx2); close(done2) }()

	select {
	case <-reconnected:
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	<-done1

	require.Eventually(t, tr.Connected, 2*time.Second, 5*time.Millisecond, "the old connection's teardown must not clear the new one")
}

func TestErrorMapping(t *testing.T) {
	versions := map[string][3]int{"omega": {13, 5, 0}, "piers": {13, 1000, 0}}

	for name, v := range versions {
		t.Run(name, func(t *testing.T) {
			s := fakekodi.New(t, fakekodi.Options{Version: v})
			tr := NewTransport(testConfig(t, s), nil)
			runTransport(t, tr)

			err := tr.Call(t.Context(), kodirpc.MethodPlayerOpen, map[string]any{"item": map[string]int{"movieid": 999}}, nil)
			require.ErrorIs(t, err, media.ErrNotFound)

			var rpcErr *RPCError
			require.ErrorAs(t, err, &rpcErr)
			assert.Equal(t, kodirpc.MethodPlayerOpen, rpcErr.Method)

			if v[1] >= 1000 {
				assert.Equal(t, CodeNotFound, rpcErr.Code)
			} else {
				assert.Equal(t, CodeInvalidParams, rpcErr.Code)
			}

			err = tr.Call(t.Context(), kodirpc.MethodVideoLibraryGetMovieDetails, map[string]int{"movieid": 999}, nil)
			require.ErrorIs(t, err, media.ErrNotFound)
		})
	}

	s := fakekodi.New(t, fakekodi.Options{})
	s.Handle("Player.Open", func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: CodeUnavailable, Message: "Unavailable."}
	})
	s.Handle("Application.SetVolume", func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: CodeInvalidParams, Message: "Invalid params."}
	})

	tr := NewTransport(testConfig(t, s), nil)

	err := tr.Call(t.Context(), "Player.Open", map[string]any{}, nil)
	require.ErrorIs(t, err, media.ErrUnavailable)
	require.NotErrorIs(t, err, media.ErrNotFound)

	err = tr.Call(t.Context(), "Application.SetVolume", map[string]any{}, nil)
	require.NotErrorIs(t, err, media.ErrNotFound, "invalid params elsewhere is not a missing item")

	err = tr.Call(t.Context(), "No.Such", nil, nil)

	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, CodeMethodNotFound, rpcErr.Code)

	s.Close()

	err = ping(t.Context(), tr)
	require.ErrorIs(t, err, media.ErrUnreachable)
}

func TestTimeoutIsUnreachable(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	s.Handle("Test.Slow", func(json.RawMessage) (any, *fakekodi.Error) {
		time.Sleep(200 * time.Millisecond)

		return "late", nil
	})

	for _, ws := range []bool{false, true} {
		cfg := testConfig(t, s)
		cfg.Timeout = 50 * time.Millisecond
		cfg.DisableWebSocket = !ws
		tr := NewTransport(cfg, nil)

		if ws {
			runTransport(t, tr)
		}

		err := tr.Call(t.Context(), "Test.Slow", nil, nil)
		require.ErrorIs(t, err, media.ErrUnreachable, "websocket=%v", ws)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err = tr.Call(ctx, "Test.Slow", nil, nil)
		require.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, media.ErrUnreachable, "a cancelled caller is not an unreachable Kodi")
	}
}

func TestClientVersionGate(t *testing.T) {
	omega := fakekodi.New(t, fakekodi.Options{})
	c := NewClient(testConfig(t, omega), nil)

	v, err := c.APIVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, kodirpc.APIVersion{Major: 13, Minor: 5}, v)

	_, err = c.PlayerGetChapters(t.Context(), kodirpc.PlayerGetChaptersParams{PlayerID: 1})
	require.ErrorIs(t, err, ErrUnsupported)
	assert.Empty(t, omega.CallsTo(kodirpc.MethodPlayerGetChapters), "unsupported methods are not sent")
	assert.Len(t, omega.CallsTo(kodirpc.MethodJSONRPCVersion), 1, "the version is cached")

	piers := fakekodi.New(t, fakekodi.Options{Version: [3]int{13, 1000, 0}})
	c = NewClient(testConfig(t, piers), nil)

	_, err = c.PlayerGetChapters(t.Context(), kodirpc.PlayerGetChaptersParams{PlayerID: 1})
	require.NotErrorIs(t, err, ErrUnsupported)
	assert.Len(t, piers.CallsTo(kodirpc.MethodPlayerGetChapters), 1)
}

func TestCallLogging(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "secret"})
	cfg := testConfig(t, s)
	cfg.DisableWebSocket = true
	cfg.Username, cfg.Password = "kodi", "secret"

	var buf bytes.Buffer

	// Labelled as the app labels an instance's logger.
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).With("instance", "Lounge", "backend", "kodi")
	require.NoError(t, ping(t.Context(), NewTransport(cfg, log)))

	line := buf.String()
	assert.Contains(t, line, `msg="kodi call" instance=Lounge backend=kodi host=`+cfg.Host, "the instance's name and its address under different keys")
	assert.Equal(t, 1, strings.Count(line, "instance="))
	assert.NotContains(t, line, "error=", "a successful call has no error to show")

	buf.Reset()

	cfg.Password = "wrong"
	require.Error(t, ping(t.Context(), NewTransport(cfg, log)))
	assert.Contains(t, buf.String(), "error=", "a failed call says why")
}
