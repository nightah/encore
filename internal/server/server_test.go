package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/basepath"
	"github.com/nightah/encore/internal/buildinfo"
	"github.com/nightah/encore/internal/config"
)

func newTestServer(t *testing.T, cfg config.Server, deps Deps) http.Handler {
	t.Helper()

	srv, err := New(cfg, slog.New(slog.DiscardHandler), deps)
	require.NoError(t, err)

	return srv.Handler()
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))

	return rec
}

func TestHealthz(t *testing.T) {
	h := newTestServer(t, config.Server{Address: ":0", BasePath: "/"}, Deps{})

	rec := do(t, h, http.MethodGet, PathHealthz)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

func TestAlexaUnavailableUntilConfigured(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	rec := do(t, h, http.MethodPost, PathAlexa)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestAlexaHandlerMounted(t *testing.T) {
	alexa := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{Alexa: alexa})

	testCases := []struct {
		name     string
		method   string
		expected int
	}{
		{name: "ShouldPassPOSTToTheAlexaHandler", method: http.MethodPost, expected: http.StatusTeapot},
		{name: "ShouldRejectGET", method: http.MethodGet, expected: http.StatusMethodNotAllowed},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, do(t, h, tc.method, PathAlexa).Code)
		})
	}
}

func TestAdminAPIMounted(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	alexa := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	h := newTestServer(t, config.Server{BasePath: "/encore/"}, Deps{Alexa: alexa, API: api})

	testCases := []struct {
		name     string
		method   string
		have     string
		expected int
	}{
		{name: "ShouldServeTheAPI", method: http.MethodGet, have: "/encore/api/v1/overview", expected: http.StatusTeapot},
		{name: "ShouldKeepTheAlexaRouteOutsideTheAPI", method: http.MethodPost, have: "/encore" + PathAlexa, expected: http.StatusAccepted},
		{name: "ShouldKeepTheHealthCheckOutsideTheAPI", method: http.MethodGet, have: "/encore" + PathHealthz, expected: http.StatusOK},
		{name: "ShouldNotFindAnUnknownAPIVersion", method: http.MethodGet, have: "/encore/api/v2/x", expected: http.StatusNotFound},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, do(t, h, tc.method, tc.have).Code)
		})
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	rec := do(t, h, http.MethodGet, "/api/v1/nope")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestBasePath(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path + " " + basepath.From(r.Context())))
	})
	h := newTestServer(t, config.Server{BasePath: "/encore/"}, Deps{API: api})

	assert.Equal(t, "/api/v1/overview /encore/", do(t, h, http.MethodGet, "/encore/api/v1/overview").Body.String())
	assert.Equal(t, "/api/v1/overview /", do(t, h, http.MethodGet, "/api/v1/overview").Body.String(), "served at / too, so a wrong base path never locks anyone out")
	assert.Equal(t, http.StatusOK, do(t, h, http.MethodGet, PathHealthz).Code)
	assert.Equal(t, http.StatusOK, do(t, h, http.MethodGet, "/encore"+PathHealthz).Code)

	rec := do(t, h, http.MethodGet, "/encore")
	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "/encore/", rec.Header().Get("Location"))
}

func TestBasePathChangesWhileRunning(t *testing.T) {
	base := "/"
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(basepath.From(r.Context()))) })
	h := newTestServer(t, config.Server{BasePath: "/ignored"}, Deps{API: api, BasePath: func() string { return base }})

	assert.Equal(t, "/", do(t, h, http.MethodGet, "/api/v1/overview").Body.String(), "Deps.BasePath wins over config")

	base = "/encore"

	assert.Equal(t, "/encore/", do(t, h, http.MethodGet, "/encore/api/v1/overview").Body.String())
	assert.Equal(t, "/", do(t, h, http.MethodGet, "/api/v1/overview").Body.String())
}

