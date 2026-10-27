package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/basepath"
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
	assert.NotContains(t, do(t, h, http.MethodGet, "/encorex/api/v1/overview").Body.String(), "/encore/", "a longer first segment isn't the prefix")

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
