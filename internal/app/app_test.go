package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/secrets"
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

func TestEndToEndCopiesFollowThePreferredQuality(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		stream := func(width, height int) *fakekodi.StreamDetails {
			return &fakekodi.StreamDetails{Video: []fakekodi.VideoStream{{Codec: "hevc", Width: width, Height: height}}}
		}

		lib := fakekodi.DefaultLibrary()
		hotFuzz := lib.Movies[5]
		hotFuzz.StreamDetails = stream(1920, 1080)
		lib.Movies[5].StreamDetails = stream(3840, 1600)
		lib.Movies[5].Resume = fakekodi.Resume{Position: 282, Total: 7260}
		hotFuzz.MovieID = 60
		lib.Movies = append(lib.Movies, hotFuzz)

		woe := lib.Episodes[1]
		woe.StreamDetails = stream(1920, 1080)
		lib.Episodes[1].StreamDetails = stream(3840, 2160)
		woe.EpisodeID = 120
		lib.Episodes = append(lib.Episodes, woe)

		kodi := newKodi(t, fakekodi.Options{Library: lib})
		h := startHarness(t, onFakes(defaultTuning()), config.KodiInstance{Name: "Lounge", URL: kodi.URL(), WebSocketURL: kodi.WebSocketURL(), Default: true, PreferredQuality: "1080p"})
		h.kodi = kodi
		h.waitIndexed(t)

		resp := h.say(t, "WatchMovie", "Movie", "hot fuzz")
		assert.Equal(t, "Playing Hot Fuzz.", speech(resp), "two copies are one title")

		resp = h.say(t, "WatchNextEpisode", "Show", "severance")
		assert.Contains(t, speech(resp), "Severance, season 2, episode 4")

		calls := h.kodi.CallsTo("Player.Open")
		require.Len(t, calls, 2)
		assert.JSONEq(t, `{"item":{"movieid":60},"options":{"resume":{"minutes":4,"seconds":42}}}`, string(calls[0].Params), "the instance prefers 1080p over the 2160p copy, carrying on where that stopped")
		assert.Contains(t, string(calls[1].Params), `"episodeid":120`, "and the 1080p copy of the episode")
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

func TestNewRefusesToStartWithoutTheGeneratedKey(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()

	a, err := New(t.Context(), &cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.NoError(t, a.Secrets().Put(t.Context(), "test/secret", []byte("hunter2")))
	require.NoError(t, a.Close())

	// The key Encore generated next to the database is lost, but the secrets it sealed are still there.
	require.NoError(t, os.Remove(filepath.Join(cfg.DataDir, secrets.KeyFileName)))

	_, err = New(t.Context(), &cfg, slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "opening secrets: ")
	require.ErrorContains(t, err, "is missing; restore it or set secrets.key")
	assert.NoFileExists(t, filepath.Join(cfg.DataDir, secrets.KeyFileName), "no new key replaces the lost one")
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

func TestEndToEndSkinSearch(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := fakekodi.DefaultLibrary()
		lib.Addons = append(lib.Addons,
			fakekodi.Addon{AddonID: "script.skinvariables", Type: "xbmc.python.script", Name: "Skin Variables", Enabled: true},
			fakekodi.Addon{AddonID: "skin.copacetic", Type: "xbmc.gui.skin", Name: "Copacetic", Enabled: true},
		)

		kodi := newKodi(t, fakekodi.Options{Library: lib})
		kodi.SetSkin("skin.arctic.fuse.3")
		kodi.ShowWindow("1105", 1)

		h := startHarness(t, onFakes(defaultTuning()), config.KodiInstance{Name: "Lounge", URL: kodi.URL(), WebSocketURL: kodi.WebSocketURL(), Default: true, SearchMode: "skin"})
		h.kodi = kodi
		h.waitIndexed(t)

		assert.Equal(t, "Searching for dune.", speech(h.say(t, "GlobalSearch", "Query", "dune")))

		var plugin []string

		for _, c := range kodi.CallsTo("Files.GetDirectory") {
			if strings.Contains(string(c.Params), "plugin://script.skinvariables/") {
				plugin = append(plugin, string(c.Params))
			}
		}

		require.Len(t, plugin, 2, "the window is opened and its search box focused through script.skinvariables' plugin")
		assert.Contains(t, plugin[0], "command=ReplaceWindow%281105%29")
		assert.Contains(t, plugin[1], "command=SetFocus%283000%29")

		typed := kodi.CallsTo("Input.SendText")
		require.Len(t, typed, 1)
		assert.JSONEq(t, `{"text":"dune","done":true}`, string(typed[0].Params))
		assert.Empty(t, kodi.CallsTo("Addons.ExecuteAddon"), "Global Search isn't run")

		kodi.SetSkin("skin.copacetic")
		h.say(t, "GlobalSearch", "Query", "hot fuzz")

		opened := kodi.CallsTo("GUI.ActivateWindow")
		require.NotEmpty(t, opened, "the skin is read again with each request, and a skin without a profile shows the library")
		assert.Contains(t, string(opened[len(opened)-1].Params), `videodb://movies/titles/?xsp=`)
		assert.Contains(t, string(opened[len(opened)-1].Params), `hot%20fuzz`)
		assert.Empty(t, kodi.CallsTo("Addons.ExecuteAddon"), "not Global Search")

		var notes []string

		settled(t, func() bool {
			entries, err := h.app.DB().RequestLog().List(t.Context(), store.RequestLogQuery{Limit: 10})
			if err != nil || len(entries) < 2 {
				return false
			}

			notes = notes[:0]

			for _, e := range entries {
				var m loggedMatch
				require.NoError(t, json.Unmarshal(e.Match, &m))
				notes = append(notes, m.Note)
			}

			return true
		})

		assert.ElementsMatch(t, []string{"skin search: opened skin.arctic.fuse.3's search", "skin search: skin.copacetic has no profile; showed the library"}, notes)

		skin, mode, err := h.app.InstanceSkin(t.Context(), h.instance(t).id)
		require.NoError(t, err)
		assert.Equal(t, media.SearchModeSkin, mode)
		assert.Equal(t, media.Skin{ID: "skin.copacetic", Name: "Copacetic", Search: media.SkinSearchNoProfile}, skin)

		_, _, err = h.app.InstanceSkin(t.Context(), 999)
		require.ErrorIs(t, err, ErrNoRuntime)
	})
}