func TestSPA(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":           {Data: []byte(`<html><head><meta property="csp-nonce" content="{{ .CSPNonce }}"></head></html>`)},
		"static/app-abc123.js": {Data: []byte("console.log(1)")},
		"favicon.svg":          {Data: []byte("<svg/>")},
	}

	spa, err := newSPAHandler(fsys, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	t.Run("ShouldCarryTheBaseHrefTheRequestUsedInIndex", func(t *testing.T) {
		h := withBasePath(func() string { return "/encore" })(spa)

		assert.Contains(t, do(t, h, http.MethodGet, "/encore/kodi/lounge").Body.String(), `<head><base href="/encore/">`)
		assert.Contains(t, do(t, h, http.MethodGet, "/kodi/lounge").Body.String(), `<head><base href="/">`)
	})

	t.Run("ShouldFallBackToIndexWithANonceForAClientRoute", func(t *testing.T) {
		rec := do(t, spa, http.MethodGet, "/kodi/instances")
		require.Equal(t, http.StatusOK, rec.Code)

		m := regexp.MustCompile(`'nonce-([^']+)'`).FindStringSubmatch(rec.Header().Get("Content-Security-Policy"))
		require.Len(t, m, 2, "CSP must carry a nonce")

		assert.Contains(t, rec.Body.String(), `content="`+m[1]+`"`)
		assert.NotContains(t, rec.Body.String(), nonceMarker)
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	})

	t.Run("ShouldUseADifferentNoncePerRequest", func(t *testing.T) {
		a := do(t, spa, http.MethodGet, "/").Header().Get("Content-Security-Policy")
		b := do(t, spa, http.MethodGet, "/").Header().Get("Content-Security-Policy")
		assert.NotEqual(t, a, b)
	})

	t.Run("ShouldServeHashedAssetsAsImmutable", func(t *testing.T) {
		rec := do(t, spa, http.MethodGet, "/static/app-abc123.js")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")
	})

	t.Run("ShouldRevalidateOtherFiles", func(t *testing.T) {
		rec := do(t, spa, http.MethodGet, "/favicon.svg")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	})

	t.Run("ShouldNotFindAMissingAssetRatherThanServeTheShell", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, do(t, spa, http.MethodGet, "/static/missing.js").Code)
	})

	t.Run("ShouldRejectNonGET", func(t *testing.T) {
		assert.Equal(t, http.StatusMethodNotAllowed, do(t, spa, http.MethodPost, "/").Code)
	})
}

func TestSPAPlaceholderWithoutAssets(t *testing.T) {
	spa, err := newSPAHandler(fstest.MapFS{".gitkeep": {}}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	rec := do(t, spa, http.MethodGet, "/")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "not included in this build")
}

func TestServeShutsDownOnCancel(t *testing.T) {
	srv, err := New(config.Server{Address: "127.0.0.1:0", BasePath: "/"}, slog.New(slog.DiscardHandler), Deps{})
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- srv.Serve(ctx, ln) }()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+PathHealthz, http.NoBody)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	cancel()
	assert.NoError(t, <-done)
}

// TestServeEndsEventStreamsOnShutdown checks that shutting down ends the web interface's event streams, which last until their request ends, while an ordinary request in flight still finishes. A graceful shutdown that waited for the streams ran out its whole timeout whenever a browser was open, and air, which waits five seconds, started the next Encore before this one let go of the data directory. A browser's EventSource asks for the stream alone; other clients may list it among other types.
func TestServeEndsEventStreamsOnShutdown(t *testing.T) {
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldEndAStreamAskedForAlone", have: "text/event-stream"},
		{name: "ShouldEndAStreamListedAmongOtherTypes", have: "text/html;q=0.9, text/event-stream"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) { testServeEndsEventStreams(t, tc.have) })
	}
}

func testServeEndsEventStreams(t *testing.T, accept string) {
	t.Helper()

	streaming, ended := make(chan struct{}), make(chan struct{})
	slowStarted, release := make(chan struct{}), make(chan struct{})

	// The slow request is released once, in the test or, should it fail first, in its cleanup, so the server never waits on it forever.
	var once sync.Once

	releaseSlow := func() { once.Do(func() { close(release) }) }
	t.Cleanup(releaseSlow)

	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/events" {
			w.WriteHeader(http.StatusOK)
			http.NewResponseController(w).Flush()
			close(streaming)
			<-r.Context().Done()
			close(ended)

			return
		}

		close(slowStarted)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})

	srv, err := New(config.Server{Address: "127.0.0.1:0", BasePath: "/"}, slog.New(slog.DiscardHandler), Deps{API: api})
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- srv.Serve(ctx, ln) }()

	base := "http://" + ln.Addr().String() + "/api/v1/"

	stream, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"events", http.NoBody)
	require.NoError(t, err)
	stream.Header.Set("Accept", accept)

	resp, err := http.DefaultClient.Do(stream)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	<-streaming

	slow := make(chan int, 1)

	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"slow", http.NoBody)
		if err != nil {
			slow <- 0

			return
		}

		r, err := http.DefaultClient.Do(req)
		if err != nil {
			slow <- 0

			return
		}

		_ = r.Body.Close()
		slow <- r.StatusCode
	}()
	<-slowStarted

	cancel()
	releaseSlow()

	assert.Equal(t, http.StatusNoContent, <-slow, "the ordinary request finishes")

	// Waiting for the stream would run the graceful shutdown out of time, and Serve would say so.
	require.NoError(t, <-done)

	select {
	case <-ended:
	default:
		t.Fatal("the event stream outlived the server")
	}
}

