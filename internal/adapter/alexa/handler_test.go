package alexa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/testkit"
)

var update = flag.Bool("update", false, "rewrite golden response files")

// linkedDevice is the Echo the fake target directory knows; any other device is unlinked.
const linkedDevice = "amzn1.ask.device.LOUNGE"

// errRoomLookup is the fake target directory failing to look up the cellar.
var errRoomLookup = errors.New("database is locked")

// fakeTargets knows one linked Echo and two rooms: the bedroom has its own Kodi instance, the kitchen has none. Looking up the cellar fails.
type fakeTargets struct {
	target  actions.Target
	bedroom actions.Target
}

func (f fakeTargets) Target(_ context.Context, deviceID string) (actions.Target, error) {
	if deviceID != linkedDevice {
		return actions.Target{}, ErrUnlinked
	}

	return f.target, nil
}

func (f fakeTargets) TargetForRoom(ctx context.Context, deviceID, room string) (actions.Target, error) {
	switch strings.ToLower(room) {
	case "bedroom":
		return f.bedroom, nil
	case "kitchen":
		return f.Target(ctx, deviceID)
	case "cellar":
		return actions.Target{}, errRoomLookup
	}

	return actions.Target{}, ErrUnknownRoom
}

// DeviceName names the linked Echo; any other device is unnamed.
func (f fakeTargets) DeviceName(_ context.Context, deviceID string) (string, error) {
	if deviceID == linkedDevice {
		return "Lounge Echo", nil
	}

	return "", nil
}

type memRecorder struct {
	mu      sync.Mutex
	entries []LogEntry
}

func (m *memRecorder) Record(_ context.Context, e LogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries = append(m.entries, e)
}

func newHandler(t *testing.T) (*Handler, *testkit.Backend, *memRecorder) {
	t.Helper()

	h, backend, _, rec := newRoomHandler(t)

	return h, backend, rec
}

// newRoomHandler also returns the bedroom's backend.
func newRoomHandler(t *testing.T) (*Handler, *testkit.Backend, *testkit.Backend, *memRecorder) {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	log := slog.New(slog.DiscardHandler)

	speech, err := NewSpeech(src, log)
	require.NoError(t, err)

	items := testkit.SampleLibrary()
	backend := testkit.NewBackend(items...)
	bedroom := testkit.NewBackend(items...)
	matcher := &testkit.Matcher{Docs: testkit.Docs(items...)}
	rec := &memRecorder{}

	svc := actions.NewService(log)
	svc.SetSeed(1)

	h := &Handler{
		Service: svc,
		Speech:  speech,
		Targets: fakeTargets{
			target:  actions.Target{Name: "Lounge", Backend: backend, Matcher: matcher},
			bedroom: actions.Target{Name: "Bedroom", Backend: bedroom, Matcher: matcher},
		},
		Recorder: rec,
		Log:      log,
		Now:      func() time.Time { return time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC) },
	}

	return h, backend, bedroom, rec
}

// goldenState is a case's optional state.json: what the lounge backend is doing before the request.
type goldenState struct {
	// Playing is "<kind>/<id>" of a sample library item.
	Playing  string        `json:"playing"`
	Position time.Duration `json:"position"`
	// Clock is the media centre's time of day, "15:04".
	Clock      string `json:"clock"`
	Streams    bool   `json:"streams"`
	Favourites bool   `json:"favourites"`
	Addons     bool   `json:"addons"`
}

func (g goldenState) apply(t *testing.T, b *testkit.Backend) {
	t.Helper()

	if g.Playing != "" {
		for _, it := range testkit.SampleLibrary() {
			if fmt.Sprintf("%s/%d", it.Kind, it.ID) == g.Playing {
				b.SetPlaying(&media.NowPlaying{Item: it, Position: g.Position, Duration: it.Runtime})
			}
		}
	}

	if g.Clock != "" {
		clock, err := time.Parse("15:04", g.Clock)
		require.NoError(t, err)
		b.SetClock(clock)
	}

	if g.Streams {
		b.SetStreams(media.Streams{
			Audio:           []media.Stream{{Index: 0, Name: "English 5.1", Language: "eng"}, {Index: 1, Name: "Français", Language: "fre"}},
			CurrentAudio:    &media.Stream{Index: 0, Name: "English 5.1", Language: "eng"},
			Subtitles:       []media.Stream{{Index: 0, Name: "English", Language: "en"}, {Index: 1, Name: "Deutsch", Language: "de"}},
			CurrentSubtitle: &media.Stream{Index: 0, Name: "English", Language: "en"},
		})
	}

	if g.Favourites {
		b.SetFavourites(media.Favourite{Title: "Radio National", Type: "media", Path: "http://radio/rn.mp3"})
	}

	if g.Addons {
		b.SetAddons(media.Addon{ID: "plugin.video.youtube", Name: "YouTube"}, media.Addon{ID: "script.globalsearch", Name: "Global Search"})
	}
}

