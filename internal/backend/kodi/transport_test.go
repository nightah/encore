package kodi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// newFake starts a fake Kodi on an in-memory network of its own, so a test can run in a synctest bubble.
func newFake(t *testing.T, opts fakekodi.Options) *fakekodi.Server {
	t.Helper()

	return fakekodi.NewNetwork(t).New(opts)
}

func testConfig(t *testing.T, s *fakekodi.Server) Config {
	t.Helper()

	cfg, err := ConfigFromURLs(s.URL(), s.WebSocketURL())
	require.NoError(t, err)

	cfg.Timeout = 2 * time.Second
	cfg.HeartbeatInterval = time.Hour
	cfg.ReconnectMin = 10 * time.Millisecond
	cfg.ReconnectMax = 100 * time.Millisecond
	cfg.HTTPClient = s.Client()

	return cfg
}

// runTransport runs tr until the test ends. In a synctest bubble it returns once the WebSocket is connected.
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

	synctest.Wait()
	require.True(t, tr.Connected(), "websocket never connected")
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

// stateLog counts the connection changes a transport reports.
type stateLog struct{ ups, downs atomic.Int32 }

func (l *stateLog) listen(tr *Transport) {
	tr.Listen(Listener{State: func(up bool) {
		if up {
			l.ups.Add(1)
		} else {
			l.downs.Add(1)
		}
	}})
}

// dialLog wraps a client to record when each WebSocket handshake is attempted, refusing the first refuse of them.
type dialLog struct {
	next   http.RoundTripper
	refuse int

	mu    sync.Mutex
	dials []time.Time
}

func (d *dialLog) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Upgrade") != "" {
		d.mu.Lock()
		d.dials = append(d.dials, time.Now())
		n := len(d.dials)
		d.mu.Unlock()

		if n <= d.refuse {
			return nil, errors.New("connection refused")
		}
	}

	return d.next.RoundTrip(r)
}

func (d *dialLog) times() []time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]time.Time(nil), d.dials...)
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
	s := newFake(t, fakekodi.Options{Username: "kodi", Password: "secret"})
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
	synctest.Test(t, func(t *testing.T) {
		s := newFake(t, fakekodi.Options{})
		s.Handle("Test.Echo", func(params json.RawMessage) (any, *fakekodi.Error) {
			// A random delay lets responses overtake each other.
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

		calls := s.CallsTo("Test.Echo")
		assert.Len(t, calls, n)

		for _, c := range calls {
			assert.True(t, c.WebSocket, "calls go over the websocket while it is up")
		}
	})
}

func TestNotifications(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newFake(t, fakekodi.Options{})
		tr := NewTransport(testConfig(t, s), nil)

		got := make(chan Notification, 4)
		stop := tr.Listen(Listener{Notification: func(n Notification) { got <- n }})

		runTransport(t, tr)

		s.Notify("VideoLibrary.OnUpdate", map[string]any{"item": map[string]any{"id": 7, "type": "movie"}})
		synctest.Wait()

		require.Len(t, got, 1, "the listener hears the notification")

		n := <-got
		assert.Equal(t, kodirpc.NotificationVideoLibraryOnUpdate, n.Method)
		assert.JSONEq(t, `{"sender":"xbmc","data":{"item":{"id":7,"type":"movie"}}}`, string(n.Params))

		stop()
		s.Notify("VideoLibrary.OnUpdate", map[string]any{})
		synctest.Wait()
		assert.Empty(t, got, "an unregistered listener gets nothing")
	})
}

func TestReconnectAfterRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newFake(t, fakekodi.Options{})
		cfg := testConfig(t, s)
		tr := NewTransport(cfg, nil)

		var states stateLog

		states.listen(tr)
		runTransport(t, tr)
		s.Restart(t)
		synctest.Wait()

		assert.False(t, tr.Connected(), "the restart drops the WebSocket")
		assert.Equal(t, int32(1), states.downs.Load())

		time.Sleep(cfg.ReconnectMin)
		synctest.Wait()

		require.True(t, tr.Connected(), "the transport reconnects within the first backoff")
		assert.Equal(t, int32(2), states.ups.Load())

		before := len(s.Calls())
		require.NoError(t, ping(t.Context(), tr))
		assert.True(t, s.Calls()[before].WebSocket)
	})
}

