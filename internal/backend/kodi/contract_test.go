//go:build contract

package kodi

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// contractClient connects to the real Kodi named by ENCORE_CONTRACT_KODI_URL (and optionally ENCORE_CONTRACT_KODI_WS_URL).
func contractClient(t *testing.T) (*Client, bool) {
	t.Helper()

	httpURL := os.Getenv("ENCORE_CONTRACT_KODI_URL")
	if httpURL == "" {
		t.Skip("ENCORE_CONTRACT_KODI_URL is not set")
	}

	wsURL := os.Getenv("ENCORE_CONTRACT_KODI_WS_URL")

	cfg, err := ConfigFromURLs(httpURL, wsURL)
	require.NoError(t, err)

	cfg.DisableWebSocket = wsURL == ""
	cfg.ReconnectMin = 50 * time.Millisecond

	return NewClient(cfg, nil), wsURL != ""
}

func TestContractPingAndVersion(t *testing.T) {
	c, _ := contractClient(t)

	pong, err := c.JSONRPCPing(t.Context())
	require.NoError(t, err)
	assert.EqualValues(t, "pong", pong)

	v, err := c.APIVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, kodirpc.BaselineVersion.Major, v.Major)
	assert.False(t, v.Less(kodirpc.BaselineVersion), "encore needs API %s or newer, Kodi speaks %s", kodirpc.BaselineVersion, v)

	if want := os.Getenv("ENCORE_CONTRACT_KODI_VERSION"); want != "" {
		if api, ok := releaseAPIVersions[want]; ok {
			want = api
		}

		assert.Equal(t, want, v.String())
	}
}

// releaseAPIVersions maps the Kodi release names CI passes in ENCORE_CONTRACT_KODI_VERSION to the JSON-RPC API version each speaks. A plain version number is compared as is.
var releaseAPIVersions = map[string]string{
	"Omega": "13.5.0",
	"Piers": "13.1000.0",
}

func TestContractIntrospect(t *testing.T) {
	c, _ := contractClient(t)

	var desc struct {
		Methods map[string]json.RawMessage `json:"methods"`
	}
	require.NoError(t, c.Transport().Call(t.Context(), "JSONRPC.Introspect", map[string]any{"getdescriptions": false}, &desc))

	v, err := c.APIVersion(t.Context())
	require.NoError(t, err)

	// Every method the generated table says this version has must really exist.
	var missing []string

	for name, info := range kodirpc.Methods {
		if _, ok := desc.Methods[name]; info.AvailableIn(v) && overHTTP(info) && !ok {
			missing = append(missing, name)
		}
	}

	assert.Empty(t, missing, "kodirpc claims these exist in API %s", v)
}

// overHTTP reports whether Kodi lists the method when introspected over HTTP, which hides methods needing announcements or direct downloads.
func overHTTP(info kodirpc.MethodInfo) bool {
	for _, tr := range info.Transport {
		if tr != "Response" && tr != "FileDownloadRedirect" {
			return false
		}
	}

	return true
}

func TestContractBackend(t *testing.T) {
	c, ws := contractClient(t)
	b := NewBackend(c, BackendOptions{})
	ctx := t.Context()

	info, err := b.Info(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, info.Version)

	for _, kind := range []media.Kind{media.KindMovie, media.KindTVShow, media.KindMusicVideo, media.KindArtist, media.KindAlbum, media.KindSong, media.KindPlaylist} {
		_, err := b.Library(ctx, kind)
		require.NoError(t, err, "listing %ss", kind)
	}

	_, err = b.Item(ctx, media.Ref{Kind: media.KindMovie, ID: 987654})
	require.ErrorIs(t, err, media.ErrNotFound, "a missing movie maps to ErrNotFound on this Kodi version")

	err = b.Play(ctx, media.Ref{Kind: media.KindMovie, ID: 987654}, media.PlayOptions{})
	require.ErrorIs(t, err, media.ErrNotFound)

	np, err := b.NowPlaying(ctx)
	require.NoError(t, err)
	assert.Nil(t, np, "headless Kodi plays nothing")

	// The time zone is learned from these labels, so their format must hold on every Kodi version.
	local, dateKnown, err := b.localNow(ctx)
	require.NoError(t, err)
	assert.True(t, dateKnown, "System.Date(yyyy-mm-dd) is readable")
	assert.LessOrEqual(t, dateOffset(local, time.Now()).Abs(), 14*time.Hour)

	if !ws {
		return
	}

	events := collectEvents(t, b)
	require.Equal(t, media.Event{Kind: media.EventConnected}, nextEvent(t, events))

	_, err = b.Volume(ctx, media.VolumeChange{ToggleMute: true})
	require.NoError(t, err)

	_, err = b.Volume(ctx, media.VolumeChange{ToggleMute: true})
	require.NoError(t, err)
	assert.True(t, c.Transport().Connected())
}
