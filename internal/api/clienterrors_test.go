package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClientErrorReports(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.client()

	report := map[string]string{"kind": "render", "message": "chip is undefined", "path": "/", "stack": "TypeError: chip is undefined\n    at OutcomeChip"}

	// Pages served by Encore report before anyone signs in, with the CSRF token.
	assert.Equal(t, http.StatusNoContent, c.do(http.MethodPost, "/api/v1/client-errors", report).status)
	assert.Contains(t, h.logs.String(), `msg="browser error" component=api kind=render message="chip is undefined" path=/`)

	noToken := h.client()
	noToken.csrf = ""
	assert.Equal(t, http.StatusForbidden, noToken.do(http.MethodPost, "/api/v1/client-errors", report).status, "the CSRF token is required")

	report["kind"] = "bogus"
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/client-errors", report).status)
}

func TestClientErrorLimiter(t *testing.T) {
	var l clientErrorLimiter

	now := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC)

	for range clientErrorsPerMinute {
		ok, _ := l.allow(now)
		assert.True(t, ok)
	}

	ok, _ := l.allow(now.Add(30 * time.Second))
	assert.False(t, ok, "over the limit within the minute")

	ok, dropped := l.allow(now.Add(time.Minute))
	assert.True(t, ok, "a new minute starts afresh")
	assert.Equal(t, 1, dropped, "and reports what was dropped")
}