func TestReconnectBackoff(t *testing.T) {
	const minWait, maxWait = 100 * time.Millisecond, 800 * time.Millisecond

	// backoff is the ceiling of the wait before the nth retry: doubling from the minimum, capped at the maximum.
	backoff := func(n int) time.Duration { return min(minWait<<n, maxWait) }

	setup := func(t *testing.T, refuse int) (*fakekodi.Server, *Transport, *dialLog) {
		t.Helper()

		s := newFake(t, fakekodi.Options{})
		cfg := testConfig(t, s)
		cfg.ReconnectMin, cfg.ReconnectMax = minWait, maxWait
		dials := &dialLog{next: s.Client().Transport, refuse: refuse}
		cfg.HTTPClient = &http.Client{Transport: dials}

		return s, NewTransport(cfg, nil), dials
	}

	// gaps returns the time from each dial to the next.
	gaps := func(times []time.Time) []time.Duration {
		out := make([]time.Duration, 0, len(times))
		for i := 1; i < len(times); i++ {
			out = append(out, times[i].Sub(times[i-1]))
		}

		return out
	}

	t.Run("a refused WebSocket is retried after a doubling, capped wait", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			_, tr, dials := setup(t, math.MaxInt)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})

			go func() { defer close(done); _ = tr.Run(ctx) }()

			time.Sleep(10 * time.Second)
			synctest.Wait()
			assert.False(t, tr.Connected())

			got := gaps(dials.times())
			require.GreaterOrEqual(t, len(got), 10, "a refused WebSocket keeps being retried")

			for i, g := range got {
				// Equal jitter: between half and all of the backoff.
				assert.GreaterOrEqual(t, g, backoff(i)/2, "retry %d", i+1)
				assert.LessOrEqual(t, g, backoff(i), "retry %d", i+1)
			}

			cancel()
			<-done

			n := len(dials.times())

			time.Sleep(time.Minute)
			assert.Len(t, dials.times(), n, "a stopped transport stops retrying")
		})
	})

	t.Run("only a connection that lasted resets the backoff", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// Five refusals raise the backoff to its maximum.
			const refused = 5

			s, tr, dials := setup(t, refused)

			// The first connection drops as soon as it's up.
			var ups atomic.Int32

			tr.Listen(Listener{State: func(up bool) {
				if up && ups.Add(1) == 1 {
					s.DropConnections()
				}
			}})

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})

			go func() { defer close(done); _ = tr.Run(ctx) }()

			time.Sleep(10 * time.Second)
			synctest.Wait()
			require.True(t, tr.Connected())

			times := dials.times()
			require.Len(t, times, refused+2, "the refused dials, the flapping connection and the one that stayed")

			flap := times[refused+1].Sub(times[refused])
			assert.GreaterOrEqual(t, flap, maxWait/2, "a connection that dropped at once doesn't reset the backoff")
			assert.LessOrEqual(t, flap, maxWait)

			// Up for longer than the maximum backoff: the next drop is retried within the minimum.
			dropped := time.Now()
			s.DropConnections()
			time.Sleep(minWait)
			synctest.Wait()

			require.True(t, tr.Connected())

			times = dials.times()
			require.Len(t, times, refused+3)
			assert.GreaterOrEqual(t, times[refused+2].Sub(dropped), minWait/2)
			assert.LessOrEqual(t, times[refused+2].Sub(dropped), minWait)

			cancel()
			<-done
		})
	})
}

func TestHTTPFallbackWhileWebSocketDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newFake(t, fakekodi.Options{})
		cfg := testConfig(t, s)
		tr := NewTransport(cfg, nil)
		runTransport(t, tr)

		s.SetWebSocket(false)
		synctest.Wait()
		require.False(t, tr.Connected())

		before := len(s.Calls())
		require.NoError(t, ping(t.Context(), tr))
		assert.False(t, s.Calls()[before].WebSocket, "falls back to HTTP")

		s.SetWebSocket(true)
		time.Sleep(cfg.ReconnectMax)
		synctest.Wait()
		assert.True(t, tr.Connected(), "reconnects once the websocket is back")
	})
}

func TestHeartbeatDropsDeadConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const heartbeat, timeout = 50 * time.Millisecond, 100 * time.Millisecond

		s := newFake(t, fakekodi.Options{})

		var hang atomic.Bool

		release := make(chan struct{})
		defer close(release)

		s.Handle(kodirpc.MethodJSONRPCPing, func(json.RawMessage) (any, *fakekodi.Error) {
			if hang.Load() {
				<-release
			}

			return "pong", nil
		})

		cfg := testConfig(t, s)
		cfg.HeartbeatInterval, cfg.Timeout = heartbeat, timeout
		tr := NewTransport(cfg, nil)

		var states stateLog

		states.listen(tr)
		runTransport(t, tr)

		time.Sleep(2 * heartbeat)
		synctest.Wait()
		assert.Len(t, s.CallsTo(kodirpc.MethodJSONRPCPing), 2, "an idle connection is pinged every interval")
		assert.Zero(t, states.downs.Load(), "healthy heartbeats keep the connection")

		// The next heartbeat goes unanswered; it gives up after the shorter of the timeout and the interval.
		hang.Store(true)
		time.Sleep(heartbeat + heartbeat - time.Millisecond)
		synctest.Wait()
		assert.Zero(t, states.downs.Load(), "a heartbeat gets its time to answer")

		time.Sleep(time.Millisecond)
		synctest.Wait()
		assert.Equal(t, int32(1), states.downs.Load(), "an unanswered heartbeat drops the socket")
		assert.False(t, tr.Connected())
	})
}

func TestRunIsShared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newFake(t, fakekodi.Options{})
		tr := NewTransport(testConfig(t, s), nil)

		ctx1, cancel1 := context.WithCancel(t.Context())
		ctx2, cancel2 := context.WithCancel(t.Context())

		done1, done2 := make(chan struct{}), make(chan struct{})

		go func() { _ = tr.Run(ctx1); close(done1) }()
		go func() { _ = tr.Run(ctx2); close(done2) }()

		synctest.Wait()
		require.True(t, tr.Connected())
		require.Equal(t, 1, s.WebSocketClients(), "concurrent Runs share one connection")

		cancel1()
		<-done1
		assert.True(t, tr.Connected(), "the connection stays while another Run is active")

		cancel2()
		<-done2
		assert.False(t, tr.Connected())
	})
}

func TestRunRestartKeepsNewConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newFake(t, fakekodi.Options{})
		tr := NewTransport(testConfig(t, s), nil)

		// The first connection's loop is held right after it connects, so it tears down only after the next Run has started.
		held, release := make(chan struct{}), make(chan struct{})

		var ups atomic.Int32

		tr.Listen(Listener{State: func(up bool) {
			if up && ups.Add(1) == 1 {
				close(held)
				<-release
			}
		}})

		ctx1, cancel1 := context.WithCancel(t.Context())
		done1 := make(chan struct{})

		go func() { _ = tr.Run(ctx1); close(done1) }()

		<-held
		cancel1()

		// The first Run has let go and waits for its connection's teardown.
		synctest.Wait()

		tr.mu.Lock()
		require.Zero(t, tr.runs)
		tr.mu.Unlock()

		ctx2, cancel2 := context.WithCancel(t.Context())
		done2 := make(chan struct{})

		t.Cleanup(func() { cancel2(); <-done2 })

		go func() { _ = tr.Run(ctx2); close(done2) }()

		synctest.Wait()
		assert.Equal(t, int32(1), ups.Load(), "the next Run waits for the old connection's teardown")

		close(release)
		<-done1
		synctest.Wait()

		assert.Equal(t, int32(2), ups.Load())
		assert.True(t, tr.Connected(), "the old connection's teardown must not clear the new one")
	})
}

