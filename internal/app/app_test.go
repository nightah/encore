package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/store"
)

const testDevice = "amzn1.ask.device.TEST"

type harness struct {
	app     *App
	kodi    *fakekodi.Server
	handler *alexa.Handler
}

func newHarness(t *testing.T, def bool) *harness {
	t.Helper()

	kodi := newKodi(t, fakekodi.Options{})
	h := startHarness(t, onFakes(defaultTuning()), config.KodiInstance{Name: "Lounge", URL: kodi.URL(), WebSocketURL: kodi.WebSocketURL(), Default: def})
	h.kodi = kodi

	return h
}

// startHarness runs an App with one configured Kodi instance, fake or real, and the Alexa handler in front of it.
func startHarness(t *testing.T, tune tuning, kodi config.KodiInstance) *harness {
	t.Helper()

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Kodi = []config.KodiInstance{kodi}

	log := slog.New(slog.DiscardHandler)

	a, err := newApp(t.Context(), &cfg, log, tune)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = a.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
		assert.NoError(t, a.Close())
	})

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := alexa.NewSpeech(src, log)
	require.NoError(t, err)

	h := &alexa.Handler{Service: actions.NewService(log), Speech: speech, Targets: a, Recorder: a.Recorder(), Log: log}

	return &harness{app: a, handler: h}
}

func (h *harness) waitIndexed(t *testing.T) {
	t.Helper()

	h.app.mu.RLock()
	defer h.app.mu.RUnlock()

	require.Len(t, h.app.instances, 1)

	for _, inst := range h.app.instances {
		settled(t, func() bool { return inst.Index.Len() > 0 }, "the library is indexed")
	}
}

func (h *harness) say(t *testing.T, intent, slot, value string) alexa.ResponseEnvelope {
	t.Helper()

	body := fmt.Appendf(nil, `{"version":"1.0","session":{"new":true,"attributes":{}},
		"context":{"System":{"device":{"deviceId":%q}}},
		"request":{"type":"IntentRequest","requestId":"req-%d","timestamp":"2026-10-26T09:00:00Z","locale":"en-AU",
		"intent":{"name":%q,"slots":{%q:{"name":%q,"value":%q}}}}}`, testDevice, time.Now().UnixNano(), intent, slot, slot, value)

	rec := httptest.NewRecorder()
	h.handler.ServeBody(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/alexa", bytes.NewReader(body)), body)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp alexa.ResponseEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	return resp
}

func speech(resp alexa.ResponseEnvelope) string {
	if resp.Response.OutputSpeech == nil {
		return ""
	}

	return resp.Response.OutputSpeech.Text
}

func TestEndToEndPlayMovie(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)

		resp := h.say(t, "WatchMovie", "Movie", "hot fuzz")
		assert.Equal(t, "Playing Hot Fuzz.", speech(resp))

		calls := h.kodi.CallsTo("Player.Open")
		require.Len(t, calls, 1)
		assert.Contains(t, string(calls[0].Params), `"movieid":6`)

		dev, err := h.app.DB().EchoDevices().GetByAlexaID(t.Context(), testDevice)
		require.NoError(t, err)
		assert.EqualValues(t, 1, dev.RequestCount)

		settled(t, func() bool {
			entries, err := h.app.DB().RequestLog().List(t.Context(), store.RequestLogQuery{})
			return err == nil && len(entries) == 1 && entries[0].Outcome == "played"
		})
	})
}

func TestEndToEndAmbiguousTitleAsks(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)

		resp := h.say(t, "WatchMovie", "Movie", "dune")

		assert.Contains(t, speech(resp), "Did you mean")
		assert.Empty(t, h.kodi.CallsTo("Player.Open"))
		require.NotNil(t, resp.Response.ShouldEndSession)
		assert.False(t, *resp.Response.ShouldEndSession)
	})
}

func TestEndToEndNextEpisode(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)

		resp := h.say(t, "WatchNextEpisode", "Show", "severance")
		assert.Contains(t, speech(resp), "Severance, season 2, episode 4")

		calls := h.kodi.CallsTo("Player.Open")
		require.Len(t, calls, 1)
		assert.Contains(t, string(calls[0].Params), `"episodeid":102`)
	})
}

func TestEndToEndUnlinkedDevice(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, false)
		h.waitIndexed(t)

		resp := h.say(t, "WatchMovie", "Movie", "hot fuzz")
		assert.Contains(t, speech(resp), "isn't linked to a Kodi")
		assert.Empty(t, h.kodi.CallsTo("Player.Open"))
	})
}

func TestConfiguredInstancesAreReconciled(t *testing.T) {
	h := newHarness(t, true)

	list, err := h.app.DB().KodiInstances().List(t.Context())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Lounge", list[0].Name)
	assert.True(t, list[0].IsDefaultForUnlinked)
	assert.NotZero(t, list[0].WSPort, "the WebSocket port comes from websocket_url")
}

func TestSavingTheLearnedMACLeavesTheRecordAlone(t *testing.T) {
	h := newHarness(t, true)

	h.app.mu.RLock()
	var inst *Instance
	for _, i := range h.app.instances {
		inst = i
	}
	h.app.mu.RUnlock()

	done := make(chan struct{})

	go func() {
		defer close(done)

		inst.saveMAC(t.Context())
	}()

	// Voice requests read the record while the MAC address is saved.
	for {
		_ = inst.language()

		select {
		case <-done:
			assert.Equal(t, "DC:A6:32:4B:19:0E", strings.ToUpper(inst.Waker.MAC().String()))

			return
		default:
		}
	}
}