func serveBody(t *testing.T, h *Handler, body []byte) []byte {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeBody(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/alexa", bytes.NewReader(body)), body)
	require.Equal(t, http.StatusOK, rec.Code)

	return rec.Body.Bytes()
}

// TestGolden replays each testdata/golden/<case>/request.json, after applying the optional state.json, and compares the response with response.golden.json and the backend calls it made with calls.golden.json. Run with -update to rewrite the golden files after an intended change, then review the diff.
func TestGolden(t *testing.T) {
	dirs, err := filepath.Glob("testdata/golden/*")
	require.NoError(t, err)
	require.NotEmpty(t, dirs)

	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			h, backend, bedroom, _ := newRoomHandler(t)

			if raw, err := os.ReadFile(filepath.Join(dir, "state.json")); err == nil {
				var state goldenState
				require.NoError(t, json.Unmarshal(raw, &state))
				state.apply(t, backend)
			}

			req, err := os.ReadFile(filepath.Join(dir, "request.json"))
			require.NoError(t, err)

			var got bytes.Buffer
			require.NoError(t, json.Indent(&got, serveBody(t, h, req), "", "  "))
			got.WriteByte('\n')

			calls, err := json.MarshalIndent(map[string][]string{"lounge": backend.Calls(), "bedroom": bedroom.Calls()}, "", "  ")
			require.NoError(t, err)

			calls = append(calls, '\n')

			compareGolden(t, filepath.Join(dir, "response.golden.json"), got.Bytes())
			compareGolden(t, filepath.Join(dir, "calls.golden.json"), calls)
		})
	}
}

func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; run go test -update")
	assert.JSONEq(t, string(want), string(got))
}

func TestQuestionAndAnswerRoundTrip(t *testing.T) {
	h, backend, rec := newHandler(t)

	ask := serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-ambiguous/request.json"))

	var resp ResponseEnvelope
	require.NoError(t, json.Unmarshal(ask, &resp))
	require.NotNil(t, resp.SessionAttributes[sessionPending])
	assert.Empty(t, backend.Calls())

	// Alexa sends the session attributes back with the next request in the session.
	var yes RequestEnvelope
	require.NoError(t, json.Unmarshal(mustRead(t, "testdata/golden/yes-after-question/request.json"), &yes))
	yes.Session.Attributes = resp.SessionAttributes

	body, err := json.Marshal(yes)
	require.NoError(t, err)

	serveBody(t, h, body)
	assert.Equal(t, []string{"Play movie/1 resume"}, backend.Calls(), "yes picks the first title offered")

	require.Len(t, rec.entries, 2)
	assert.Equal(t, "asked", rec.entries[0].Outcome)
	assert.Equal(t, "played", rec.entries[1].Outcome)
	assert.Equal(t, "Lounge", rec.entries[1].Target)
}

func TestRecorderCapturesMatch(t *testing.T) {
	h, _, rec := newHandler(t)

	serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-unique/request.json"))

	require.Len(t, rec.entries, 1)
	e := rec.entries[0]
	assert.Equal(t, "WatchMovie", e.Intent)
	assert.Equal(t, map[string]string{"Movie": "hot fuzz"}, e.Slots)
	require.NotNil(t, e.Match)
	assert.Equal(t, "Playing Hot Fuzz.", e.Speech)
}