func TestAPIDocs(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	rec := do(t, h, http.MethodGet, "/api/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "script-src 'self'")
	assert.Contains(t, rec.Body.String(), `<script src="docs/swagger-ui-bundle.js">`)

	testCases := []struct {
		name     string
		have     string
		expected int
	}{
		{name: "ShouldServeTheSwaggerUIBundle", have: "/api/docs/swagger-ui-bundle.js", expected: http.StatusOK},
		{name: "ShouldServeTheSwaggerUIStylesheet", have: "/api/docs/swagger-ui.css", expected: http.StatusOK},
		{name: "ShouldServeTheInitScript", have: "/api/docs/init.js", expected: http.StatusOK},
		{name: "ShouldRedirectAPIToTheDocs", have: "/api", expected: http.StatusTemporaryRedirect},
		{name: "ShouldServeOnlyThePagesAssets", have: "/api/docs/LICENSE", expected: http.StatusNotFound},
		{name: "ShouldNotFindOtherAPIPaths", have: "/api/elsewhere", expected: http.StatusNotFound},
		{name: "ShouldLeaveTheSPAAnsweringOutsideAPI", have: "/encore-is-not-mounted-here", expected: http.StatusOK},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, do(t, h, http.MethodGet, tc.have).Code)
		})
	}
}

func TestHealthzReportsTheVersionUncached(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	rec := do(t, h, http.MethodGet, PathHealthz)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, map[string]string{"status": "ok", "version": buildinfo.Get().Version}, body)
}

func TestHealthzAnswersOnlyReads(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	testCases := []struct {
		name     string
		method   string
		expected int
	}{
		{name: "ShouldAnswerHEADAsAGETWithoutTheBody", method: http.MethodHead, expected: http.StatusOK},
		{name: "ShouldNotAnswerPOST", method: http.MethodPost, expected: http.StatusNotFound},
		{name: "ShouldNotAnswerPUT", method: http.MethodPut, expected: http.StatusNotFound},
		{name: "ShouldNotAnswerDELETE", method: http.MethodDelete, expected: http.StatusNotFound},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, tc.method, PathHealthz)

			if tc.expected == http.StatusOK {
				assert.Equal(t, tc.expected, rec.Code)

				return
			}

			assert.Equal(t, tc.expected, rec.Code, "falls through to the API's JSON not found")
			assert.NotContains(t, rec.Body.String(), `"status"`)
		})
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldSetThemOnTheHealthCheck", have: PathHealthz},
		{name: "ShouldSetThemOnAnAPINotFound", have: "/api/v1/nope"},
		{name: "ShouldSetThemOnTheSPA", have: "/"},
		{name: "ShouldSetThemOnTheAPIDocs", have: "/api/"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, tc.have)
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "same-origin", rec.Header().Get("Referrer-Policy"))
			assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
		})
	}
}

func TestAlexaErrorsAreJSON(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	testCases := []struct {
		name     string
		method   string
		expected int
	}{
		{name: "ShouldReportUnavailableAsJSON", method: http.MethodPost, expected: http.StatusServiceUnavailable},
		{name: "ShouldReportGETNotAllowedAsJSON", method: http.MethodGet, expected: http.StatusMethodNotAllowed},
		{name: "ShouldReportPUTNotAllowedAsJSON", method: http.MethodPut, expected: http.StatusMethodNotAllowed},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, tc.method, PathAlexa)
			assert.Equal(t, tc.expected, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.NotEmpty(t, body["error"])
		})
	}

	assert.Equal(t, http.MethodPost, do(t, h, http.MethodGet, PathAlexa).Header().Get("Allow"))
}

