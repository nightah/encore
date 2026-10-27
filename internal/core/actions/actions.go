package actions

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// Allows reports whether the skill was set up to play kind.
func (t Target) Allows(kind media.Kind) bool {
	c := media.ContentOf(kind)

	return c == "" || !slices.Contains(t.Disabled, c)
}

// Allowed returns the kinds the skill was set up to play, in order.
func (t Target) Allowed(kinds []media.Kind) []media.Kind {
	out := make([]media.Kind, 0, len(kinds))

	for _, k := range kinds {
		if t.Allows(k) {
			out = append(out, k)
		}
	}

	return out
}

// ContentDisabled is the reply to a request for a kind of title the skill was set up without.
func ContentDisabled() Outcome {
	return Outcome{Say: say("content.disabled", nil), EndSession: true}
}

func (t Target) queueLimit() int {
	if t.QueueLimit > 0 {
		return t.QueueLimit
	}

	return DefaultQueueLimit
}

func (t Target) language() string {
	if t.Language != "" {
		return t.Language
	}

	return "en"
}

// NewService returns a Service.
func NewService(log *slog.Logger) *Service {
	return &Service{log: log, now: time.Now, rand: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))} //nolint:gosec // picking something to watch is not security sensitive
}

// SetSeed makes random picks repeatable, for tests.
func (s *Service) SetSeed(seed uint64) {
	s.randMu.Lock()
	defer s.randMu.Unlock()

	s.rand = rand.New(rand.NewPCG(seed, seed)) //nolint:gosec // see NewService
}

func (s *Service) intN(n int) int {
	s.randMu.Lock()
	defer s.randMu.Unlock()

	return s.rand.IntN(n)
}

func (s *Service) shuffle(n int, swap func(i, j int)) {
	s.randMu.Lock()
	defer s.randMu.Unlock()

	s.rand.Shuffle(n, swap)
}

func say(id string, params map[string]string) Message {
	return Message{ID: id, Params: params}
}

func done(msg Message) Outcome {
	return Outcome{Say: msg, EndSession: true}
}

// silent is a successful outcome with nothing to say: the TV is the confirmation, and the adapter may add a sound.
func silent() Outcome { return Outcome{Confirm: true, EndSession: true} }

// keepOpen is a silent success that keeps the session open, so the next command needs no wake word, as when using a remote.
func keepOpen() Outcome { return Outcome{Confirm: true} }

// failed reports a playback error, saying so when nothing is playing.
func failed(t Target, err error) Outcome {
	if errors.Is(err, media.ErrNothingPlaying) {
		return done(say("playback.nothing_playing", map[string]string{"kodi": t.Name}))
	}

	return failure(t, err)
}

func failure(t Target, err error) Outcome {
	o := done(say("error.generic", nil))

	if errors.Is(err, media.ErrUnreachable) {
		o = done(say("error.kodi_unreachable", map[string]string{"kodi": t.Name}))
	}

	o.Err = err

	return o
}

// withWake runs fn, and if the backend was unreachable and t can be woken, wakes it and runs fn again.
func withWake(ctx context.Context, t Target, progress Progress, fn func() error) error {
	err := fn()
	if !errors.Is(err, media.ErrUnreachable) || t.Wake == nil {
		return err
	}

	if progress != nil {
		progress(ctx, say("kodi.waking", map[string]string{"kodi": t.Name}))
	}

	if werr := t.Wake(ctx); werr != nil {
		return errors.Join(err, werr)
	}

	return fn()
}