func TestSpeechDurations(t *testing.T) {
	assert.Equal(t, "1 hour 12 minutes", spokenDuration("en", 72*time.Minute))
	assert.Equal(t, "2 hours", spokenDuration("en", 2*time.Hour))
	assert.Equal(t, "less than a minute", spokenDuration("en", 20*time.Second))
	assert.Equal(t, "1 Stunde 1 Minute", spokenDuration("de", 61*time.Minute))
	assert.Equal(t, "18 minutes", spokenDuration("xx", 18*time.Minute), "unknown languages fall back to English")
}

func TestSpeechUnknownLocaleFallsBack(t *testing.T) {
	h, _, _ := newHandler(t)

	got := h.Speech.Render("xx-XX", actions.Message{ID: "playback.playing_movie", Params: map[string]string{"title": "Dune"}})
	assert.Equal(t, "Playing Dune.", got)
}

func TestSpeechUntranslatedFallsBackToEnglish(t *testing.T) {
	s := &Speech{
		responses: map[string]map[string]string{
			"de-DE": {"goodbye": "Okay."},
			"en-US": {"goodbye": "Okay.", "playback.now_movie": "You're watching {title}. There's {remaining} left."},
		},
		languages: map[string]string{"de-DE": "de", "en-US": "en"},
		log:       slog.New(slog.DiscardHandler),
	}

	got := s.Render("de-DE", actions.Message{ID: "playback.now_movie", Params: map[string]string{"title": "Dune"}, Durations: map[string]time.Duration{"remaining": 72 * time.Minute}})
	assert.Equal(t, "You're watching Dune. There's 1 hour 12 minutes left.", got, "English text with English durations until German is translated")
	assert.Empty(t, s.Render("de-DE", actions.Message{ID: "no.such.message"}))
}

func TestProgressiveSend(t *testing.T) {
	var got struct {
		Header    map[string]string `json:"header"`
		Directive map[string]string `json:"directive"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/directives", r.URL.Path)
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	env := &RequestEnvelope{}
	env.Request.RequestID = "req-1"
	env.Context.System.APIEndpoint = srv.URL
	env.Context.System.APIAccessToken = "token"

	require.NoError(t, NewProgressive(srv.Client()).Send(t.Context(), env, "Waking Lounge…"))
	assert.Equal(t, "req-1", got.Header["requestId"])
	assert.Equal(t, "VoicePlayer.Speak", got.Directive["type"])
	assert.Equal(t, "Waking Lounge…", got.Directive["speech"])
}

func TestProgressiveSkipsWithoutEndpoint(t *testing.T) {
	assert.NoError(t, NewProgressive(nil).Send(t.Context(), &RequestEnvelope{}, "hello"))
}

func TestSlotResolvedIDs(t *testing.T) {
	var s Slot
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Movie","value":"the new dune","resolutions":{"resolutionsPerAuthority":[
		{"authority":"amzn1.er-authority.echo-sdk.skill.AMAZON.Movie","status":{"code":"ER_SUCCESS_MATCH"},"values":[{"value":{"name":"Dune: Part Two","id":"movie/3"}}]},
		{"authority":"amzn1.er-authority.echo-sdk.dynamic","status":{"code":"ER_SUCCESS_NO_MATCH"}}]}}`), &s))

	assert.Equal(t, []string{"movie/3"}, s.ResolvedIDs())

	req := titleRequest(s, []media.Kind{media.KindMovie}, nil)
	assert.InDelta(t, resolvedBoost, req.Hints["movie/3"], 1e-9)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()

	b, err := os.ReadFile(path)
	require.NoError(t, err)

	return b
}

func TestPlayMediaDeepSearch(t *testing.T) {
	req := mustRead(t, "testdata/golden/play-media-not-found/request.json")
	req = bytes.Replace(req, []byte(`"value": "conclave"`), []byte(`"value": "innuendo"`), 1)

	h, backend, _ := newHandler(t)
	serveBody(t, h, req)
	assert.Empty(t, backend.Calls(), "without deep search, play X only looks for films and shows")

	h, backend, _ = newHandler(t)
	ft := h.Targets.(fakeTargets)
	ft.target.DeepSearch = true
	h.Targets = ft

	serveBody(t, h, req)
	assert.Contains(t, backend.Calls(), "Play album/41 resume", "deep search finds the album")
}

// reportingBackend reports Play to the call observer, as the Kodi transport does for every call.
type reportingBackend struct {
	*testkit.Backend
}