func TestSPAServesIndexOnlyAsTheShell(t *testing.T) {
	spa, err := newSPAHandler(fstest.MapFS{
		"index.html":   {Data: []byte(`<html><head><script nonce="{{ .CSPNonce }}"></script></head></html>`)},
		"static/a.css": {Data: []byte("a{}")},
	}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	testCases := []struct {
		name     string
		have     string
		expected int
	}{
		{name: "ShouldServeTheShellAtTheRoot", have: "/", expected: http.StatusOK},
		{name: "ShouldServeTheShellForTheStaticDirectory", have: "/static", expected: http.StatusOK},
		{name: "ShouldServeTheShellForAnUncleanPath", have: "/./kodi/../settings", expected: http.StatusOK},
		{name: "ShouldNotServeIndexRaw", have: "/index.html", expected: http.StatusNotFound},
		{name: "ShouldNotServeIndexRawThroughAnUncleanPath", have: "/kodi/../index.html", expected: http.StatusNotFound},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, spa, http.MethodGet, tc.have)

			if tc.expected != http.StatusOK {
				assert.Equal(t, tc.expected, rec.Code)
				assert.NotContains(t, rec.Body.String(), nonceMarker, "index.html is never served raw, without its nonce")

				return
			}

			require.Equal(t, tc.expected, rec.Code)
			assert.NotContains(t, rec.Body.String(), nonceMarker)
			assert.Contains(t, rec.Body.String(), `<base href="/">`)
			assert.NotEmpty(t, rec.Header().Get("Content-Security-Policy"))
		})
	}
}

func TestSPAHeadRequests(t *testing.T) {
	spa, err := newSPAHandler(fstest.MapFS{
		"index.html":   {Data: []byte(`<html><head></head></html>`)},
		"static/a.css": {Data: []byte("a{}")},
	}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	rec := do(t, spa, http.MethodHead, "/kodi")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String(), "HEAD has no body")
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "'nonce-")

	rec = do(t, spa, http.MethodHead, "/static/a.css")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")

	rec = do(t, spa, http.MethodDelete, "/")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, HEAD", rec.Header().Get("Allow"))
}

func TestSPAWithoutAUI(t *testing.T) {
	spa, err := newSPAHandler(nil, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldServeThePlaceholderAtTheRoot", have: "/"},
		{name: "ShouldServeThePlaceholderForAClientRoute", have: "/settings"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, spa, http.MethodGet, tc.have)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, placeholderIndex, rec.Body.String(), "the placeholder is served as is, without a base href")
		})
	}

	assert.Equal(t, http.StatusNotFound, do(t, spa, http.MethodGet, "/static/app.js").Code)
}

func TestSPAPlaceholderLogsWhyAssetsAreMissing(t *testing.T) {
	var logs bytes.Buffer

	_, err := newSPAHandler(fstest.MapFS{}, slog.New(slog.NewTextHandler(&logs, nil)))
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "web interface assets not found")
}

func TestSPARejectsAnUnreadableIndex(t *testing.T) {
	_, err := newSPAHandler(fstest.MapFS{"index.html/child": {Data: []byte("x")}}, slog.New(slog.DiscardHandler))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading embedded index.html")
}

