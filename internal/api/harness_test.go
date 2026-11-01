package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

const testPassword = "correct horse battery"

// syncBuffer is a log sink safe for concurrent writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

type harness struct {
	t      *testing.T
	api    *API
	app    *app.App
	kodi   *fakekodi.Server
	smapi  *smapitest.Server
	server *httptest.Server
	logs   *syncBuffer
	cfg    *config.Config
	// skew moves the API's clock forward, in nanoseconds.
	skew atomic.Int64
}

type harnessOpts struct {
	// env configures the server as ENCORE_* variables would, so keys become managed.
	env      []string
	noKodi   bool
	discover func(ctx context.Context) ([]kodi.Instance, error)
	mutate   func(*Options)
}

// embeddedSpeech is the embedded sources' Speech, built once and shared by every harness: NewSpeech expands every sample of every locale.
var embeddedSpeech = sync.OnceValues(func() (*alexa.Speech, error) {
	src, err := loadSources()
	if err != nil {
		return nil, err
	}

	return alexa.NewSpeech(src, slog.New(slog.DiscardHandler))
})

func newHarness(t *testing.T, o harnessOpts) *harness {
	t.Helper()

	h := &harness{t: t, logs: &syncBuffer{}}
	h.kodi = fakekodi.New(t, fakekodi.Options{})
	h.smapi = smapitest.New(t, smapitest.Options{Pending: 1})

	env := append([]string{"ENCORE_DATA_DIR=" + t.TempDir()}, o.env...)

	cfg, err := config.Load(config.Options{Environ: func() []string { return env }})
	require.NoError(t, err)

	if !o.noKodi {
		cfg.Kodi = []config.KodiInstance{{Name: "Lounge", URL: h.kodi.URL(), WebSocketURL: h.kodi.WebSocketURL(), Default: true}}
	}

	h.cfg = cfg
	log := slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	a, err := app.New(t.Context(), cfg, log)
	require.NoError(t, err)

	h.app = a

	src, err := loadSources()
	require.NoError(t, err)

	speech, err := embeddedSpeech()
	require.NoError(t, err)

	opts := Options{
		App:      a,
		Sources:  src,
		Speech:   speech,
		Actions:  actions.NewService(log),
		Logger:   log,
		Now:      func() time.Time { return time.Now().Add(time.Duration(h.skew.Load())) },
		Discover: o.discover,
		// Discovered instances report Kodi 21.3; the real probe is tested in package kodi.
		ProbeVersion:   func(context.Context, string, int) (string, error) { return "21.3", nil },
		Heartbeat:      100 * time.Millisecond,
		StatusInterval: 200 * time.Millisecond,
		LWAEndpoint:    h.smapi.Endpoint(),
		SMAPI: smapi.Options{
			BaseURL: h.smapi.URL,
			Retry:   smapi.RetryPolicy{Base: time.Millisecond, Max: 2 * time.Millisecond, MaxRetryAfter: time.Millisecond},
			Poll:    smapi.PollConfig{Interval: time.Millisecond, MaxInterval: 2 * time.Millisecond, Timeout: 10 * time.Second},
		},
	}

	if o.mutate != nil {
		o.mutate(&opts)
	}

	ctx, cancel := context.WithCancel(t.Context())

	h.api, err = New(ctx, opts)
	require.NoError(t, err)

	var wg sync.WaitGroup

	wg.Go(func() { _ = a.Run(ctx) })
	wg.Go(func() { h.api.Run(ctx) })

	h.server = httptest.NewServer(h.api.Handler())

	t.Cleanup(func() {
		h.server.CloseClientConnections()
		h.server.Close()
		cancel()
		wg.Wait()
		assert.NoError(t, a.Close())
	})

	return h
}

// setupToken reads the one-time token from the log, as an operator would.
func (h *harness) setupToken() string {
	m := regexp.MustCompile(`setup_token=(\S+)`).FindStringSubmatch(h.logs.String())
	require.Len(h.t, m, 2, "the setup token is logged")

	return m[1]
}

