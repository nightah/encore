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

	assert.Equal(t, http.StatusTeapot, do(t, h, http.MethodPost, PathAlexa).Code)
	assert.Equal(t, http.StatusMethodNotAllowed, do(t, h, http.MethodGet, PathAlexa).Code)
}

func TestAdminAPIMounted(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	alexa := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	h := newTestServer(t, config.Server{BasePath: "/encore/"}, Deps{Alexa: alexa, API: api})

	assert.Equal(t, http.StatusTeapot, do(t, h, http.MethodGet, "/encore/api/v1/overview").Code)
	assert.Equal(t, http.StatusAccepted, do(t, h, http.MethodPost, "/encore"+PathAlexa).Code, "the Alexa route stays outside the API")
	assert.Equal(t, http.StatusOK, do(t, h, http.MethodGet, "/encore"+PathHealthz).Code, "and so does the health check")
	assert.Equal(t, http.StatusNotFound, do(t, h, http.MethodGet, "/encore/api/v2/x").Code)
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

	t.Run("index carries the base href the request used", func(t *testing.T) {
		h := withBasePath(func() string { return "/encore" })(spa)

		assert.Contains(t, do(t, h, http.MethodGet, "/encore/kodi/lounge").Body.String(), `<head><base href="/encore/">`)
		assert.Contains(t, do(t, h, http.MethodGet, "/kodi/lounge").Body.String(), `<head><base href="/">`)
	})

	t.Run("client route falls back to index with a nonce", func(t *testing.T) {
		rec := do(t, spa, http.MethodGet, "/kodi/instances")
		require.Equal(t, http.StatusOK, rec.Code)

		m := regexp.MustCompile(`'nonce-([^']+)'`).FindStringSubmatch(rec.Header().Get("Content-Security-Policy"))
		require.Len(t, m, 2, "CSP must carry a nonce")

		assert.Contains(t, rec.Body.String(), `content="`+m[1]+`"`)
		assert.NotContains(t, rec.Body.String(), nonceMarker)
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	})

	t.Run("nonce differs per request", func(t *testing.T) {
		a := do(t, spa, http.MethodGet, "/").Header().Get("Content-Security-Policy")
		b := do(t, spa, http.MethodGet, "/").Header().Get("Content-Security-Policy")
		assert.NotEqual(t, a, b)
	})

	t.Run("hashed assets are immutable", func(t *testing.T) {
		rec := do(t, spa, http.MethodGet, "/static/app-abc123.js")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")
	})

	t.Run("other files revalidate", func(t *testing.T) {
		rec := do(t, spa, http.MethodGet, "/favicon.svg")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	})

	t.Run("missing asset is 404 not the shell", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, do(t, spa, http.MethodGet, "/static/missing.js").Code)
	})

	t.Run("non-GET is rejected", func(t *testing.T) {
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

func TestAPIDocs(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	rec := do(t, h, http.MethodGet, "/api/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "script-src 'self'")
	assert.Contains(t, rec.Body.String(), `<script src="docs/swagger-ui-bundle.js">`)

	for _, asset := range []string{"swagger-ui-bundle.js", "swagger-ui.css", "init.js"} {
		assert.Equal(t, http.StatusOK, do(t, h, http.MethodGet, "/api/docs/"+asset).Code, asset)
	}

	assert.Equal(t, http.StatusTemporaryRedirect, do(t, h, http.MethodGet, "/api").Code, "/api redirects to the docs")
	assert.Equal(t, http.StatusNotFound, do(t, h, http.MethodGet, "/api/docs/LICENSE").Code, "only the page's assets are served")
	assert.Equal(t, http.StatusNotFound, do(t, h, http.MethodGet, "/api/elsewhere").Code)
	assert.Equal(t, http.StatusOK, do(t, h, http.MethodGet, "/encore-is-not-mounted-here").Code, "the SPA still answers outside /api/")
}

func TestHealthzReportsTheVersionUncached(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	rec := do(t, h, http.MethodGet, PathHealthz)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, map[string]string{"status": "ok", "version": buildinfo.Get().Version}, body)
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	for _, target := range []string{PathHealthz, "/api/v1/nope", "/", "/api/"} {
		rec := do(t, h, http.MethodGet, target)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), target)
		assert.Equal(t, "same-origin", rec.Header().Get("Referrer-Policy"), target)
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"), target)
	}
}

