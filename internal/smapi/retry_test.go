package smapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
