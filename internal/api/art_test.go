package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
)

func TestNowPlayingArt(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lounge := h.loungeID()

	resp, err := http.Post(h.kodi.URL(), "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"Player.Open","params":{"item":{"movieid":1}}}`))
	require.NoError(t, err)
	resp.Body.Close()

	var inst Instance

	require.Eventually(t, func() bool {
		c.json(http.MethodGet, fmt.Sprintf("/api/v1/kodi/instances/%d", lounge), nil, http.StatusOK, &inst)

		return inst.NowPlaying.V != nil && inst.NowPlaying.V.ArtURL != ""
	}, 10*time.Second, 100*time.Millisecond)

	art := inst.NowPlaying.V.ArtURL
	assert.Equal(t, fmt.Sprintf("/api/v1/kodi/instances/%d/art?ref=%s", lounge, url.QueryEscape("image://fake/movie-1-poster.png/")), art)

	img := c.do(http.MethodGet, art, nil)
	require.Equal(t, http.StatusOK, img.status)
	assert.True(t, bytes.HasPrefix(img.body, []byte("\x89PNG")))
	assert.Equal(t, "image/png", img.header.Get("Content-Type"))
	assert.Contains(t, img.header.Get("Content-Security-Policy"), "sandbox", "artwork opened on its own can't run script")

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, fmt.Sprintf("/api/v1/kodi/instances/%d/art?ref=%s", lounge, url.QueryEscape("http://192.0.2.1/secret")), nil).status, "only image:// references")
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/v1/kodi/instances/999/art?ref=image%3A%2F%2Ffake%2Fx%2F", nil).status)
	assert.Equal(t, http.StatusUnauthorized, h.client().do(http.MethodGet, art, nil).status, "artwork needs a sign-in")
}

func TestArtContentType(t *testing.T) {
	for ct, want := range map[string]string{
		"image/png":                "image/png",
		"image/jpeg; charset=x":    "image/jpeg; charset=x",
		"text/html; charset=utf-8": "application/octet-stream",
		"application/javascript":   "application/octet-stream",
		"":                         "application/octet-stream",
		"image":                    "application/octet-stream",
	} {
		assert.Equal(t, want, artContentType(ct), ct)
	}
}

func TestArtHeadersAndFailures(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lounge := h.loungeID()

	img := c.do(http.MethodGet, artURL(lounge, "image://fake/movie-1-poster.png/"), nil)
	require.Equal(t, http.StatusOK, img.status)
	assert.Equal(t, "private, max-age=86400", img.header.Get("Cache-Control"), "the browser keeps artwork for a day, privately")
	assert.Equal(t, "nosniff", img.header.Get("X-Content-Type-Options"))

	p := c.do(http.MethodGet, artURL(lounge, "image://elsewhere/missing.png/"), nil).problem(t)
	assert.Equal(t, http.StatusBadGateway, p.Status, "Kodi has no such image")

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, fmt.Sprintf("/api/v1/kodi/instances/%d/art", lounge), nil).status, "ref is required")
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, artURL(lounge, "image://"+strings.Repeat("a", 2048)), nil).status, "ref is bounded")
}

// A Kodi instance, or something posing as one, can't serve a page that would run on Encore's origin.
func TestArtFromKodiIsNeverAPage(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	den := fakekodi.New(t, fakekodi.Options{})
	hostile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/image/") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<script>alert(document.cookie)</script>")

			return
		}

		den.ServeHTTP(w, r)
	}))
	t.Cleanup(hostile.Close)

	var inst Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Den", "url": hostile.URL + "/jsonrpc"}, http.StatusCreated, &inst)

	resp := c.do(http.MethodGet, artURL(inst.ID, "image://fake/x.svg/"), nil)
	require.Equal(t, http.StatusOK, resp.status)
	assert.Equal(t, "application/octet-stream", resp.header.Get("Content-Type"))
	assert.Equal(t, artCSP, resp.header.Get("Content-Security-Policy"))
}
