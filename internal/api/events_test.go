package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
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
		require.LessOrEqual(t, len(hub.ring), ringSize, "the ring never grows past ringSize")
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

func TestHubSkipsValuesItCantEncode(t *testing.T) {
	t.Parallel()

	hub := newHub()
	c, _ := hub.subscribe(0)

	hub.publish("request", make(chan int))
	hub.publish("request", nil)

	ev := <-c.ch
	assert.EqualValues(t, 1, ev.ID, "the unencodable value took no ID")
	assert.Equal(t, "null", string(ev.Data))

	select {
	case extra := <-c.ch:
		t.Fatalf("unexpected event %+v", extra)
	default:
	}
}

func TestHubUnsubscribeIsIdempotent(t *testing.T) {
	t.Parallel()

	hub := newHub()
	c, _ := hub.subscribe(0)

	hub.unsubscribe(c)
	hub.unsubscribe(c) // closing dropped twice would panic

	assert.Zero(t, hub.clientCount())

	hub.publish("request", 1)

	select {
	case ev := <-c.ch:
		t.Fatalf("an unsubscribed client got %+v", ev)
	default:
	}
}

func TestHubReplaysOnlyNewerEvents(t *testing.T) {
	t.Parallel()

	hub := newHub()

	for i := range 3 {
		hub.publish("request", i)
	}

	_, backlog := hub.subscribe(3)
	assert.Empty(t, backlog, "a client that saw the latest event is up to date")

	_, backlog = hub.subscribe(99)
	assert.Empty(t, backlog, "an ID from before a restart replays nothing")

	_, backlog = hub.subscribe(1)
	require.Len(t, backlog, 2)
	assert.EqualValues(t, 2, backlog[0].ID)
	assert.Equal(t, "1", string(backlog[0].Data))
}

// failingWriter accepts the headers but refuses the body after limit bytes, as a closed connection does.
type failingWriter struct {
	*httptest.ResponseRecorder
	limit int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.Body.Len()+len(p) > w.limit {
		return 0, errors.New("connection reset")
	}

	return w.ResponseRecorder.Write(p)
}

// streamAPI is just enough of an API to serve the event stream.
func streamAPI(heartbeat time.Duration) *API {
	return &API{events: newHub(), opts: Options{Heartbeat: heartbeat}}
}

func TestStreamEndsWhenAWriteFails(t *testing.T) {
	t.Parallel()

	retry := len("retry: 3000\n\n")

	for name, tc := range map[string]struct {
		limit     int
		heartbeat time.Duration
		lastID    string
		live      bool
	}{
		"the retry hint": {limit: 0, heartbeat: time.Hour},
		"the backlog":    {limit: retry + 1, heartbeat: time.Hour, lastID: "1"},
		"a live event":   {limit: retry + 1, heartbeat: time.Hour, live: true},
		"the heartbeat":  {limit: retry + 1, heartbeat: time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				a := streamAPI(tc.heartbeat)
				a.events.publish("request", 1)
				a.events.publish("request", 2)

				w := &failingWriter{ResponseRecorder: httptest.NewRecorder(), limit: tc.limit}
				done := make(chan struct{})

				go func() {
					defer close(done)
					a.stream(t.Context(), w, tc.lastID)
				}()

				synctest.Wait()

				if tc.live {
					require.Equal(t, 1, a.events.clientCount())
					a.events.publish("request", 3)
				}

				// The heartbeat is the only write that needs time to pass.
				time.Sleep(tc.heartbeat)
				synctest.Wait()

				select {
				case <-done:
				default:
					t.Fatal("the stream kept running after a write failed")
				}

				assert.Zero(t, a.events.clientCount(), "and unsubscribed")
			})
		})
	}
}

func TestStreamWritesBacklogAndEvents(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		a := streamAPI(time.Hour)
		a.events.publish("request", map[string]string{"heard": "dune"})
		a.events.publish("request", map[string]string{"heard": "arrival"})

		ctx, cancel := context.WithCancel(t.Context())
		w := httptest.NewRecorder()
		done := make(chan struct{})

		go func() {
			defer close(done)
			a.stream(ctx, w, "1")
		}()

		synctest.Wait()
		require.Equal(t, 1, a.events.clientCount())
		a.events.publish("deploy", map[string]string{"step": "package"})
		synctest.Wait()

		cancel()
		<-done

		assert.Equal(t, "retry: 3000\n\n"+
			"id: 2\nevent: request\ndata: {\"heard\":\"arrival\"}\n\n"+
			"id: 3\nevent: deploy\ndata: {\"step\":\"package\"}\n\n", w.Body.String())
		assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		assert.Zero(t, a.events.clientCount())
	})
}

func TestStreamIgnoresAMalformedLastEventID(t *testing.T) {
	t.Parallel()

	a := streamAPI(time.Hour)
	a.events.publish("request", 1)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	w := httptest.NewRecorder()
	a.stream(ctx, w, "not-a-number")

	assert.Equal(t, "retry: 3000\n\n", w.Body.String(), "nothing is replayed")
}

func TestStreamEndsWhenTheClientIsDropped(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		a := streamAPI(time.Hour)
		done := make(chan struct{})

		go func() {
			defer close(done)
			a.stream(t.Context(), httptest.NewRecorder(), "")
		}()

		synctest.Wait()
		require.Equal(t, 1, a.events.clientCount())

		// A client too slow for clientBuffer events is dropped by the hub.
		a.events.mu.Lock()
		for c := range a.events.clients {
			delete(a.events.clients, c)
			close(c.dropped)
		}
		a.events.mu.Unlock()

		synctest.Wait()

		select {
		case <-done:
		default:
			t.Fatal("a dropped client's stream kept running")
		}
	})
}

func TestEventsNeedASignIn(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	assert.Equal(t, http.StatusUnauthorized, h.client().do(http.MethodGet, "/api/v1/events", nil).status)
}