func TestAlexaErrorsAreJSON(t *testing.T) {
	h := newTestServer(t, config.Server{BasePath: "/"}, Deps{})

	for method, want := range map[string]int{http.MethodPost: http.StatusServiceUnavailable, http.MethodGet: http.StatusMethodNotAllowed, http.MethodPut: http.StatusMethodNotAllowed} {
		rec := do(t, h, method, PathAlexa)
		assert.Equal(t, want, rec.Code, method)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), method)

		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), method)
		assert.NotEmpty(t, body["error"], method)
	}

	assert.Equal(t, http.MethodPost, do(t, h, http.MethodGet, PathAlexa).Header().Get("Allow"))
}

func TestSPAServesIndexOnlyAsTheShell(t *testing.T) {
	spa, err := newSPAHandler(fstest.MapFS{
		"index.html":   {Data: []byte(`<html><head><script nonce="{{ .CSPNonce }}"></script></head></html>`)},
		"static/a.css": {Data: []byte("a{}")},
	}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	for _, target := range []string{"/", "/static", "/./kodi/../settings"} {
		rec := do(t, spa, http.MethodGet, target)
		require.Equal(t, http.StatusOK, rec.Code, target)
		assert.NotContains(t, rec.Body.String(), nonceMarker, target)
		assert.Contains(t, rec.Body.String(), `<base href="/">`, target)
		assert.NotEmpty(t, rec.Header().Get("Content-Security-Policy"), target)
	}

	for _, target := range []string{"/index.html", "/kodi/../index.html"} {
		rec := do(t, spa, http.MethodGet, target)
		assert.Equal(t, http.StatusNotFound, rec.Code, target)
		assert.NotContains(t, rec.Body.String(), nonceMarker, "%s: index.html is never served raw, without its nonce", target)
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

	for _, target := range []string{"/", "/settings"} {
		rec := do(t, spa, http.MethodGet, target)
		require.Equal(t, http.StatusOK, rec.Code, target)
		assert.Equal(t, placeholderIndex, rec.Body.String(), "%s: the placeholder is served as is, without a base href", target)
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

	for _, tc := range []struct {
		name, index, href, want string
	}{
		{"first in head", `<html><head><title>x</title></head></html>`, "/encore/", `<html><head><base href="/encore/"><title>x</title></head></html>`},
		{"upper case head", `<HTML><HEAD></HEAD></HTML>`, "/", `<HTML><HEAD><base href="/"></HEAD></HTML>`},
		{"escaped", `<head></head>`, `/a"b<c>/`, `<head><base href="/a&#34;b&lt;c&gt;/"></head>`},
		{"no head", `<html><body></body></html>`, "/encore/", `<html><body></body></html>`},
		{"head with attributes isn't matched", `<head lang="en"></head>`, "/", `<head lang="en"></head>`},
		{"empty", ``, "/", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, string(withBaseHref([]byte(tc.index), tc.href)))
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

	for _, target := range []string{"/api/", "/api/docs/init.js"} {
		rec := do(t, h, http.MethodGet, target)
		require.Equal(t, http.StatusOK, rec.Code, target)
		assert.Equal(t, apiDocsCSP, rec.Header().Get("Content-Security-Policy"), target)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), target)
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

	for target, want := range map[string]string{
		"/static/app-abc123.js":      "public, max-age=31536000, immutable",
		"/static.webmanifest":        "no-cache",
		"/staticfiles/logo-1234.svg": "no-cache",
	} {
		rec := do(t, spa, http.MethodGet, target)
		require.Equal(t, http.StatusOK, rec.Code, target)
		assert.Equal(t, want, rec.Header().Get("Cache-Control"), "%s: only files inside static/ carry content hashes", target)
	}
}

func TestBasePathMatchesWholeSegmentsOnly(t *testing.T) {
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path + " " + basepath.From(r.Context())))
	})
	h := withBasePath(func() string { return "/encore/" })(echo)

	for target, want := range map[string]string{
		"/encore/kodi":  "/kodi /encore/",
		"/encore/":      "/ /encore/",
		"/encorex/kodi": "/encorex/kodi /",
		"/encorex":      "/encorex /",
		"/kodi/encore":  "/kodi/encore /",
		"/":             "/ /",
	} {
		assert.Equal(t, want, do(t, h, http.MethodGet, target).Body.String(), target)
	}
}

func TestNewNonceIsRandomAndURLSafeEnough(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for range 100 {
		n, err := newNonce()
		require.NoError(t, err)
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
