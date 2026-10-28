package api

import (
	"net/http"
	"strings"
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

	_, dropped = l.allow(now.Add(2 * time.Minute))
	assert.Zero(t, dropped, "dropped reports are counted once")

	ok, _ = l.allow(now.Add(90 * time.Second))
	assert.True(t, ok, "the window runs from its first report, not the clock minute")
}

func TestClientErrorFlood(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	report := map[string]string{"kind": "unhandled", "message": "loop", "path": "/settings"}

	for range clientErrorsPerMinute + 3 {
		assert.Equal(t, http.StatusNoContent, c.do(http.MethodPost, "/api/v1/client-errors", report).status, "the page is never told to stop")
	}

	assert.Equal(t, clientErrorsPerMinute, strings.Count(h.logs.String(), `msg="browser error"`), "at most 20 a minute are logged")
	assert.Contains(t, h.logs.String(), "user=admin", "a signed-in admin is named")

	h.skew.Store(int64(time.Minute))
	c.json(http.MethodPost, "/api/v1/client-errors", report, http.StatusNoContent, nil)
	assert.Contains(t, h.logs.String(), `msg="browser errors not logged: too many in a minute" component=api dropped=3`)
	assert.Equal(t, clientErrorsPerMinute+1, strings.Count(h.logs.String(), `msg="browser error"`))
}
