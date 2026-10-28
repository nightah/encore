package kodi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
)

func TestImageFailures(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "s3cret"})
	cfg := testConfig(t, s)
	cfg.Username, cfg.Password = "kodi", "wrong"
	c := NewClient(cfg, nil)

	_, _, err := c.Image(t.Context(), "image://fake/movie-1-poster.png/")
	require.ErrorIs(t, err, ErrUnauthorized)
	require.ErrorIs(t, err, media.ErrUnreachable, "wrong credentials are reported like an unreachable Kodi")

	cfg.Password = "s3cret"
	c = NewClient(cfg, nil)

	_, _, err = c.Image(t.Context(), "image://elsewhere/missing.png/")
	require.ErrorContains(t, err, "HTTP 404")
	require.NotErrorIs(t, err, media.ErrUnreachable, "a missing image is not a missing Kodi")

	_, _, err = c.Image(t.Context(), "image://"+strings.Repeat("a", maxImageRef))
	require.ErrorIs(t, err, ErrNotImage, "overlong references are refused")

	_, _, err = c.Image(t.Context(), "")
	require.ErrorIs(t, err, ErrNotImage)

	s.Close()

	_, _, err = c.Image(t.Context(), "image://fake/movie-1-poster.png/")
	require.ErrorIs(t, err, media.ErrUnreachable)
}

func TestImageRequestFailure(t *testing.T) {
	c := NewClient(Config{Host: "bad host"}, nil)

	_, _, err := c.Image(t.Context(), "image://fake/x.png/")

	require.ErrorContains(t, err, "kodi: image")
	assert.NotErrorIs(t, err, media.ErrUnreachable, "a host that can't be in a URL is a configuration mistake, not an absent Kodi")
}