func (b reportingBackend) Play(ctx context.Context, ref media.Ref, opts media.PlayOptions) error {
	media.ReportCall(ctx, media.Call{Method: "Player.Open", Duration: 40 * time.Millisecond})

	return b.Backend.Play(ctx, ref, opts)
}

func TestRecorderCapturesCallsAndTiming(t *testing.T) {
	h, backend, rec := newHandler(t)
	ft := h.Targets.(fakeTargets)
	ft.target.Backend = reportingBackend{backend}
	h.Targets = ft

	serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-unique/request.json"))

	require.Len(t, rec.entries, 1)
	e := rec.entries[0]
	assert.Equal(t, []media.Call{{Method: "Player.Open", Duration: 40 * time.Millisecond}}, e.KodiCalls)
	assert.Zero(t, e.Verify, "the request didn't go through VerifiedFunc")
}

// The logged duration is how long Alexa waited, so it counts the signature check that ran before the handler.
func TestRecorderDurationIncludesVerify(t *testing.T) {
	h, _, rec := newHandler(t)
	now := fixedNow
	h.Now = func() time.Time {
		now = now.Add(100 * time.Millisecond)

		return now
	}

	body := mustRead(t, "testdata/golden/watch-movie-unique/request.json")
	ctx := context.WithValue(t.Context(), verifyTimeKey{}, 650*time.Millisecond)
	h.ServeBody(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/alexa", bytes.NewReader(body)), body)

	require.Len(t, rec.entries, 1)
	e := rec.entries[0]
	assert.Equal(t, 650*time.Millisecond, e.Verify)
	assert.Equal(t, 750*time.Millisecond, e.Duration, "650 ms verifying plus the 100 ms the handler's clock saw")
}

// A session end has no action outcome; it is logged as ok rather than with an empty outcome the UI can't show.
func TestRecorderLogsSessionEndAsOK(t *testing.T) {
	h, _, rec := newHandler(t)

	req := mustRead(t, "testdata/golden/play-media-not-found/request.json")
	req = bytes.Replace(req, []byte(`"type": "IntentRequest"`), []byte(`"type": "SessionEndedRequest"`), 1)
	serveBody(t, h, req)

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "ok", rec.entries[0].Outcome)
}

func withDisabled(t *testing.T, deep bool, groups ...media.Content) (*Handler, *testkit.Backend) {
	t.Helper()

	h, backend, _ := newHandler(t)

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	h.IntentContent = src.Model.IntentContent()
	ft := h.Targets.(fakeTargets)
	ft.target.Disabled = groups
	ft.target.DeepSearch = deep
	h.Targets = ft

	return h, backend
}

func TestContentDisabled(t *testing.T) {
	const declined = "This skill is set up without that kind of title."

	t.Run("a music intent is declined without music", func(t *testing.T) {
		h, backend := withDisabled(t, true, media.ContentMusic)
		res := serveBody(t, h, mustRead(t, "testdata/golden/listen-artist/request.json"))
		assert.Contains(t, string(res), declined)
		assert.Empty(t, backend.Calls())
	})

	t.Run("play X leaves out music even with deep search", func(t *testing.T) {
		req := bytes.Replace(mustRead(t, "testdata/golden/play-media-not-found/request.json"), []byte(`"value": "conclave"`), []byte(`"value": "innuendo"`), 1)
		h, backend := withDisabled(t, true, media.ContentMusic)
		serveBody(t, h, req)
		assert.NotContains(t, backend.Calls(), "Play album/41 resume")
	})

	t.Run("play X with only music finds music", func(t *testing.T) {
		req := bytes.Replace(mustRead(t, "testdata/golden/play-media-not-found/request.json"), []byte(`"value": "conclave"`), []byte(`"value": "innuendo"`), 1)
		h, backend := withDisabled(t, true, media.ContentMovies, media.ContentTV)
		serveBody(t, h, req)
		assert.Contains(t, backend.Calls(), "Play album/41 resume")
	})

	t.Run("play X is declined with neither films nor shows", func(t *testing.T) {
		h, backend := withDisabled(t, false, media.ContentMovies, media.ContentTV)
		res := serveBody(t, h, mustRead(t, "testdata/golden/play-media-not-found/request.json"))
		assert.Contains(t, string(res), declined, "without deep search, play X looks only for films and shows")
		assert.Empty(t, backend.Calls())
	})

	t.Run("a recommendation of an unknown type is any video", func(t *testing.T) {
		h, _ := withDisabled(t, false, media.ContentMovies, media.ContentTV)
		res := serveBody(t, h, intentRequest(t, "Recommend", map[string]slotSpec{"MediaType": {"podcasts", "podcast"}}))

		var resp ResponseEnvelope
		require.NoError(t, json.Unmarshal(res, &resp))
		followup, err := json.Marshal(resp.SessionAttributes["followup"])
		require.NoError(t, err)
		assert.JSONEq(t, `{"recommend":{"kinds":["musicvideo"]}}`, string(followup), "a music video is the video left")
	})

	t.Run("a movie recommendation is declined without movies", func(t *testing.T) {
		h, _ := withDisabled(t, false, media.ContentMovies)
		res := serveBody(t, h, mustRead(t, "testdata/golden/recommend-movie/request.json"))
		assert.Contains(t, string(res), declined)
	})

	t.Run("what's new falls back to what is left", func(t *testing.T) {
		h, _ := withDisabled(t, false, media.ContentMovies)
		res := serveBody(t, h, mustRead(t, "testdata/golden/whats-new-shows/request.json"))
		assert.NotContains(t, string(res), declined)
	})

	t.Run("everything allowed by default", func(t *testing.T) {
		h, backend := withDisabled(t, false)
		serveBody(t, h, mustRead(t, "testdata/golden/listen-artist/request.json"))
		assert.NotEmpty(t, backend.Calls())
	})
}

