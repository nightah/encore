package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/store"
)

type sse struct {
	id    uint64
	event string
	data  string
}

// stream delivers comment lines as events named "comment".
func (c *client) stream(ctx context.Context, lastID string) (<-chan sse, *http.Response) {
	c.h.t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.h.server.URL+"/api/v1/events", http.NoBody)
	require.NoError(c.h.t, err)

	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}

	resp, err := c.http.Do(req)
	require.NoError(c.h.t, err)
	require.Equal(c.h.t, http.StatusOK, resp.StatusCode)

	out := make(chan sse, 64)

	go func() {
		defer close(out)
		defer resp.Body.Close()

		sc := bufio.NewScanner(resp.Body)

		var ev sse

		for sc.Scan() {
			line := sc.Text()

			switch {
			case strings.HasPrefix(line, ":"):
				out <- sse{event: "comment", data: line}
			case strings.HasPrefix(line, "id: "):
				ev.id, _ = strconv.ParseUint(line[4:], 10, 64)
			case strings.HasPrefix(line, "event: "):
				ev.event = line[7:]
			case strings.HasPrefix(line, "data: "):
				ev.data = line[6:]
			case line == "" && ev.event != "":
				out <- ev
				ev = sse{}
			}
		}
	}()

	return out, resp
}

func next(t *testing.T, ch <-chan sse, event string) sse {
	t.Helper()

	timeout := time.After(10 * time.Second)

	for {
		select {
		case ev, ok := <-ch:
			require.True(t, ok, "stream closed while waiting for %s", event)

			if ev.event == event {
				return ev
			}
		case <-timeout:
			t.Fatalf("no %s event", event)
		}
	}
}

func TestEventStream(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	h.loungeID()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, resp := c.stream(ctx, "")
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

	next(t, ch, "comment") // the heartbeat arrives on an idle stream

	require.NoError(t, h.app.DB().RequestLog().Insert(t.Context(), &store.RequestLogEntry{DeviceID: "amzn1.ask.device.X", Intent: "WatchMovie", Outcome: "played", Slots: json.RawMessage(`{"Movie":"hot fuzz"}`)}))

	ev := next(t, ch, "request")

	var rs RequestSummary

	require.NoError(t, json.Unmarshal([]byte(ev.data), &rs))
	assert.Equal(t, "hot fuzz", rs.Heard)

	// Resuming after an earlier event replays what came since.
	h.api.events.publish("attention", []Attention{{Kind: "test"}})
	cancel()

	resumed, resumedResp := c.stream(t.Context(), strconv.FormatUint(ev.id, 10))
	defer resumedResp.Body.Close()

	replayed := next(t, resumed, "attention")
	assert.Greater(t, replayed.id, ev.id)
	assert.Contains(t, replayed.data, `"kind":"test"`)
}

func TestSlowClientIsDropped(t *testing.T) {
	hub := newHub()

	slow, _ := hub.subscribe(0)
	fast, _ := hub.subscribe(0)

	for i := range clientBuffer + 1 {
		hub.publish("request", i)

		<-fast.ch // a reader keeps up
	}

	select {
	case <-slow.dropped:
	default:
		t.Fatal("a client that stops reading is dropped")
	}

	select {
	case <-fast.dropped:
		t.Fatal("a client that keeps up stays")
	default:
	}

	assert.Equal(t, 1, hub.clientCount())
}

func TestRingKeepsRecentEvents(t *testing.T) {
	hub := newHub()

	for i := range ringSize + 10 {
		hub.publish("request", i)
	}

	c, backlog := hub.subscribe(5)
	defer hub.unsubscribe(c)

	require.Len(t, backlog, ringSize, "only the most recent events are kept")
	assert.EqualValues(t, 11, backlog[0].ID)

	_, none := hub.subscribe(0)
	assert.Empty(t, none, "no Last-Event-ID, no replay")
}

func TestPlayerChangeRefreshesStatus(t *testing.T) {
	// Polling every hour: a quick update can only come from Kodi's notification.
	h := newHarness(t, harnessOpts{mutate: func(o *Options) { o.StatusInterval = time.Hour }})
	c := h.admin()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	events, stream := c.stream(ctx, "")
	defer stream.Body.Close()

	next(t, events, "comment")

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"Player.Open","params":{"item":{"movieid":1}}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.kodi.URL(), body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	deadline := time.After(3 * time.Second)

	for {
		select {
		case ev := <-events:
			if ev.event == "instance" && strings.Contains(ev.data, `"now_playing":{`) {
				return
			}
		case <-deadline:
			t.Fatal("the instance's status didn't update after Kodi reported playback")
		}
	}
}

func TestLogsNameInstanceAndBackend(t *testing.T) {
	h := newHarness(t, harnessOpts{})

	var line string

	require.Eventually(t, func() bool {
		for l := range strings.SplitSeq(h.logs.String(), "\n") {
			if strings.Contains(l, `msg="kodi call"`) {
				line = l

				return true
			}
		}

		return false
	}, 10*time.Second, 50*time.Millisecond)

	assert.Contains(t, line, "instance=Lounge backend=kodi")
	assert.Contains(t, line, "host=")
	assert.NotContains(t, h.logs.String(), " kodi=", "the instance is instance=, whatever kind of media server it is")
	assert.NotContains(t, h.logs.String(), `\"`, "messages quote with “”, which log lines don't escape")
}
