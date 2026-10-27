package kodi_test

import (
	"net"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
)

func TestProbeVersion(t *testing.T) {
	// The fake Kodi requires a password over HTTP, as a real one usually does; the WebSocket answers without one.
	srv := fakekodi.New(t, fakekodi.Options{Username: "kodi", Password: "s3cret"})

	u, err := url.Parse(srv.WebSocketURL())
	require.NoError(t, err)

	host, port, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)

	p, err := strconv.Atoi(port)
	require.NoError(t, err)

	v, err := kodi.ProbeVersion(t.Context(), host, p)
	require.NoError(t, err)
	assert.Equal(t, "21.3", v)

	_, err = kodi.ProbeVersion(t.Context(), host, 0)
	assert.Error(t, err, "no WebSocket port, nothing to ask")
}