func TestRecorderMarksSimulatorRequests(t *testing.T) {
	req := mustRead(t, "testdata/golden/play-media-not-found/request.json")

	h, _, rec := newHandler(t)
	serveBody(t, h, req)

	h2, _, rec2 := newHandler(t)
	h2.Simulating = func() bool { return true }
	serveBody(t, h2, req)

	require.Len(t, rec.entries, 1)
	require.Len(t, rec2.entries, 1)
	assert.False(t, rec.entries[0].Simulated)
	assert.True(t, rec2.entries[0].Simulated, "a request during the deploy smoke test")
}

func TestConfirmSound(t *testing.T) {
	speech := func(confirm bool, golden string) map[string]any {
		t.Helper()

		h, _, _ := newHandler(t)
		ft := h.Targets.(fakeTargets)
		ft.target.ConfirmSound = confirm
		h.Targets = ft

		var res struct {
			Response struct {
				OutputSpeech map[string]any `json:"outputSpeech"`
			} `json:"response"`
		}

		require.NoError(t, json.Unmarshal(serveBody(t, h, mustRead(t, "testdata/golden/"+golden+"/request.json")), &res))

		return res.Response.OutputSpeech
	}

	assert.Equal(t, map[string]any{"type": "SSML", "ssml": `<speak><audio src="` + ConfirmSoundURL + `"/></speak>`}, speech(true, "fullscreen"), "a command with nothing to say plays the sound")
	assert.Nil(t, speech(false, "fullscreen"), "silence when it is off")
	assert.Equal(t, "PlainText", speech(true, "watch-movie-in-bedroom")["type"], "a spoken answer stays spoken")
}

func TestServeBodyRejectsUndecodableEnvelope(t *testing.T) {
	h, backend, rec := newHandler(t)

	for _, body := range []string{"", "{", `{"request":"IntentRequest"}`, `[]`} {
		w := httptest.NewRecorder()
		h.ServeBody(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/alexa", nil), []byte(body))

		assert.Equal(t, http.StatusBadRequest, w.Code, body)
	}

	assert.Empty(t, backend.Calls())
	assert.Empty(t, rec.entries, "nothing is logged for a request Encore could not read")
}

func TestServeBodyIgnoresOtherRequestTypes(t *testing.T) {
	h, backend, rec := newHandler(t)

	req := bytes.Replace(mustRead(t, "testdata/golden/play-media-not-found/request.json"), []byte(`"type": "IntentRequest"`), []byte(`"type": "AudioPlayer.PlaybackStarted"`), 1)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(serveBody(t, h, req), &resp))

	assert.Equal(t, map[string]any{"version": "1.0", "response": map[string]any{}}, resp, "an empty response acknowledges an event")
	assert.Empty(t, backend.Calls())
	require.Len(t, rec.entries, 1)
	assert.Equal(t, "AudioPlayer.PlaybackStarted", rec.entries[0].Type)
	assert.Equal(t, "ok", rec.entries[0].Outcome)
}

