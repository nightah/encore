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
	testCases := []struct {
		name     string
		have     string
		expected time.Duration
		delta    time.Duration
		ok       bool
	}{
		{name: "ShouldParseSeconds", have: "7", expected: 7 * time.Second, ok: true},
		{name: "ShouldParseHTTPDate", have: time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), expected: time.Hour, delta: 2 * time.Second, ok: true},
		{name: "ShouldRejectEmpty"},
		{name: "ShouldRejectText", have: "soon"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := retryAfter(tc.have)

			assert.Equal(t, tc.ok, ok)
			assert.InDelta(t, tc.expected.Seconds(), actual.Seconds(), tc.delta.Seconds())
		})
	}
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

	testCases := []struct {
		name       string
		method     string
		noGetBody  bool
		status     int
		header     http.Header
		haveErr    error
		attempt    int
		expected   time.Duration
		expectedOK bool
	}{
		{name: "ShouldCapRetryAfter", method: http.MethodGet, status: http.StatusTooManyRequests, header: http.Header{"Retry-After": {"60"}}, attempt: 1, expected: 10 * time.Second, expectedOK: true},
		{name: "ShouldNotRetryBadRequest", method: http.MethodGet, status: http.StatusBadRequest, attempt: 1},
		{name: "ShouldNotRetryWhenAttemptsExhausted", method: http.MethodGet, status: http.StatusServiceUnavailable, attempt: p.MaxAttempts},
		{name: "ShouldRetryIdempotentTransportError", method: http.MethodGet, haveErr: errors.New("connection reset"), attempt: 1, expectedOK: true},
		{name: "ShouldNotRetryPOSTTransportError", method: http.MethodPost, haveErr: errors.New("connection reset"), attempt: 1},
		{name: "ShouldNotRetryBodyThatCannotBeRewound", method: http.MethodPost, noGetBody: true, status: http.StatusTooManyRequests, attempt: 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader

			if tc.method == http.MethodPost {
				body = strings.NewReader("{}")
			}

			req := httptest.NewRequestWithContext(t.Context(), tc.method, "/", body)

			if tc.noGetBody {
				req.GetBody = nil
			}

			actual, ok := p.decide(req, tc.status, tc.header, tc.haveErr, tc.attempt)

			assert.Equal(t, tc.expectedOK, ok)

			if tc.expected != 0 {
				assert.Equal(t, tc.expected, actual)
			}
		})
	}
}

func TestPollDefaults(t *testing.T) {
	p := PollConfig{}.withDefaults()
	assert.Equal(t, 5*time.Second, p.Interval, "SMAPI asks for at least 5 s between polls")
	assert.Equal(t, 15*time.Second, p.MaxInterval)
	assert.Equal(t, 15*time.Minute, p.Timeout)

	assert.Equal(t, 20*time.Second, PollConfig{Interval: 20 * time.Second}.withDefaults().MaxInterval, "never below the first wait")
}

func TestIdempotent(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected bool
	}{
		{name: "ShouldTreatGETAsIdempotent", have: http.MethodGet, expected: true},
		{name: "ShouldTreatHEADAsIdempotent", have: http.MethodHead, expected: true},
		{name: "ShouldTreatPUTAsIdempotent", have: http.MethodPut, expected: true},
		{name: "ShouldTreatDELETEAsIdempotent", have: http.MethodDelete, expected: true},
		{name: "ShouldNotTreatPOSTAsIdempotent", have: http.MethodPost},
		{name: "ShouldNotTreatPATCHAsIdempotent", have: http.MethodPatch},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, idempotent(tc.have))
		})
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

	testCases := []struct {
		name string
		have int
	}{
		{name: "ShouldCapAttempt21", have: 21},
		{name: "ShouldCapAttempt30", have: 30},
		{name: "ShouldCapAttempt64", have: 64},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := p.backoff(tc.have)

			assert.GreaterOrEqual(t, actual, p.Max/2)
			assert.LessOrEqual(t, actual, p.Max)
		})
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
	testCases := []struct {
		name     string
		canceled bool
		have     time.Duration
		err      error
	}{
		{name: "ShouldReturnImmediatelyForZero"},
		{name: "ShouldWait", have: time.Millisecond},
		{name: "ShouldReportEndedContextForZero", canceled: true, err: context.Canceled},
		{name: "ShouldReportEndedContext", canceled: true, have: time.Hour, err: context.Canceled},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tc.canceled {
				cancel()
			}

			err := sleep(ctx, tc.have)

			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

type failingTransport struct{ left int }

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if f.left > 0 {
		f.left--

		return nil, errors.New("connection reset")
	}

	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
}

// A network error retried counts as failed; one the policy can't retry, such as a POST's, counts as given up only after the last attempt.
func TestDoerCountsNetworkErrors(t *testing.T) {
	counts := &retryCounts{}
	d := &doer{http: &http.Client{Transport: &failingTransport{left: 2}}, retry: RetryPolicy{Base: time.Nanosecond, Max: time.Nanosecond}.withDefaults(), counts: counts}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://smapi.invalid/v1/vendors", http.NoBody)
	require.NoError(t, err)

	resp, err := d.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, int64(2), counts.failed.Load())

	d.http.Transport = &failingTransport{left: 1}

	req, err = http.NewRequestWithContext(t.Context(), http.MethodPost, "https://smapi.invalid/v1/skills", http.NoBody)
	require.NoError(t, err)

	_, err = d.Do(req) //nolint:bodyclose // no response on error
	require.Error(t, err)
	assert.Equal(t, int64(2), counts.failed.Load())
	assert.Zero(t, counts.gaveUp.Load(), "a POST isn't retried after a network error, so it isn't given up on either")
}