func TestWithBaseHref(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     string
		href     string
		expected string
	}{
		{name: "ShouldPutTheBaseFirstInHead", have: `<html><head><title>x</title></head></html>`, href: "/encore/", expected: `<html><head><base href="/encore/"><title>x</title></head></html>`},
		{name: "ShouldFindAnUpperCaseHead", have: `<HTML><HEAD></HEAD></HTML>`, href: "/", expected: `<HTML><HEAD><base href="/"></HEAD></HTML>`},
		{name: "ShouldEscapeTheHref", have: `<head></head>`, href: `/a"b<c>/`, expected: `<head><base href="/a&#34;b&lt;c&gt;/"></head>`},
		{name: "ShouldLeaveADocumentWithoutAHeadAlone", have: `<html><body></body></html>`, href: "/encore/", expected: `<html><body></body></html>`},
		{name: "ShouldFindAHeadWithAnAttribute", have: `<head lang="en"></head>`, href: "/", expected: `<head lang="en"><base href="/"></head>`},
		{name: "ShouldFindAHeadWithABareAttribute", have: `<head data-x></head>`, href: "/", expected: `<head data-x><base href="/"></head>`},
		{name: "ShouldFindAnUpperCaseHeadWithAttributes", have: `<HEAD LANG="en" data-x></HEAD>`, href: "/", expected: `<HEAD LANG="en" data-x><base href="/"></HEAD>`},
		{name: "ShouldFindAHeadWithAGreaterThanInAnAttribute", have: `<head data-x="a>b" data-y='c>d'></head>`, href: "/", expected: `<head data-x="a>b" data-y='c>d'><base href="/"></head>`},
		{name: "ShouldFindAHeadWithANewlineBeforeTheAttributes", have: "<head\n\tlang=\"en\">", href: "/", expected: "<head\n\tlang=\"en\"><base href=\"/\">"},
		{name: "ShouldFindASelfClosingHead", have: `<head/><title>x</title>`, href: "/", expected: `<head/><base href="/"><title>x</title>`},
		{name: "ShouldSkipAHeaderBeforeTheHead", have: `<header class="x"></header><head lang="en"></head>`, href: "/", expected: `<header class="x"></header><head lang="en"><base href="/"></head>`},
		{name: "ShouldNotMistakeAHeaderForTheHead", have: `<html><body><header></header><HEADER id="h"></HEADER></body></html>`, href: "/", expected: `<html><body><header></header><HEADER id="h"></HEADER></body></html>`},
		{name: "ShouldLeaveAnUnclosedHeadTagAlone", have: `<head lang="en`, href: "/", expected: `<head lang="en`},
		{name: "ShouldLeaveAHeadAtTheEndAlone", have: `<head`, href: "/", expected: `<head`},
		{name: "ShouldHandleNonASCIIThatLowersToAnotherLengthBeforeHead", have: "<!-- \u212a\u0130 --><HEAD>", href: "/", expected: "<!-- \u212a\u0130 --><HEAD><base href=\"/\">"},
		{name: "ShouldLeaveAnEmptyDocumentAlone", href: "/"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, string(withBaseHref([]byte(tc.have), tc.href)))
		})
	}
}

func TestContentSecurityPolicyUsesTheNonce(t *testing.T) {
	t.Parallel()

	csp := contentSecurityPolicy("abc")
	assert.Equal(t, map[string]string{
		"default-src":     "'self'",
		"script-src":      "'self' 'nonce-abc'",
		"style-src":       "'self' 'nonce-abc'",
		"img-src":         "'self' data:",
		"font-src":        "'self'",
		"connect-src":     "'self'",
		"object-src":      "'none'",
		"base-uri":        "'self'",
		"form-action":     "'self'",
		"frame-ancestors": "'none'",
	}, cspDirectives(t, csp), "every directive is pinned, so a loosened or dropped one is noticed")
	assert.NotContains(t, csp, "unsafe-inline")
}

// cspDirectives splits a Content-Security-Policy header into its directives, failing on a repeated one.
func cspDirectives(t *testing.T, csp string) map[string]string {
	t.Helper()

	out := map[string]string{}

	for d := range strings.SplitSeq(csp, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(d), " ")
		require.NotContains(t, out, name, "directive %s appears twice", name)
		out[name] = value
	}

	return out
}

func TestAPIDocsAssetsKeepTheirPolicy(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldKeepThePolicyOnThePage", have: "/api/"},
		{name: "ShouldKeepThePolicyOnAnAsset", have: "/api/docs/init.js"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, tc.have)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, apiDocsCSP, rec.Header().Get("Content-Security-Policy"))
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		})
	}

	assert.Equal(t, "public, max-age=86400", do(t, h, http.MethodGet, "/api/docs/swagger-ui.css").Header().Get("Cache-Control"), "the assets change only with Encore")
	assert.Equal(t, "no-cache", do(t, h, http.MethodGet, "/api/").Header().Get("Cache-Control"))
	assert.Equal(t, "'none'", cspDirectives(t, apiDocsCSP)["frame-ancestors"])
}

