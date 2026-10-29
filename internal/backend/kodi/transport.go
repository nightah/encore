package kodi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// NewTransport returns a Transport for cfg. Nothing is dialled until the first call or Run.
func NewTransport(cfg Config, log *slog.Logger) *Transport {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	cfg = cfg.withDefaults()

	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}

	return &Transport{
		cfg: cfg,
		// host, not kodi: the instance's logger already names it as kodi.
		log:       log.With("host", cfg.Host),
		http:      hc,
		listeners: map[int]Listener{},
	}
}

// Call implements kodirpc.Caller over the WebSocket when connected, HTTP otherwise.
func (t *Transport) Call(ctx context.Context, method string, params, result any) error {
	return t.call(ctx, method, params, result, false)
}

// PingHTTP sends JSONRPC.Ping over HTTP even while the WebSocket is up. Kodi authenticates only HTTP, so this is how the credentials are checked; the answer is also recorded for HTTPAuth.
func (t *Transport) PingHTTP(ctx context.Context) error {
	var res kodirpc.JSONRPCPingResult

	return t.call(ctx, kodirpc.MethodJSONRPCPing, kodirpc.JSONRPCPingParams{}, &res, true)
}

// HTTPAuth reports what Kodi's web server made of the credentials on the last HTTP request it answered, a JSON-RPC call or an image.
func (t *Transport) HTTPAuth() HTTPAuth { return HTTPAuth(t.auth.Load()) }

func (t *Transport) noteAuth(status int) {
	switch status {
	case http.StatusOK:
		t.auth.Store(int32(HTTPAuthAccepted))
	case http.StatusUnauthorized:
		t.auth.Store(int32(HTTPAuthRefused))
	}
}

func (t *Transport) call(ctx context.Context, method string, params, result any, httpOnly bool) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, t.cfg.Timeout)
		defer cancel()
	}

	id := t.ids.Add(1)
	start := time.Now()
	via := "http"

	var err error

	if ws := t.conn(); ws != nil && !httpOnly {
		via = "ws"

		err = ws.call(ctx, id, method, params, result)
		if errors.Is(err, errNotSent) {
			via = "http"
			err = t.httpCall(ctx, id, method, params, result)
		}
	} else {
		err = t.httpCall(ctx, id, method, params, result)
	}

	latency := time.Since(start)

	attrs := []slog.Attr{slog.String("method", method), slog.String("via", via), slog.Duration("latency", latency)}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}

	t.log.LogAttrs(ctx, slog.LevelDebug, "kodi call", attrs...)

	media.ReportCall(ctx, media.Call{Method: method, Duration: latency, Err: err})

	return err
}

// Connected reports whether the WebSocket is up.
func (t *Transport) Connected() bool { return t.conn() != nil }

func (t *Transport) conn() *wsConn {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.ws
}

// Listen registers l and returns a func that unregisters it.
func (t *Transport) Listen(l Listener) (cancel func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nextLis++
	id := t.nextLis
	t.listeners[id] = l

	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		delete(t.listeners, id)
	}
}

func (t *Transport) snapshotListeners() []Listener {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Listener, 0, len(t.listeners))
	for _, l := range t.listeners {
		out = append(out, l)
	}

	return out
}

func (t *Transport) notify(n Notification) {
	for _, l := range t.snapshotListeners() {
		if l.Notification != nil {
			l.Notification(n)
		}
	}
}

func (t *Transport) setConn(ws *wsConn) {
	t.mu.Lock()
	t.ws = ws
	t.mu.Unlock()

	for _, l := range t.snapshotListeners() {
		if l.State != nil {
			l.State(ws != nil)
		}
	}
}

// Run keeps the WebSocket connected, reconnecting with backoff, until ctx is done. Concurrent Runs share one connection, which closes when the last returns.
func (t *Transport) Run(ctx context.Context) error {
	if t.cfg.DisableWebSocket {
		<-ctx.Done()

		return nil
	}

	t.acquire()
	defer t.release()

	<-ctx.Done()

	return nil
}

func (t *Transport) acquire() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.runs++
	if t.runs > 1 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	prev := t.done
	t.stop, t.done = cancel, make(chan struct{})

	go t.loop(ctx, prev, t.done)
}

func (t *Transport) release() {
	t.mu.Lock()

	t.runs--
	if t.runs > 0 {
		t.mu.Unlock()

		return
	}

	stop, done := t.stop, t.done
	t.mu.Unlock()

	stop()
	<-done
}

// prev is the previous loop's done, nil for the first; waiting for it stops its teardown clearing this loop's connection.
func (t *Transport) loop(ctx context.Context, prev <-chan struct{}, done chan struct{}) {
	defer close(done)

	if prev != nil {
		<-prev
	}

	backoff := t.cfg.ReconnectMin

	for {
		ws, err := t.dial(ctx)
		if err == nil {
			up := time.Now()

			t.log.Info("kodi websocket connected", "url", t.cfg.WebSocketURL())
			t.setConn(ws)
			err = ws.serve(ctx, t.cfg.HeartbeatInterval, t.cfg.Timeout)
			t.setConn(nil)

			if ctx.Err() == nil {
				t.log.Warn("kodi websocket disconnected", "error", err)
			}

			if time.Since(up) > t.cfg.ReconnectMax {
				backoff = t.cfg.ReconnectMin
			}
		} else if ctx.Err() == nil {
			t.log.Debug("kodi websocket dial failed", "error", err, "retry_in", backoff)
		}

		if ctx.Err() != nil {
			return
		}

		// Equal jitter: wait between half and all of the current backoff.
		wait := backoff/2 + rand.N(backoff/2+1) //nolint:gosec // G404: jitter needs no cryptographic randomness

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		backoff = min(backoff*2, t.cfg.ReconnectMax)
	}
}

func (m *message) decode(method string, result any) error {
	if m.Error != nil {
		m.Error.Method = method

		return m.Error
	}

	if result == nil || len(m.Result) == 0 {
		return nil
	}

	if err := json.Unmarshal(m.Result, result); err != nil {
		return fmt.Errorf("kodi: %s: decoding result: %w", method, err)
	}

	return nil
}

// A timeout counts as Kodi being unreachable.
func ctxErr(method string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return unreachable(method, err)
	}

	return fmt.Errorf("kodi: %s: %w", method, err)
}
