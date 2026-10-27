package kodi

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

func ptr[T any](v T) *T { return &v }

// Play implements media.Backend. A TV show plays its next episode. With opts.Shuffle, shows, artists, albums and playlists are expanded, shuffled and capped here and queued in that order.
func (b *Backend) Play(ctx context.Context, ref media.Ref, opts media.PlayOptions) error {
	id := kodirpc.LibraryID(ref.ID)
	resume := &kodirpc.PlayerOpenOptions{Resume: kodirpc.NewPlayerOpenOptionsResumeFromBool(opts.Resume)}

	if opts.Shuffle {
		switch ref.Kind {
		case media.KindTVShow, media.KindArtist, media.KindAlbum, media.KindPlaylist:
			return b.playShuffled(ctx, ref, opts)
		}
	}

	switch ref.Kind {
	case media.KindMovie:
		return b.open(ctx, kodirpc.NewPlaylistItemFromMovieID(kodirpc.PlaylistItemMovieID{MovieID: id}), resume)
	case media.KindEpisode:
		return b.open(ctx, kodirpc.NewPlaylistItemFromEpisodeID(kodirpc.PlaylistItemEpisodeID{EpisodeID: id}), resume)
	case media.KindMusicVideo:
		return b.open(ctx, kodirpc.NewPlaylistItemFromMusicvideoID(kodirpc.PlaylistItemMusicvideoID{MusicvideoID: id}), resume)
	case media.KindSong:
		return b.open(ctx, kodirpc.NewPlaylistItemFromSongID(kodirpc.PlaylistItemSongID{SongID: id}), nil)
	case media.KindAlbum:
		return b.open(ctx, kodirpc.NewPlaylistItemFromAlbumID(kodirpc.PlaylistItemAlbumID{AlbumID: id}), nil)
	case media.KindArtist:
		return b.open(ctx, kodirpc.NewPlaylistItemFromArtistID(kodirpc.PlaylistItemArtistID{ArtistID: id}), nil)
	case media.KindPlaylist:
		pl, err := b.playlistFile(ctx, ref.ID)
		if err != nil {
			return err
		}

		return b.open(ctx, kodirpc.NewPlaylistItemFromFile(kodirpc.PlaylistItemFile{File: pl.path}), nil)
	case media.KindTVShow:
		ep, err := b.NextEpisode(ctx, ref.ID)
		if err != nil {
			return err
		}

		return b.Play(ctx, ep.Ref(), media.PlayOptions{Resume: opts.Resume})
	}

	return fmt.Errorf("kodi: can't play %q: %w", ref.Kind, errUnsupportedKind)
}

func (b *Backend) open(ctx context.Context, item kodirpc.PlaylistItem, opts *kodirpc.PlayerOpenOptions) error {
	_, err := b.c.PlayerOpen(ctx, kodirpc.PlayerOpenParams{Item: kodirpc.NewPlayerOpenItemFromPlaylistItem(item), Options: opts})

	return err
}

// Prefers video, then audio, then anything else.
func (b *Backend) activePlayer(ctx context.Context) (kodirpc.PlayerID, error) {
	players, err := b.c.PlayerGetActivePlayers(ctx)
	if err != nil {
		return 0, err
	}

	if len(players) == 0 {
		return 0, media.ErrNothingPlaying
	}

	for _, want := range []kodirpc.PlayerType{kodirpc.PlayerTypeVideo, kodirpc.PlayerTypeAudio} {
		for _, p := range players {
			if p.Type == want {
				return p.PlayerID, nil
			}
		}
	}

	return players[0].PlayerID, nil
}

// Transport implements media.Backend.
func (b *Backend) Transport(ctx context.Context, cmd media.Transport) error {
	player, err := b.activePlayer(ctx)
	if err != nil {
		return err
	}

	switch cmd {
	case media.TransportPause, media.TransportResume:
		props, err := b.c.PlayerGetProperties(ctx, kodirpc.PlayerGetPropertiesParams{PlayerID: player, Properties: []kodirpc.PlayerPropertyName{"speed"}})
		if err != nil {
			return err
		}

		play := cmd == media.TransportResume
		if (props.Speed != 0) == play {
			return nil
		}

		_, err = b.c.PlayerPlayPause(ctx, kodirpc.PlayerPlayPauseParams{PlayerID: player, Play: kodirpc.NewGlobalToggleFromBool(play)})

		return err
	case media.TransportStop:
		_, err = b.c.PlayerStop(ctx, kodirpc.PlayerStopParams{PlayerID: player})
	case media.TransportNext, media.TransportPrevious:
		to := kodirpc.PlayerGoToToStringNext
		if cmd == media.TransportPrevious {
			to = kodirpc.PlayerGoToToStringPrevious
		}

		_, err = b.c.PlayerGoTo(ctx, kodirpc.PlayerGoToParams{PlayerID: player, To: kodirpc.NewPlayerGoToToFromString(to)})
	case media.TransportStartOver:
		err = b.seek(ctx, player, kodirpc.NewPlayerSeekValueFromTime(kodirpc.PlayerSeekValueTime{}))
	default:
		return fmt.Errorf("kodi: unknown transport command %q", cmd)
	}

	return err
}