func TestErrorMapping(t *testing.T) {
	versions := map[string][3]int{"omega": {13, 5, 0}, "piers": {13, 1000, 0}}

	for name, v := range versions {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newFake(t, fakekodi.Options{Version: v})
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
		})
	}

	s := newFake(t, fakekodi.Options{})
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
	for _, ws := range []bool{false, true} {
		t.Run(fmt.Sprintf("websocket=%v", ws), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const timeout = 50 * time.Millisecond

				s := newFake(t, fakekodi.Options{})
				s.Handle("Test.Slow", func(json.RawMessage) (any, *fakekodi.Error) {
					time.Sleep(4 * timeout)

					return "late", nil
				})

				cfg := testConfig(t, s)
				cfg.Timeout = timeout
				cfg.DisableWebSocket = !ws
				tr := NewTransport(cfg, nil)

				if ws {
					runTransport(t, tr)
				}

				start := time.Now()
				err := tr.Call(t.Context(), "Test.Slow", nil, nil)
				require.ErrorIs(t, err, media.ErrUnreachable)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Equal(t, timeout, time.Since(start), "a call without a deadline gets the configured timeout")

				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				err = tr.Call(ctx, "Test.Slow", nil, nil)
				require.ErrorIs(t, err, context.Canceled)
				require.NotErrorIs(t, err, media.ErrUnreachable, "a cancelled caller is not an unreachable Kodi")

				// Let the slow handler finish before the bubble ends.
				time.Sleep(4 * timeout)
			})
		})
	}
}

func TestCancelledCallKeepsWebSocket(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newFake(t, fakekodi.Options{})
		tr := NewTransport(testConfig(t, s), nil)
		runTransport(t, tr)

		ws := tr.conn()
		require.NotNil(t, ws)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// The websocket library picks between a done context and its write lock at random, so repeat until the write path is all but certain to have been taken.
		for range 100 {
			err := tr.Call(ctx, kodirpc.MethodJSONRPCPing, nil, nil)
			require.ErrorIs(t, err, context.Canceled)
			require.NotErrorIs(t, err, media.ErrUnreachable, "a cancelled caller is not an unreachable Kodi")
		}

		synctest.Wait()

		select {
		case <-ws.done:
			t.Fatalf("a cancelled caller closed the shared websocket: %v", ws.err)
		default:
		}

		assert.Same(t, ws, tr.conn())
		require.NoError(t, ping(t.Context(), tr))
	})
}

func TestClientVersionGate(t *testing.T) {
	omega := newFake(t, fakekodi.Options{})
	c := NewClient(testConfig(t, omega), nil)

	v, err := c.APIVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, kodirpc.APIVersion{Major: 13, Minor: 5}, v)

	_, err = c.PlayerGetChapters(t.Context(), kodirpc.PlayerGetChaptersParams{PlayerID: 1})
	require.ErrorIs(t, err, ErrUnsupported)
	assert.Empty(t, omega.CallsTo(kodirpc.MethodPlayerGetChapters), "unsupported methods are not sent")
	assert.Len(t, omega.CallsTo(kodirpc.MethodJSONRPCVersion), 1, "the version is cached")

	piers := newFake(t, fakekodi.Options{Version: [3]int{13, 1000, 0}})
	c = NewClient(testConfig(t, piers), nil)

	_, err = c.PlayerGetChapters(t.Context(), kodirpc.PlayerGetChaptersParams{PlayerID: 1})
	require.NotErrorIs(t, err, ErrUnsupported)
	assert.Len(t, piers.CallsTo(kodirpc.MethodPlayerGetChapters), 1)
}

func TestCallLogging(t *testing.T) {
	s := newFake(t, fakekodi.Options{Username: "kodi", Password: "secret"})
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
