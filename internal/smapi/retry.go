package smapi

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

func (p RetryPolicy) withDefaults() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 6
	}

	if p.Base <= 0 {
		p.Base = time.Second
	}

	if p.Max <= 0 {
		p.Max = 30 * time.Second
	}

	if p.MaxRetryAfter <= 0 {
		p.MaxRetryAfter = 2 * time.Minute
	}

	return p
}

func (p RetryPolicy) decide(req *http.Request, status int, hdr http.Header, err error, attempt int) (time.Duration, bool) {
	if attempt >= p.MaxAttempts || req.Context().Err() != nil {
		return 0, false
	}

	if !rewindable(req) {
		return 0, false
	}

	if err != nil {
		// The request may have been processed; only repeat it when doing so is harmless.
		return p.backoff(attempt), idempotent(req.Method)
	}

	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
	default:
		return 0, false
	}

	wait := p.backoff(attempt)
	if ra, ok := retryAfter(hdr.Get("Retry-After")); ok {
		wait = max(wait, min(ra, p.MaxRetryAfter))
	}

	return wait, true
}

// backoff is exponential with jitter in [d/2, d).
func (p RetryPolicy) backoff(attempt int) time.Duration {
	d := p.Base << min(attempt-1, 20)
	if d > p.Max || d <= 0 {
		d = p.Max
	}

	half := d / 2

	return half + rand.N(half+1) //nolint:gosec // jitter needs no cryptographic randomness
}

func hasBody(req *http.Request) bool {
	return req.Body != nil && req.Body != http.NoBody
}

// rewindable reports whether req can be sent again.
func rewindable(req *http.Request) bool {
	return !hasBody(req) || req.GetBody != nil
}

func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// retryAfter parses a Retry-After header in either delay-seconds or HTTP-date form.
func retryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}

	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0), true
	}

	return 0, false
}