func (b *Backend) seek(ctx context.Context, player kodirpc.PlayerID, v kodirpc.PlayerSeekValue) error {
	_, err := b.c.PlayerSeek(ctx, kodirpc.PlayerSeekParams{PlayerID: player, Value: v})

	return err
}

var seekSteps = map[media.SeekStep]kodirpc.PlayerSeekValueStepStep{
	media.SeekSmallForward:  kodirpc.PlayerSeekValueStepStepSmallforward,
	media.SeekSmallBackward: kodirpc.PlayerSeekValueStepStepSmallbackward,
	media.SeekBigForward:    kodirpc.PlayerSeekValueStepStepBigforward,
	media.SeekBigBackward:   kodirpc.PlayerSeekValueStepStepBigbackward,
}

// Seek implements media.Backend.
func (b *Backend) Seek(ctx context.Context, s media.Seek) error {
	var v kodirpc.PlayerSeekValue

	switch {
	case s.To != nil:
		v = kodirpc.NewPlayerSeekValueFromTime(kodirpc.PlayerSeekValueTime{Time: positionTime(max(0, *s.To))})
	case s.Step != "":
		step, ok := seekSteps[s.Step]
		if !ok {
			return fmt.Errorf("kodi: unknown seek step %q", s.Step)
		}

		v = kodirpc.NewPlayerSeekValueFromStep(kodirpc.PlayerSeekValueStep{Step: step})
	case s.By != 0:
		v = kodirpc.NewPlayerSeekValueFromSeconds(kodirpc.PlayerSeekValueSeconds{Seconds: int(math.Round(s.By.Seconds()))})
	default:
		return nil
	}

	player, err := b.activePlayer(ctx)
	if err != nil {
		return err
	}

	return b.seek(ctx, player, v)
}

// Volume implements media.Backend and returns the state after the change.
func (b *Backend) Volume(ctx context.Context, change media.VolumeChange) (media.VolumeState, error) {
	var err error

	switch {
	case change.Set != nil:
		err = b.setVolume(ctx, *change.Set)
	case change.Mute != nil:
		_, err = b.c.ApplicationSetMute(ctx, kodirpc.ApplicationSetMuteParams{Mute: kodirpc.NewGlobalToggleFromBool(*change.Mute)})
	case change.ToggleMute:
		_, err = b.c.ApplicationSetMute(ctx, kodirpc.ApplicationSetMuteParams{Mute: kodirpc.NewGlobalToggleFromString(kodirpc.GlobalToggleStringToggle)})
	case change.Step != 0:
		var cur media.VolumeState
		if cur, err = b.volumeState(ctx); err == nil {
			err = b.setVolume(ctx, cur.Level+change.Step)
		}
	}

	if err != nil {
		return media.VolumeState{}, err
	}

	return b.volumeState(ctx)
}

func (b *Backend) setVolume(ctx context.Context, level int) error {
	_, err := b.c.ApplicationSetVolume(ctx, kodirpc.ApplicationSetVolumeParams{
		Volume: kodirpc.NewApplicationSetVolumeVolumeFromInt(max(0, min(100, level))),
	})

	return err
}

func (b *Backend) volumeState(ctx context.Context) (media.VolumeState, error) {
	props, err := b.c.ApplicationGetProperties(ctx, kodirpc.ApplicationGetPropertiesParams{
		Properties: []kodirpc.ApplicationPropertyName{"volume", "muted"},
	})
	if err != nil {
		return media.VolumeState{}, err
	}

	return media.VolumeState{Level: props.Volume, Muted: props.Muted}, nil
}

// NowPlaying implements media.Backend.
func (b *Backend) NowPlaying(ctx context.Context) (*media.NowPlaying, error) {
	player, err := b.activePlayer(ctx)
	if err != nil {
		if errors.Is(err, media.ErrNothingPlaying) {
			return nil, nil
		}

		return nil, err
	}

	item, err := b.c.PlayerGetItem(ctx, kodirpc.PlayerGetItemParams{PlayerID: player, Properties: playingFields})
	if err != nil {
		return nil, err
	}

	props, err := b.c.PlayerGetProperties(ctx, kodirpc.PlayerGetPropertiesParams{
		PlayerID:   player,
		Properties: []kodirpc.PlayerPropertyName{"speed", "time", "totaltime"},
	})
	if err != nil {
		return nil, err
	}

	np := &media.NowPlaying{
		Item:     b.m.playing(&item.Item),
		Position: globalDuration(props.Time),
		Duration: globalDuration(props.Totaltime),
		Paused:   props.Speed == 0,
	}

	if np.Duration == 0 {
		np.Duration = np.Item.Runtime
	}

	return np, nil
}
