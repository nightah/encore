package kodi

import (
	"context"
	"fmt"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// SetShuffle implements media.Backend.
func (b *Backend) SetShuffle(ctx context.Context, on bool) error {
	player, err := b.activePlayer(ctx)
	if err != nil {
		return err
	}

	_, err = b.c.PlayerSetShuffle(ctx, kodirpc.PlayerSetShuffleParams{PlayerID: player, Shuffle: kodirpc.NewGlobalToggleFromBool(on)})

	return err
}

// SetRepeat implements media.Backend.
func (b *Backend) SetRepeat(ctx context.Context, mode media.Repeat) (media.Repeat, error) {
	player, err := b.activePlayer(ctx)
	if err != nil {
		return "", err
	}

	var repeat kodirpc.PlayerSetRepeatRepeat

	switch mode {
	case media.RepeatCycle:
		repeat = kodirpc.NewPlayerSetRepeatRepeatFromString(kodirpc.PlayerSetRepeatRepeatStringCycle)
	case media.RepeatOff, media.RepeatOne, media.RepeatAll:
		repeat = kodirpc.NewPlayerSetRepeatRepeatFromPlayerRepeat(kodirpc.PlayerRepeat(mode))
	default:
		return "", fmt.Errorf("kodi: unknown repeat mode %q", mode)
	}

	if _, err := b.c.PlayerSetRepeat(ctx, kodirpc.PlayerSetRepeatParams{PlayerID: player, Repeat: repeat}); err != nil {
		return "", err
	}

	props, err := b.c.PlayerGetProperties(ctx, kodirpc.PlayerGetPropertiesParams{PlayerID: player, Properties: []kodirpc.PlayerPropertyName{"repeat"}})
	if err != nil {
		return "", err
	}

	return media.Repeat(props.Repeat), nil
}

// Picture implements media.Backend.
func (b *Backend) Picture(ctx context.Context, cmd media.PictureCommand) error {
	player, err := b.activePlayer(ctx)
	if err != nil {
		return err
	}

	zoom := func(z kodirpc.PlayerZoomZoom) error {
		_, err := b.c.PlayerZoom(ctx, kodirpc.PlayerZoomParams{PlayerID: player, Zoom: z})

		return err
	}

	rotate := func(v kodirpc.PlayerRotateValue) error {
		_, err := b.c.PlayerRotate(ctx, kodirpc.PlayerRotateParams{PlayerID: player, Value: v})

		return err
	}

	move := func(d kodirpc.PlayerMoveDirection) error {
		_, err := b.c.PlayerMove(ctx, kodirpc.PlayerMoveParams{PlayerID: player, Direction: d})

		return err
	}

	switch cmd {
	case media.PictureZoomIn:
		return zoom(kodirpc.NewPlayerZoomZoomFromString(kodirpc.PlayerZoomZoomStringIn))
	case media.PictureZoomOut:
		return zoom(kodirpc.NewPlayerZoomZoomFromString(kodirpc.PlayerZoomZoomStringOut))
	case media.PictureZoomReset:
		return zoom(kodirpc.NewPlayerZoomZoomFromInt(1))
	case media.PictureRotateClockwise:
		return rotate(kodirpc.PlayerRotateValueClockwise)
	case media.PictureRotateCounterClockwise:
		return rotate(kodirpc.PlayerRotateValueCounterclockwise)
	case media.PictureMoveUp:
		return move(kodirpc.PlayerMoveDirectionUp)
	case media.PictureMoveDown:
		return move(kodirpc.PlayerMoveDirectionDown)
	case media.PictureMoveLeft:
		return move(kodirpc.PlayerMoveDirectionLeft)
	case media.PictureMoveRight:
		return move(kodirpc.PlayerMoveDirectionRight)
	}

	return fmt.Errorf("kodi: unknown picture command %q", cmd)
}

// Streams implements media.Backend.
func (b *Backend) Streams(ctx context.Context) (media.Streams, error) {
	player, err := b.activePlayer(ctx)
	if err != nil {
		return media.Streams{}, err
	}

	props, err := b.c.PlayerGetProperties(ctx, kodirpc.PlayerGetPropertiesParams{
		PlayerID:   player,
		Properties: []kodirpc.PlayerPropertyName{"currentaudiostream", "audiostreams", "subtitleenabled", "currentsubtitle", "subtitles"},
	})
	if err != nil {
		return media.Streams{}, err
	}

	s := media.Streams{SubtitlesOn: props.Subtitleenabled}

	for _, a := range props.Audiostreams {
		s.Audio = append(s.Audio, media.Stream{Index: a.Index, Name: a.Name, Language: a.Language})
	}

	for _, sub := range props.Subtitles {
		s.Subtitles = append(s.Subtitles, media.Stream{Index: sub.Index, Name: sub.Name, Language: sub.Language})
	}

	// Kodi reports an empty object, not null, when there is no current stream.
	if a := props.Currentaudiostream; a != nil && (a.Name != "" || a.Language != "" || len(s.Audio) > 0) {
		s.CurrentAudio = &media.Stream{Index: a.Index, Name: a.Name, Language: a.Language}
	}

	if sub := props.Currentsubtitle; sub != nil && (sub.Name != "" || sub.Language != "" || len(s.Subtitles) > 0) {
		s.CurrentSubtitle = &media.Stream{Index: sub.Index, Name: sub.Name, Language: sub.Language}
	}

	return s, nil
}

// SetSubtitle implements media.Backend.
func (b *Backend) SetSubtitle(ctx context.Context, c media.StreamChange) error {
	player, err := b.activePlayer(ctx)
	if err != nil {
		return err
	}

	p := kodirpc.PlayerSetSubtitleParams{PlayerID: player}

	switch c.Step {
	case media.StreamOn, media.StreamOff:
		p.Subtitle = kodirpc.NewPlayerSetSubtitleSubtitleFromString(kodirpc.PlayerSetSubtitleSubtitleString(c.Step))
	case media.StreamNext, media.StreamPrevious:
		p.Subtitle, p.Enable = kodirpc.NewPlayerSetSubtitleSubtitleFromString(kodirpc.PlayerSetSubtitleSubtitleString(c.Step)), true
	case "":
		p.Subtitle, p.Enable = kodirpc.NewPlayerSetSubtitleSubtitleFromInt(c.Index), true
	default:
		return fmt.Errorf("kodi: unknown subtitle step %q", c.Step)
	}

	_, err = b.c.PlayerSetSubtitle(ctx, p)

	return err
}

// SetAudioStream implements media.Backend.
func (b *Backend) SetAudioStream(ctx context.Context, c media.StreamChange) error {
	player, err := b.activePlayer(ctx)
	if err != nil {
		return err
	}

	p := kodirpc.PlayerSetAudioStreamParams{PlayerID: player}

	switch c.Step {
	case media.StreamNext, media.StreamPrevious:
		p.Stream = kodirpc.NewPlayerSetAudioStreamStreamFromString(kodirpc.PlayerSetAudioStreamStreamString(c.Step))
	case "":
		p.Stream = kodirpc.NewPlayerSetAudioStreamStreamFromInt(c.Index)
	default:
		return fmt.Errorf("kodi: unknown audio stream step %q", c.Step)
	}

	_, err = b.c.PlayerSetAudioStream(ctx, p)

	return err
}
