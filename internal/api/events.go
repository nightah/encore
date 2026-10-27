package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func newHub() *hub {
	return &hub{clients: map[*streamClient]struct{}{}}
}

func (h *hub) publish(name string, v any) {
	b, err := json.Marshal(fillEmpty(v))
	if err != nil {
		return
	}

	h.publishRaw(name, b)
}

func (h *hub) publishRaw(name string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	ev := event{ID: h.nextID, Name: name, Data: data}

	h.ring = append(h.ring, ev)
	if len(h.ring) > ringSize {
		h.ring = h.ring[len(h.ring)-ringSize:]
	}

	for c := range h.clients {
		select {
		case c.ch <- ev:
		default:
			// Too slow: drop it rather than block everyone else or buffer without bound.
			delete(h.clients, c)
			close(c.dropped)
		}
	}
}

func (h *hub) subscribe(lastID uint64) (*streamClient, []event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	c := &streamClient{ch: make(chan event, clientBuffer), dropped: make(chan struct{})}
	h.clients[c] = struct{}{}

	var backlog []event

	if lastID > 0 {
		for _, ev := range h.ring {
			if ev.ID > lastID {
				backlog = append(backlog, ev)
			}
		}
	}

	return c, backlog
}

func (h *hub) unsubscribe(c *streamClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.dropped)
	}
}

func (h *hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.clients)
}

func (a *API) registerEvents(api huma.API) {
	op := protected(huma.Operation{
		OperationID: "streamEvents",
		Method:      http.MethodGet,
		Path:        Prefix + "/events",
		Tags:        []string{"events"},
		Summary:     "Live updates as Server-Sent Events",
		Description: "Each event has a monotonic id (resume with the Last-Event-ID header), an event name (request, instance, library, device, deploy, attention) and a JSON data payload. A comment heartbeat is sent every 15 seconds. Slow clients are disconnected and should reconnect.",
		Responses: map[string]*huma.Response{
			"200": {Description: "An event stream", Content: map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString}}}},
		},
	})

	huma.Register(api, op, func(_ context.Context, in *struct {
		LastEventID string `header:"Last-Event-ID"`
	},
	) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			r, w := humago.Unwrap(hctx)
			a.stream(r.Context(), w, in.LastEventID)
		}}, nil
	})
}

func (a *API) stream(ctx context.Context, w http.ResponseWriter, lastEventID string) {
	rc := http.NewResponseController(w)

	// The server's timeouts suit ordinary requests; this one stays open.
	_ = rc.SetWriteDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	last, _ := strconv.ParseUint(lastEventID, 10, 64)

	client, backlog := a.events.subscribe(last)
	defer a.events.unsubscribe(client)

	write := func(ev event) bool {
		_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Name, ev.Data)

		return err == nil && rc.Flush() == nil
	}

	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil || rc.Flush() != nil {
		return
	}

	for _, ev := range backlog {
		if !write(ev) {
			return
		}
	}

	heartbeat := time.NewTicker(a.opts.Heartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-client.dropped:
			return
		case ev := <-client.ch:
			if !write(ev) {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