type failingNames struct {
	fakeTargets

	err error
}

func (f failingNames) DeviceName(context.Context, string) (string, error) { return "", f.err }

func TestIdentifyEchoReportsLookupFailure(t *testing.T) {
	h, _, rec := newHandler(t)
	boom := errors.New("database locked")
	h.Targets = failingNames{fakeTargets: h.Targets.(fakeTargets), err: boom}

	var resp ResponseEnvelope
	require.NoError(t, json.Unmarshal(serveBody(t, h, mustRead(t, "testdata/golden/identify-echo-named/request.json")), &resp))

	assert.True(t, *resp.Response.ShouldEndSession)
	assert.NotContains(t, resp.Response.OutputSpeech.Text, "Lounge Echo")
	require.Len(t, rec.entries, 1)
	assert.Equal(t, "error", rec.entries[0].Outcome)
	require.ErrorIs(t, rec.entries[0].Err, boom)
}

func TestSpokenIDSuffix(t *testing.T) {
	for in, want := range map[string]string{
		"amzn1.ask.device.AMAXYZSJY": "S J Y",
		"AB":                         "A B",
		"":                           "",
		"device-äöü":                 "ä ö ü",
		"ABCD":                       "B C D",
		"ABC":                        "A B C",
	} {
		assert.Equal(t, want, spokenIDSuffix(in), in)
	}
}

func TestHandlerClockDefaultsToNow(t *testing.T) {
	h, _, rec := newHandler(t)
	h.Now = nil

	before := time.Now()

	serveBody(t, h, mustRead(t, "testdata/golden/help/request.json"))

	require.Len(t, rec.entries, 1)
	assert.False(t, rec.entries[0].Time.Before(before), "entries are stamped with the wall clock when no clock is set")
}

func TestHandlerWithoutRecorder(t *testing.T) {
	h, backend, _ := newHandler(t)
	h.Recorder = nil

	serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-unique/request.json"))
	assert.NotEmpty(t, backend.Calls())
}

// sleepyBackend is unreachable until woken, as a Kodi instance on a sleeping PC is.
type sleepyBackend struct {
	*testkit.Backend

	mu    sync.Mutex
	awake bool
}

func (b *sleepyBackend) Play(ctx context.Context, ref media.Ref, opts media.PlayOptions) error {
	b.mu.Lock()
	awake := b.awake
	b.mu.Unlock()

	if !awake {
		return media.ErrUnreachable
	}

	return b.Backend.Play(ctx, ref, opts)
}

func (b *sleepyBackend) wake(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.awake = true

	return nil
}

func TestProgressiveResponseWhileWaking(t *testing.T) {
	for name, status := range map[string]int{"delivered": http.StatusNoContent, "refused": http.StatusForbidden} {
		t.Run(name, func(t *testing.T) {
			var (
				mu     sync.Mutex
				speech []string
			)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Directive map[string]string `json:"directive"`
				}

				if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					mu.Lock()
					speech = append(speech, body.Directive["speech"])
					mu.Unlock()
				}

				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)

			h, backend, _ := newHandler(t)
			sleepy := &sleepyBackend{Backend: backend}
			ft := h.Targets.(fakeTargets)
			ft.target.Backend = sleepy
			ft.target.Wake = sleepy.wake
			h.Targets = ft
			h.Progressive = NewProgressive(srv.Client())

			var env RequestEnvelope
			require.NoError(t, json.Unmarshal(mustRead(t, "testdata/golden/watch-movie-unique/request.json"), &env))
			env.Context.System.APIEndpoint = srv.URL
			env.Context.System.APIAccessToken = "token"

			body, err := json.Marshal(env)
			require.NoError(t, err)

			var resp ResponseEnvelope
			require.NoError(t, json.Unmarshal(serveBody(t, h, body), &resp))

			mu.Lock()
			defer mu.Unlock()

			assert.Equal(t, []string{"Waking Lounge…"}, speech)
			assert.Equal(t, []string{"Play movie/4 resume"}, backend.Calls(), "the action is retried once the Kodi instance is awake, whether or not Alexa took the interim speech")
		})
	}

	t.Run("without progressive responses", func(t *testing.T) {
		h, backend, _ := newHandler(t)
		sleepy := &sleepyBackend{Backend: backend}
		ft := h.Targets.(fakeTargets)
		ft.target.Backend = sleepy
		ft.target.Wake = sleepy.wake
		h.Targets = ft
		require.Nil(t, h.Progressive)

		// Alexa offers an endpoint for progressive responses, which goes unused.
		var env RequestEnvelope
		require.NoError(t, json.Unmarshal(mustRead(t, "testdata/golden/watch-movie-unique/request.json"), &env))
		env.Context.System.APIEndpoint = "http://127.0.0.1:1"
		env.Context.System.APIAccessToken = "token"

		body, err := json.Marshal(env)
		require.NoError(t, err)

		var resp ResponseEnvelope
		require.NoError(t, json.Unmarshal(serveBody(t, h, body), &resp))
		assert.Equal(t, []string{"Play movie/4 resume"}, backend.Calls(), "the Kodi instance is woken all the same, with nothing said meanwhile")
	})
}

