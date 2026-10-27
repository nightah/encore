package actions

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// Transport sends a playback command. Success is silent: the TV is the confirmation.
func (s *Service) Transport(ctx context.Context, t Target, cmd media.Transport) Outcome {
	err := t.Backend.Transport(ctx, cmd)

	switch {
	case errors.Is(err, media.ErrNothingPlaying):
		return done(say("playback.nothing_playing", map[string]string{"kodi": t.Name}))
	case err != nil:
		return failure(t, err)
	}

	return silent()
}

// SetVolume sets an absolute level. Levels up to 10 are read as Kanzi's 0–10 scale, larger ones as a percentage.
func (s *Service) SetVolume(ctx context.Context, t Target, level int) Outcome {
	if level <= 10 {
		level *= 10
	}

	level = min(max(level, 0), 100)

	state, err := t.Backend.Volume(ctx, media.VolumeChange{Set: &level})
	if err != nil {
		return failure(t, err)
	}

	return done(say("volume.set", map[string]string{"level": strconv.Itoa(state.Level)}))
}

// StepVolume raises or lowers the volume by one step. Success is silent.
func (s *Service) StepVolume(ctx context.Context, t Target, up bool) Outcome {
	step := volumeStep
	if !up {
		step = -step
	}

	if _, err := t.Backend.Volume(ctx, media.VolumeChange{Step: step}); err != nil {
		return failure(t, err)
	}

	return silent()
}

// ToggleMute mutes or unmutes. Success is silent.
func (s *Service) ToggleMute(ctx context.Context, t Target) Outcome {
	if _, err := t.Backend.Volume(ctx, media.VolumeChange{ToggleMute: true}); err != nil {
		return failure(t, err)
	}

	return silent()
}

// NowPlaying describes what is playing and how long is left.
func (s *Service) NowPlaying(ctx context.Context, t Target) Outcome {
	np, err := t.Backend.NowPlaying(ctx)
	if err != nil {
		return failure(t, err)
	}

	if np == nil {
		return done(say("playback.nothing_playing", map[string]string{"kodi": t.Name}))
	}

	item := np.Item
	remaining := map[string]time.Duration{"remaining": np.Remaining()}

	switch item.Kind {
	case media.KindEpisode:
		msg := say("playback.now_episode", map[string]string{
			"show":    item.ShowTitle,
			"season":  strconv.Itoa(item.Season),
			"episode": strconv.Itoa(item.Episode),
			"title":   item.Title,
		})
		msg.Durations = remaining

		return done(msg)
	case media.KindMovie:
		msg := say("playback.now_movie", map[string]string{"title": item.Title})
		msg.Durations = remaining

		return done(msg)
	default:
		return done(say("playback.now_other", map[string]string{"title": item.Title}))
	}
}

// NextEpisode plays the next unwatched episode of a show named show.
func (s *Service) NextEpisode(ctx context.Context, t Target, show string, pending []Choice, progress Progress) Outcome {
	return s.PlayTitle(ctx, t, TitleRequest{Query: show, Kinds: []media.Kind{media.KindTVShow}, Pending: pending}, progress)
}
