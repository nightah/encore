package actions

import (
	"context"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// Seek jumps within the playing item. Success is silent.
func (s *Service) Seek(ctx context.Context, t Target, req SeekRequest) Outcome {
	seek := media.Seek{Step: req.Step}

	switch {
	case req.To != nil:
		seek = media.Seek{To: req.To}
	case req.By != 0:
		seek = media.Seek{By: req.By}
	case req.Step == "":
		seek.Step = media.SeekSmallForward
	}

	if err := t.Backend.Seek(ctx, seek); err != nil {
		return failed(t, err)
	}

	return silent()
}

// Shuffle turns shuffle on or off.
func (s *Service) Shuffle(ctx context.Context, t Target, on bool) Outcome {
	if err := t.Backend.SetShuffle(ctx, on); err != nil {
		return failed(t, err)
	}

	if on {
		return done(say("player.shuffle_on", nil))
	}

	return done(say("player.shuffle_off", nil))
}

// Repeat sets the repeat mode; media.RepeatCycle steps through off, one and all as Kanzi's "loop on" did.
func (s *Service) Repeat(ctx context.Context, t Target, mode media.Repeat) Outcome {
	now, err := t.Backend.SetRepeat(ctx, mode)
	if err != nil {
		return failed(t, err)
	}

	switch now {
	case media.RepeatOne:
		return done(say("player.repeat_one", nil))
	case media.RepeatAll:
		return done(say("player.repeat_all", nil))
	default:
		return done(say("player.repeat_off", nil))
	}
}

// Picture zooms, rotates or pans the video, keeping the session open for the next adjustment.
func (s *Service) Picture(ctx context.Context, t Target, cmds ...media.PictureCommand) Outcome {
	for _, cmd := range cmds {
		if err := t.Backend.Picture(ctx, cmd); err != nil {
			return failed(t, err)
		}
	}

	return keepOpen()
}

// Toggle flips a display or audio setting such as fullscreen or passthrough. Success is silent.
func (s *Service) Toggle(ctx context.Context, t Target, action media.InputAction) Outcome {
	if err := t.Backend.Input(ctx, action); err != nil {
		return failure(t, err)
	}

	return silent()
}

// TimeRemaining says how long is left and when the item will finish, by the media centre's clock.
func (s *Service) TimeRemaining(ctx context.Context, t Target) Outcome {
	np, err := t.Backend.NowPlaying(ctx)
	if err != nil {
		return failure(t, err)
	}

	if np == nil {
		return done(say("playback.nothing_playing", map[string]string{"kodi": t.Name}))
	}

	left := np.Remaining()
	if left < nearlyOver {
		return done(say("player.nearly_over", nil))
	}

	clock, err := t.Backend.Clock(ctx)
	if err != nil || clock.IsZero() {
		s.log.DebugContext(ctx, "no clock for the finish time", "error", err)

		msg := say("player.remaining", nil)
		msg.Durations = map[string]time.Duration{"remaining": left}

		return done(msg)
	}

	msg := say("player.remaining_until", nil)
	msg.Durations = map[string]time.Duration{"remaining": left}
	msg.Times = map[string]time.Time{"end": clock.Add(left).Round(time.Minute)}

	return done(msg)
}
