package kodi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

func (t *Transport) dial(ctx context.Context) (*wsConn, error) {
	ctx, cancel := context.WithTimeout(ctx, t.cfg.Timeout)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, t.cfg.WebSocketURL(), nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	c.SetReadLimit(wsReadLimit)

	return &wsConn{t: t, c: c, pending: map[int64]chan *message{}, done: make(chan struct{})}, nil
}

// Also sends heartbeats, until the connection fails or ctx is done.
func (w *wsConn) serve(ctx context.Context, heartbeat, timeout time.Duration) error {
	go w.read(ctx)

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.close(ctx.Err())
			<-w.done

			return ctx.Err()
		case <-w.done:
			return w.err
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, min(timeout, heartbeat))
			_, err := kodirpc.Call[kodirpc.JSONRPCPingParams, kodirpc.JSONRPCPingResult](pctx, w.callerFor(), kodirpc.MethodJSONRPCPing, kodirpc.JSONRPCPingParams{})
			cancel()

			if err != nil && ctx.Err() == nil {
				w.close(fmt.Errorf("heartbeat: %w", err))
			}
		}
	}
}

// Lets heartbeats bypass the HTTP fallback.
func (w *wsConn) callerFor() kodirpc.Caller { return wsCaller{w} }

func (c wsCaller) Call(ctx context.Context, method string, params, result any) error {
	return c.w.call(ctx, c.w.t.ids.Add(1), method, params, result)
}

func (w *wsConn) read(ctx context.Context) {
	for {
		_, data, err := w.c.Read(ctx)
		if err != nil {
			w.close(err)

			return
		}

		var msg message
		if err := json.Unmarshal(data, &msg); err != nil {
			w.t.log.Debug("kodi websocket: ignoring malformed message", "error", err)

			continue
		}

		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			if msg.Method != "" {
				w.t.notify(Notification{Method: kodirpc.NotificationName(msg.Method), Params: msg.Params})
			}

			continue
		}

		id, err := strconv.ParseInt(string(msg.ID), 10, 64)
		if err != nil {
			continue
		}

		w.mu.Lock()
		ch := w.pending[id]
		delete(w.pending, id)
		w.mu.Unlock()

		if ch != nil {
			ch <- &msg
		}
	}
}

// Idempotent; fails every pending call.
func (w *wsConn) close(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.done:
		return
	default:
	}

	w.err = err
	close(w.done)
	_ = w.c.CloseNow()
}

func (w *wsConn) call(ctx context.Context, id int64, method string, params, result any) error {
	data, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("kodi: %s: encoding params: %w", method, err)
	}

	ch := make(chan *message, 1)

	w.mu.Lock()
	select {
	case <-w.done:
		w.mu.Unlock()

		return errNotSent
	default:
	}

	w.pending[id] = ch
	w.mu.Unlock()

	forget := func() {
		w.mu.Lock()
		delete(w.pending, id)
		w.mu.Unlock()
	}

	if ctx.Err() != nil {
		forget()

		return ctxErr(method, ctx.Err())
	}

	// The websocket library closes the whole connection when a write's context ends, so a caller giving up must not end it; only a stalled write may.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.t.cfg.Timeout)
	err = w.c.Write(wctx, websocket.MessageText, data)

	cancel()

	if err != nil {
		forget()

		if ctx.Err() != nil {
			return ctxErr(method, ctx.Err())
		}

		return fmt.Errorf("%w: %w", errNotSent, err)
	}

	select {
	case msg := <-ch:
		return msg.decode(method, result)
	case <-w.done:
		forget()

		select {
		case msg := <-ch:
			return msg.decode(method, result)
		default:
		}

		if ctx.Err() != nil {
			return ctxErr(method, ctx.Err())
		}

		return unreachable(method, fmt.Errorf("websocket closed: %w", w.err))
	case <-ctx.Done():
		forget()

		return ctxErr(method, ctx.Err())
	}
}
