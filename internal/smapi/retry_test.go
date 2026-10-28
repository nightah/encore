package smapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
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

type bodyRecorder struct {
	statuses []int
	bodies   []string
}

// RoundTrip reads the body as it stands, without rewinding it the way net/http's own transport does, as a custom transport may.
func (b *bodyRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	b.bodies = append(b.bodies, string(body))
	status := b.statuses[len(b.bodies)-1]

	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func TestDoerRewindsBodyForEveryAttempt(t *testing.T) {
	rt := &bodyRecorder{statuses: []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusOK}}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://smapi.invalid/v1/skills", strings.NewReader(`{"a":1}`))
	require.NoError(t, err)

	d := &doer{http: &http.Client{Transport: rt}, retry: RetryPolicy{Base: time.Nanosecond, Max: time.Nanosecond}.withDefaults()}

	resp, err := d.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, []string{`{"a":1}`, `{"a":1}`, `{"a":1}`}, rt.bodies)
}

func TestBackoffNeverOverflowsToZero(t *testing.T) {
	// Three hours doubled twenty times overflows time.Duration, which must still mean the longest wait, not none.
	p := RetryPolicy{Base: 3 * time.Hour, Max: 24 * time.Hour}.withDefaults()

	for _, attempt := range []int{21, 30, 64} {
		d := p.backoff(attempt)
		assert.GreaterOrEqual(t, d, p.Max/2, attempt)
		assert.LessOrEqual(t, d, p.Max, attempt)
	}
}

func TestPollBacksOffUpToMaxInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var gaps []time.Duration

		last := time.Now()
		err := Poll(t.Context(), PollConfig{Interval: 2 * time.Second, MaxInterval: 4 * time.Second, Timeout: time.Hour}, func(context.Context) (bool, error) {
			now := time.Now()
			gaps = append(gaps, now.Sub(last))
			last = now

			return len(gaps) == 6, nil
		})
		require.NoError(t, err)

		assert.Equal(t, []time.Duration{0, 2 * time.Second, 3 * time.Second, 4 * time.Second, 4 * time.Second, 4 * time.Second}, gaps, "the first check is immediate, then each wait grows by half up to the cap")
	})
}

func TestSleep(t *testing.T) {
	require.NoError(t, sleep(t.Context(), 0))
	require.NoError(t, sleep(t.Context(), time.Millisecond))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, sleep(ctx, 0), context.Canceled, "a zero wait still reports an ended context")
	require.ErrorIs(t, sleep(ctx, time.Hour), context.Canceled)
}