func TestProgressiveSendFailures(t *testing.T) {
	env := func(endpoint string) *RequestEnvelope {
		e := &RequestEnvelope{}
		e.Request.RequestID = "req-1"
		e.Context.System.APIEndpoint = endpoint
		e.Context.System.APIAccessToken = "token"

		return e
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	t.Cleanup(srv.Close)

	err := NewProgressive(srv.Client()).Send(t.Context(), env(srv.URL), "Waking Lounge…")
	require.EqualError(t, err, "sending progressive response: status 429")

	err = NewProgressive(nil).Send(t.Context(), env("http://127.0.0.1:1"), "Waking Lounge…")
	require.ErrorContains(t, err, "sending progressive response:")

	err = NewProgressive(nil).Send(t.Context(), env("http://exa mple.com"), "Waking Lounge…")
	require.Error(t, err)

	called := false
	quiet := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	t.Cleanup(quiet.Close)

	require.NoError(t, NewProgressive(quiet.Client()).Send(t.Context(), env(quiet.URL), ""), "nothing to say sends nothing")

	noToken := env(quiet.URL)
	noToken.Context.System.APIAccessToken = ""
	require.NoError(t, NewProgressive(quiet.Client()).Send(t.Context(), noToken, "hi"))
	assert.False(t, called)
}

func TestProgressiveSendGivesUpQuickly(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(block)
		srv.Close()
	})

	e := &RequestEnvelope{}
	e.Context.System.APIEndpoint = srv.URL
	e.Context.System.APIAccessToken = "token"

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := NewProgressive(srv.Client()).Send(ctx, e, "hi")
	require.ErrorIs(t, err, context.Canceled, "a progressive response never outlives the request")
}

func TestDescribeOutcome(t *testing.T) {
	msg := actions.Message{ID: "x"}

	for want, o := range map[string]actions.Outcome{
		"error":     {Err: errors.New("x"), Played: &media.Item{}},
		"played":    {Played: &media.Item{}},
		"asked":     {Pending: []actions.Choice{{}}},
		"not_found": {Say: actions.Message{ID: "match.not_found"}},
		"ok":        {Say: msg},
	} {
		assert.Equal(t, want, describeOutcome(o))
	}

	assert.Equal(t, "asked", describeOutcome(actions.Outcome{Followup: &actions.Followup{}}))
}

func TestSessionValueIgnoresMalformedAttributes(t *testing.T) {
	assert.Nil(t, pendingChoices(nil))
	assert.Nil(t, pendingChoices(map[string]any{sessionPending: "not a list"}))
	assert.Nil(t, sessionFollowupOf(map[string]any{sessionFollowup: []any{1, 2}}))
	assert.Nil(t, sessionFollowupOf(map[string]any{sessionFollowup: func() {}}), "a value that cannot be encoded is ignored")
}

func TestSlotValues(t *testing.T) {
	assert.Nil(t, slotValues(nil))
	assert.Equal(t, map[string]string{"Movie": "dune"}, slotValues(map[string]Slot{"Movie": {Value: "dune"}, "Room": {}}), "empty slots are left out")
}