// client is a browser: a cookie jar and the CSRF token from /auth/state.
type client struct {
	h    *harness
	http *http.Client
	csrf string
}

func (h *harness) client() *client {
	jar, err := cookiejar.New(nil)
	require.NoError(h.t, err)

	c := &client{h: h, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.refreshCSRF()

	return c
}

func (c *client) refreshCSRF() {
	var st AuthState

	c.json(http.MethodGet, "/api/v1/auth/state", nil, http.StatusOK, &st)
	c.csrf = st.CSRFToken
}

// admin creates the admin account on first use.
func (h *harness) admin() *client {
	c := h.client()

	var st AuthState

	c.json(http.MethodGet, "/api/v1/auth/state", nil, http.StatusOK, &st)

	if st.SetupRequired {
		c.json(http.MethodPost, "/api/v1/setup/admin", map[string]string{"setup_token": h.setupToken(), "username": "admin", "password": testPassword}, http.StatusNoContent, nil)
	} else {
		c.json(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}, http.StatusNoContent, nil)
	}

	return c
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (c *client) do(method, path string, body any, hdr ...string) response {
	c.h.t.Helper()

	var r io.Reader = http.NoBody

	switch b := body.(type) {
	case nil:
	case []byte:
		r = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		require.NoError(c.h.t, err)

		r = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(c.h.t.Context(), method, c.h.server.URL+path, r)
	require.NoError(c.h.t, err)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}

	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}

	resp, err := c.http.Do(req)
	require.NoError(c.h.t, err)

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	require.NoError(c.h.t, err)

	return response{status: resp.StatusCode, header: resp.Header, body: b}
}

func (c *client) json(method, path string, body any, status int, out any, hdr ...string) response {
	c.h.t.Helper()

	resp := c.do(method, path, body, hdr...)
	require.Equal(c.h.t, status, resp.status, "%s %s: %s", method, path, resp.body)

	if out != nil {
		require.NoError(c.h.t, json.Unmarshal(resp.body, out), string(resp.body))
	}

	return resp
}

// problem is an RFC 9457 problem document.
type problem struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Errors []struct {
		Location string `json:"location"`
		Message  string `json:"message"`
	} `json:"errors"`
}

func (r response) problem(t *testing.T) problem {
	t.Helper()

	assert.Equal(t, "application/problem+json", r.header.Get("Content-Type"))

	var p problem
	require.NoError(t, json.Unmarshal(r.body, &p), string(r.body))

	return p
}

// handlerRequest calls the handler directly with a chosen peer address, for trusted-proxy tests.
func (h *harness) handlerRequest(method, path, peer string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(h.t.Context(), method, path, nil)
	req.RemoteAddr = net.JoinHostPort(peer, "40000")

	for k, v := range hdr {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.api.Handler().ServeHTTP(rec, req)

	return rec
}

// loungeID waits for the configured Kodi instance to be indexed and returns its ID.
func (h *harness) loungeID() int64 {
	h.t.Helper()

	var id int64

	require.Eventually(h.t, func() bool {
		ids := h.app.InstanceIDs()
		if len(ids) == 0 {
			return false
		}

		id = ids[0]

		st, err := h.app.InstanceStatus(h.t.Context(), id, false)

		return err == nil && st.Counts.Movies > 0
	}, 10*time.Second, 20*time.Millisecond)

	return id
}

func contains(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}

	return true
}

// libraryID returns the Lounge's library group once its catalog has loaded.
func (h *harness) libraryID(t *testing.T) int64 {
	t.Helper()

	lounge := h.loungeID()

	for _, g := range h.app.Groups() {
		for _, m := range g.Members {
			if m.ID == lounge {
				return g.ID
			}
		}
	}

	t.Fatal("the Lounge has no library group")

	return 0
}
