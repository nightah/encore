package smapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryAfter(t *testing.T) {
	d, ok := retryAfter("7")
	assert.True(t, ok)
	assert.Equal(t, 7*time.Second, d)

	d, ok = retryAfter(time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
	assert.True(t, ok)
	assert.InDelta(t, time.Hour.Seconds(), d.Seconds(), 2)

	_, ok = retryAfter("")
	assert.False(t, ok)

	_, ok = retryAfter("soon")
	assert.False(t, ok)
}

func TestBackoffBounds(t *testing.T) {
	p := RetryPolicy{}.withDefaults()

	for attempt := 1; attempt <= 30; attempt++ {
		d := p.backoff(attempt)
		assert.GreaterOrEqual(t, d, time.Duration(0))
		assert.LessOrEqual(t, d, p.Max)
	}

	assert.GreaterOrEqual(t, p.backoff(1), p.Base/2)
}

func TestDecide(t *testing.T) {
	p := RetryPolicy{MaxRetryAfter: 10 * time.Second}.withDefaults()
	get := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	post := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("{}"))

	wait, ok := p.decide(get, http.StatusTooManyRequests, http.Header{"Retry-After": {"60"}}, nil, 1)
	assert.True(t, ok)
	assert.Equal(t, 10*time.Second, wait, "Retry-After is capped")

	_, ok = p.decide(get, http.StatusBadRequest, nil, nil, 1)
	assert.False(t, ok)

	_, ok = p.decide(get, http.StatusServiceUnavailable, nil, nil, p.MaxAttempts)
	assert.False(t, ok, "attempts exhausted")

	_, ok = p.decide(get, 0, nil, errors.New("connection reset"), 1)
	assert.True(t, ok, "idempotent requests retry transport errors")

	_, ok = p.decide(post, 0, nil, errors.New("connection reset"), 1)
	assert.False(t, ok, "a POST may have been processed")

	post.GetBody = nil
	_, ok = p.decide(post, http.StatusTooManyRequests, nil, nil, 1)
	assert.False(t, ok, "a body that cannot be rewound cannot be resent")
}

func TestPollDefaults(t *testing.T) {
	p := PollConfig{}.withDefaults()
	assert.Equal(t, 5*time.Second, p.Interval, "SMAPI asks for at least 5 s between polls")
	assert.Equal(t, 15*time.Second, p.MaxInterval)
	assert.Equal(t, 15*time.Minute, p.Timeout)

	assert.Equal(t, 20*time.Second, PollConfig{Interval: 20 * time.Second}.withDefaults().MaxInterval, "never below the first wait")
}

func TestIdempotent(t *testing.T) {
	for method, want := range map[string]bool{
		http.MethodGet:    true,
		http.MethodHead:   true,
		http.MethodPut:    true,
		http.MethodDelete: true,
		http.MethodPost:   false,
		http.MethodPatch:  false,
	} {
		assert.Equal(t, want, idempotent(method), method)
	}
}

func TestDecideStopsWhenContextEnded(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, ok := RetryPolicy{}.withDefaults().decide(httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil), http.StatusServiceUnavailable, nil, nil, 1)
	assert.False(t, ok)
}

func TestRetryAfterPastDateIsZero(t *testing.T) {
	d, ok := retryAfter(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	assert.True(t, ok)
	assert.Zero(t, d)

	_, ok = retryAfter("-5")
	assert.False(t, ok, "a negative delay is not a valid Retry-After")
}

func TestDoerRewindFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, srv.URL, strings.NewReader("body"))
	require.NoError(t, err)

	rewind := errors.New("cannot rewind")
	req.GetBody = func() (io.ReadCloser, error) { return nil, rewind }

	d := &doer{http: srv.Client(), retry: RetryPolicy{Base: time.Millisecond, Max: time.Millisecond}.withDefaults()}

	_, err = d.Do(req) //nolint:bodyclose // no response on error
	require.ErrorIs(t, err, rewind)
}

func TestDoerResendsBodyOnRetry(t *testing.T) {
	var bodies []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))

		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadGateway)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader(`{"a":1}`))
	require.NoError(t, err)

	d := &doer{http: srv.Client(), retry: RetryPolicy{Base: time.Millisecond, Max: time.Millisecond}.withDefaults()}

	resp, err := d.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, []string{`{"a":1}`, `{"a":1}`}, bodies)
}

func TestSleep(t *testing.T) {
	require.NoError(t, sleep(t.Context(), 0))
	require.NoError(t, sleep(t.Context(), time.Millisecond))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, sleep(ctx, 0), context.Canceled, "a zero wait still reports an ended context")
	require.ErrorIs(t, sleep(ctx, time.Hour), context.Canceled)
}