func TestSPAOnlyCachesTheAssetsDirectoryForever(t *testing.T) {
	spa, err := newSPAHandler(fstest.MapFS{
		"index.html":                {Data: []byte(`<html><head></head></html>`)},
		"static/app-abc123.js":      {Data: []byte("1")},
		"static.webmanifest":        {Data: []byte("{}")},
		"staticfiles/logo-1234.svg": {Data: []byte("<svg/>")},
	}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldCacheAnAssetForever", have: "/static/app-abc123.js", expected: "public, max-age=31536000, immutable"},
		{name: "ShouldRevalidateAFileNamedLikeTheDirectory", have: "/static.webmanifest", expected: "no-cache"},
		{name: "ShouldRevalidateAFileInADirectoryStartingWithStatic", have: "/staticfiles/logo-1234.svg", expected: "no-cache"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, spa, http.MethodGet, tc.have)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tc.expected, rec.Header().Get("Cache-Control"), "only files inside static/ carry content hashes")
		})
	}
}

func TestSPAServesTheWebManifestAsManifestJSON(t *testing.T) {
	spa, err := newSPAHandler(fstest.MapFS{
		"index.html":           {Data: []byte(`<html><head></head></html>`)},
		"manifest.webmanifest": {Data: []byte(`{"name":"Encore"}`)},
	}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	testCases := []struct {
		name   string
		method string
	}{
		{name: "ShouldServeItForGET", method: http.MethodGet},
		{name: "ShouldServeItForHEAD", method: http.MethodHead},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, spa, tc.method, "/manifest.webmanifest")
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "application/manifest+json", rec.Header().Get("Content-Type"), "the type must not depend on the host's mime.types")
		})
	}
}

func TestBasePathMatchesWholeSegmentsOnly(t *testing.T) {
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path + " " + basepath.From(r.Context())))
	})
	h := withBasePath(func() string { return "/encore/" })(echo)

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldStripTheBasePath", have: "/encore/kodi", expected: "/kodi /encore/"},
		{name: "ShouldStripTheBasePathAlone", have: "/encore/", expected: "/ /encore/"},
		{name: "ShouldNotStripAPrefixOfASegment", have: "/encorex/kodi", expected: "/encorex/kodi /"},
		{name: "ShouldNotStripAPrefixOfTheOnlySegment", have: "/encorex", expected: "/encorex /"},
		{name: "ShouldNotStripTheBasePathLaterInThePath", have: "/kodi/encore", expected: "/kodi/encore /"},
		{name: "ShouldServeTheRootAtTheRoot", have: "/", expected: "/ /"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, do(t, h, http.MethodGet, tc.have).Body.String())
		})
	}
}

func TestNewNonceIsRandomAndURLSafeEnough(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for range 100 {
		n := newNonce()
		assert.Regexp(t, `^[A-Za-z0-9+/]{24}$`, n, "18 random bytes in unpadded base64")
		assert.False(t, seen[n], "nonces don't repeat")
		seen[n] = true
	}
}

func TestRunListensUntilCancelled(t *testing.T) {
	srv, err := New(config.Server{Address: "127.0.0.1:0", BasePath: "/"}, slog.New(slog.DiscardHandler), Deps{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.NoError(t, srv.Run(ctx), "a cancelled server shuts down cleanly")
}

func TestRunReportsAnAddressInUse(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })

	srv, err := New(config.Server{Address: busy.Addr().String(), BasePath: "/"}, slog.New(slog.DiscardHandler), Deps{})
	require.NoError(t, err)

	err = srv.Run(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listening on "+busy.Addr().String())
}

func TestServeReturnsWhenTheListenerFails(t *testing.T) {
	srv, err := New(config.Server{BasePath: "/"}, slog.New(slog.DiscardHandler), Deps{})
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close())

	err = srv.Serve(t.Context(), ln)
	require.Error(t, err, "a dead listener ends Serve without waiting for cancellation")
	assert.NotErrorIs(t, err, http.ErrServerClosed)
}

func TestServeLogsTheAddressAndBasePath(t *testing.T) {
	var logs syncBuffer

	srv, err := New(config.Server{BasePath: "/encore/"}, slog.New(slog.NewTextHandler(&logs, nil)), Deps{})
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- srv.Serve(ctx, ln) }()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+PathHealthz, http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	cancel()
	require.NoError(t, <-done)
	assert.Contains(t, logs.String(), "address="+ln.Addr().String())
	assert.Contains(t, logs.String(), "base_path=/encore/")
}

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
