package alexa

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
)

// stalledBackend never answers Play, and ignores its context while it waits, as a backend wedged on something without a deadline would.
type stalledBackend struct {
	media.Backend

	release chan struct{}
}

func (b *stalledBackend) Play(context.Context, media.Ref, media.PlayOptions) error {
	<-b.release

	return nil
}

func TestRequestDeadline(t *testing.T) {
	for name, tc := range map[string]struct {
		stall  func(t *testing.T, h *Handler)
		speech string
		target string
	}{
		"a Kodi instance that doesn't answer": {
			stall: func(t *testing.T, h *Handler) {
				s := fakekodi.NewNetwork().New(t, fakekodi.Options{})
				stalled := make(chan struct{})
				t.Cleanup(func() { close(stalled) })

				s.Handle("Player.Open", func(json.RawMessage) (any, *fakekodi.Error) {
					<-stalled

					return "OK", nil
				})

				cfg, err := kodi.ConfigFromURLs(s.URL(), s.WebSocketURL())
				require.NoError(t, err)

				cfg.HTTPClient = s.Client()

				ft := h.Targets.(fakeTargets)
				ft.target.Backend = kodi.NewBackend(kodi.NewClient(cfg, nil), kodi.BackendOptions{Location: time.UTC})
				h.Targets = ft
			},
			speech: "Lounge isn't answering. Try again in a moment.",
			target: "Lounge",
		},
		"a backend that ignores the deadline": {
			stall: func(t *testing.T, h *Handler) {
				b := &stalledBackend{Backend: h.Targets.(fakeTargets).target.Backend, release: make(chan struct{})}
				t.Cleanup(func() { close(b.release) })

				ft := h.Targets.(fakeTargets)
				ft.target.Backend = b
				h.Targets = ft
			},
			speech: "Lounge isn't answering. Try again in a moment.",
			target: "Lounge",
		},
		"a target lookup that doesn't answer": {
			stall: func(t *testing.T, h *Handler) {
				release := make(chan struct{})
				t.Cleanup(func() { close(release) })

				h.Targets = stalledTargets{Targets: h.Targets, release: release}
			},
			speech: "Sorry, that took too long. Try again in a moment.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h, _, rec := newHandler(t)
				h.Now = time.Now
				tc.stall(t, h)

				start := time.Now()

				var resp ResponseEnvelope
				require.NoError(t, json.Unmarshal(serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-unique/request.json")), &resp))

				assert.Equal(t, RequestDeadline, time.Since(start), "Encore answers at the deadline, inside Alexa's eight seconds")
				require.NotNil(t, resp.Response.OutputSpeech)
				assert.Equal(t, tc.speech, resp.Response.OutputSpeech.Text)
				require.NotNil(t, resp.Response.ShouldEndSession)
				assert.True(t, *resp.Response.ShouldEndSession)

				rec.mu.Lock()
				defer rec.mu.Unlock()

				require.Len(t, rec.entries, 1)
				e := rec.entries[0]
				require.ErrorIs(t, e.Err, ErrTooSlow)
				assert.Equal(t, "error", e.Outcome)
				assert.Equal(t, tc.target, e.Target)
				assert.Equal(t, RequestDeadline, e.Duration)
			})
		})
	}
}

func TestRequestDeadlineCountsVerification(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h, backend, _ := newHandler(t)
		h.Now = time.Now
		b := &stalledBackend{Backend: backend, release: make(chan struct{})}
		t.Cleanup(func() { close(b.release) })

		ft := h.Targets.(fakeTargets)
		ft.target.Backend = b
		h.Targets = ft

		const verify = 2 * time.Second

		body := mustRead(t, "testdata/golden/watch-movie-unique/request.json")
		ctx := context.WithValue(t.Context(), verifyTimeKey{}, verify)
		start := time.Now()

		h.ServeBody(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/alexa", bytes.NewReader(body)), body)

		assert.Equal(t, RequestDeadline-verify, time.Since(start), "the signature check already used part of Alexa's wait")
	})
}

// A request that finishes in time is answered as usual, even when it fails.
func TestRequestDeadlineKeepsTimelyAnswers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h, backend, _ := newHandler(t)
		ft := h.Targets.(fakeTargets)
		ft.target.Backend = &sleepyBackend{Backend: backend}
		h.Targets = ft

		var resp ResponseEnvelope
		require.NoError(t, json.Unmarshal(serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-unique/request.json")), &resp))
		require.NotNil(t, resp.Response.OutputSpeech)
		assert.Equal(t, "I can't reach Lounge right now.", resp.Response.OutputSpeech.Text)
	})
}

// stalledTargets never finds the device's target.
type stalledTargets struct {
	Targets

	release chan struct{}
}

func (s stalledTargets) Target(context.Context, string) (actions.Target, error) {
	<-s.release

	return actions.Target{}, ErrUnlinked
}

// panickingTargets panics looking up the device's target, as a bug in handling a request would.
type panickingTargets struct{ Targets }

func (panickingTargets) Target(context.Context, string) (actions.Target, error) {
	panic("index out of range")
}

// A panic while handling a request answers that request with an error rather than taking Encore down with it.
func TestRequestPanicIsAnswered(t *testing.T) {
	var logs bytes.Buffer

	h, _, rec := newHandler(t)
	h.Targets = panickingTargets{Targets: h.Targets}
	h.Log = slog.New(slog.NewTextHandler(&logs, nil))

	var resp ResponseEnvelope
	require.NoError(t, json.Unmarshal(serveBody(t, h, mustRead(t, "testdata/golden/watch-movie-unique/request.json")), &resp))
	require.NotNil(t, resp.Response.OutputSpeech)
	assert.Equal(t, "Sorry, something went wrong talking to Kodi.", resp.Response.OutputSpeech.Text)

	rec.mu.Lock()
	defer rec.mu.Unlock()

	require.Len(t, rec.entries, 1)
	require.ErrorIs(t, rec.entries[0].Err, ErrPanicked)
	require.ErrorContains(t, rec.entries[0].Err, "index out of range")
	assert.Contains(t, logs.String(), "panicked handling an Alexa request")
	assert.Contains(t, logs.String(), "deadline_test.go", "the log carries the stack")
}
