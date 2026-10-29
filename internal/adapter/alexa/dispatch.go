package alexa

import (
	"context"
	"slices"

	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/media"
)

func (h *Handler) dispatch(ctx context.Context, env *RequestEnvelope, t actions.Target, intent Intent) actions.Outcome {
	locale := env.Request.Locale
	pending := pendingChoices(env.Session.Attributes)
	followup := sessionFollowupOf(env.Session.Attributes)
	sl := slots{intent: intent, locale: locale, speech: h.Speech}
	svc := h.Service

	progress := func(ctx context.Context, msg actions.Message) {
		if h.Progressive == nil {
			return
		}

		if err := h.Progressive.Send(ctx, env, h.Speech.Render(locale, msg)); err != nil {
			h.Log.DebugContext(ctx, "progressive response not sent", "error", err)
		}
	}

	music := func(shuffle bool) actions.MusicRequest {
		return actions.MusicRequest{
			Title:   sl.value("Title"),
			Artist:  sl.value("Artist"),
			Album:   sl.value("Album"),
			Genre:   sl.value("Genre"),
			Shuffle: shuffle,
		}
	}

	if c := h.IntentContent[intent.Name]; c != "" && slices.Contains(t.Disabled, c) {
		return actions.ContentDisabled()
	}

	switch intent.Name {
	// Answers to questions.
	case "AMAZON.YesIntent":
		if followup != nil && followup.Confirm != "" {
			return svc.ConfirmPower(ctx, t, followup.Confirm)
		}

		return svc.ChooseFirst(ctx, t, pending, progress)
	case "AMAZON.NoIntent":
		// Only reached when a recommendation is waiting for an answer: suggest another.
		return svc.Recommend(ctx, t, *followup.Recommend, pending)

	// Playing by name.
	case "WatchMovie":
		return svc.PlayTitle(ctx, t, sl.title("Movie", []media.Kind{media.KindMovie}, pending), progress)
	case "WatchNextEpisode":
		return svc.PlayTitle(ctx, t, sl.title("Show", []media.Kind{media.KindTVShow}, pending), progress)
	case "PlayMedia":
		// Without deep search, a title of unsaid kind is a film or a show.
		kinds := []media.Kind{media.KindMovie, media.KindTVShow}
		if t.DeepSearch {
			kinds = nil
		}

		// With content groups switched off, only what's left is searched.
		if len(t.Disabled) > 0 {
			if kinds == nil {
				kinds = append(kinds, media.KindMovie, media.KindTVShow, media.KindArtist, media.KindAlbum, media.KindSong, media.KindMusicVideo)
			}

			if kinds = t.Allowed(kinds); len(kinds) == 0 {
				return actions.ContentDisabled()
			}
		}

		return svc.PlayTitle(ctx, t, sl.title("Query", kinds, pending), progress)

	// Movies.
	case "WatchRandomMovie":
		return svc.RandomMovie(ctx, t, sl.value("Genre"), progress)
	case "WatchMovieTrailer":
		return svc.Trailer(ctx, t, sl.value("Movie"), pending)
	case "WatchNextInCollection":
		return svc.NextInCollection(ctx, t, sl.value("Movie"), progress)

	// TV.
	case "WatchEpisode":
		season, okS := sl.number("Season")
		episode, okE := sl.number("Episode")

		if !okS || !okE {
			return reprompt(fallbackMessage())
		}

		return svc.Episode(ctx, t, sl.value("Show"), season, episode, progress)
	case "WatchFirstEpisode":
		return svc.FirstEpisode(ctx, t, sl.value("Show"), progress)
	case "WatchLatestEpisode":
		return svc.LatestEpisode(ctx, t, sl.value("Show"), progress)
	case "ContinueWatching":
		return svc.ContinueWatching(ctx, t, progress)
	case "WatchRandomEpisode":
		return svc.RandomEpisode(ctx, t, sl.value("Show"), progress)
	case "WatchRandomShow":
		return svc.RandomShow(ctx, t, sl.value("Genre"), progress)
	case "ShuffleShow":
		return svc.ShuffleShow(ctx, t, sl.value("Show"), progress)
	case "BingeShow":
		return svc.Binge(ctx, t, sl.value("Show"), progress)

	// Music.
	case "ListenToArtist", "ShuffleArtist":
		return svc.Artist(ctx, t, music(intent.Name == "ShuffleArtist"), progress)
	case "ListenToAlbum", "ShuffleAlbum":
		return svc.Album(ctx, t, music(intent.Name == "ShuffleAlbum"), progress)
	case "ListenToLatestAlbum", "ShuffleLatestAlbum":
		return svc.LatestAlbum(ctx, t, music(intent.Name == "ShuffleLatestAlbum"), progress)
	case "ListenToSong":
		req := music(false)
		req.Title = sl.value("Song")

		return svc.Song(ctx, t, req, progress)
	case "ListenToSongOrAlbum":
		return svc.SongOrAlbum(ctx, t, music(false), progress)
	case "ListenToGenre":
		return svc.Genre(ctx, t, sl.value("Genre"), progress)
	case "ListenToRecentSongs":
		return svc.RecentSongs(ctx, t, progress)
	case "PartyMode":
		return svc.PartyMode(ctx, t, progress)
	case "WatchMusicVideo":
		req := music(false)
		req.Title = sl.value("MusicVideo")

		return svc.MusicVideo(ctx, t, req, progress)
	case "WatchRandomMusicVideo":
		return svc.RandomMusicVideo(ctx, t, music(false), progress)
	case "ShuffleMusicVideos":
		return svc.ShuffleMusicVideos(ctx, t, music(true), progress)
	case "PlayPlaylist", "ShufflePlaylist":
		return svc.Playlist(ctx, t, sl.value("Playlist"), intent.Name == "ShufflePlaylist", progress)

	// Recommendations and discovery.
	case "Recommend":
		kinds, ok := recommendKinds[sl.id("MediaType", "MEDIA_TYPE")]
		if !ok {
			kinds = recommendKinds["video"]
		}

		if kinds = t.Allowed(kinds); len(kinds) == 0 {
			return actions.ContentDisabled()
		}

		return svc.Recommend(ctx, t, actions.RecommendRequest{Kinds: kinds, Genre: sl.value("Genre")}, nil)
	case "WhatsNew":
		kind := whatsNewKinds[sl.id("MediaType", "MEDIA_TYPE")]
		if kind == "" {
			// Without a type, new films, else new episodes, else new albums.
			if allowed := t.Allowed([]media.Kind{media.KindMovie, media.KindTVShow, media.KindAlbum}); len(allowed) > 0 {
				kind = allowed[0]
			}
		}

		if (kind != "" && !t.Allows(kind)) || (kind == "" && len(t.Disabled) == len(media.Contents)) {
			return actions.ContentDisabled()
		}

		return svc.WhatsNew(ctx, t, actions.WhatsNewRequest{
			Kind:  kind,
			Genre: sl.value("Genre"),
			Show:  sl.value("Show"),
		})
	case "WhatAlbums":
		return svc.AlbumsBy(ctx, t, sl.value("Artist"))
	}

	return h.dispatchControl(ctx, t, intent, sl)
}

func (h *Handler) dispatchControl(ctx context.Context, t actions.Target, intent Intent, sl slots) actions.Outcome {
	svc := h.Service

	switch intent.Name {
	case "AMAZON.PauseIntent":
		return svc.Transport(ctx, t, media.TransportPause)
	case "AMAZON.ResumeIntent":
		return svc.Transport(ctx, t, media.TransportResume)
	case "AMAZON.StopIntent":
		// Like Kanzi, "stop" stops playback as well as ending the session.
		return svc.Transport(ctx, t, media.TransportStop)
	case "AMAZON.NextIntent":
		return svc.Transport(ctx, t, media.TransportNext)
	case "AMAZON.PreviousIntent":
		return svc.Transport(ctx, t, media.TransportPrevious)
	case "AMAZON.StartOverIntent":
		return svc.Transport(ctx, t, media.TransportStartOver)
	case "RoomControl":
		cmd, ok := transports[sl.id("Command", "TRANSPORT_COMMAND")]
		if !ok {
			return reprompt(fallbackMessage())
		}

		return svc.Transport(ctx, t, cmd)
	case "AMAZON.ShuffleOnIntent":
		return svc.Shuffle(ctx, t, true)
	case "AMAZON.ShuffleOffIntent":
		return svc.Shuffle(ctx, t, false)
	case "AMAZON.LoopOnIntent":
		// Kanzi cycled one, all, off.
		return svc.Repeat(ctx, t, media.RepeatCycle)
	case "AMAZON.LoopOffIntent":
		return svc.Repeat(ctx, t, media.RepeatOff)
	case "Repeat":
		mode := media.Repeat(sl.id("RepeatMode", "REPEAT_MODE"))
		if mode != media.RepeatOne && mode != media.RepeatAll && mode != media.RepeatOff {
			mode = media.RepeatCycle
		}

		return svc.Repeat(ctx, t, mode)
	case "PlayerSeek", "PlayerRewind":
		return svc.Seek(ctx, t, seekRequest(intent.Name == "PlayerRewind", sl))
	case "PlayerSeekTo":
		if d, ok := sl.duration("Position"); ok {
			return svc.Seek(ctx, t, actions.SeekRequest{To: &d})
		}

		if m, ok := sl.number("Minute"); ok {
			d := minutes(m)

			return svc.Seek(ctx, t, actions.SeekRequest{To: &d})
		}

		return reprompt(fallbackMessage())
	case "PlayerZoom":
		zoom := map[string]media.PictureCommand{"in": media.PictureZoomIn, "out": media.PictureZoomOut, "reset": media.PictureZoomReset}

		cmd, ok := zoom[sl.id("Zoom", "ZOOM_ACTION")]
		if !ok {
			cmd = media.PictureZoomIn // "enhance"
		}

		cmds := []media.PictureCommand{cmd}
		if move, ok := moves[sl.id("Direction", "NAV_DIRECTION")]; ok {
			cmds = append(cmds, move)
		}

		return svc.Picture(ctx, t, cmds...)
	case "PlayerRotate":
		if sl.id("Rotation", "ROTATE_DIRECTION") == "counterclockwise" {
			return svc.Picture(ctx, t, media.PictureRotateCounterClockwise)
		}

		return svc.Picture(ctx, t, media.PictureRotateClockwise)
	case "PlayerMove":
		move, ok := moves[sl.id("Direction", "NAV_DIRECTION")]
		if !ok {
			return reprompt(fallbackMessage())
		}

		return svc.Picture(ctx, t, move)
	case "Fullscreen":
		return svc.Toggle(ctx, t, media.InputFullscreen)
	case "StereoscopicMode":
		return svc.Toggle(ctx, t, media.InputStereoscopic)
	case "AudioPassthrough":
		return svc.Toggle(ctx, t, media.InputPassthrough)
	case "VolumeSet":
		level, ok := sl.number("Level")
		if !ok {
			return reprompt(fallbackMessage())
		}

		return svc.SetVolume(ctx, t, level)
	case "VolumeUp":
		return svc.StepVolume(ctx, t, true)
	case "VolumeDown":
		return svc.StepVolume(ctx, t, false)
	case "Mute":
		return svc.ToggleMute(ctx, t)
	case "CurrentPlayItemEnquiry":
		return svc.NowPlaying(ctx, t)
	case "CurrentPlayItemTimeRemaining":
		return svc.TimeRemaining(ctx, t)
	}

	return h.dispatchRemote(ctx, t, intent, sl)
}

func (h *Handler) dispatchRemote(ctx context.Context, t actions.Target, intent Intent, sl slots) actions.Outcome {
	svc := h.Service

	switch intent.Name {
	case "Subtitles":
		action := sl.id("SubtitleAction", "SUBTITLE_ACTION")

		switch {
		case action == "download":
			return svc.DownloadSubtitles(ctx, t)
		case sl.value("Language") != "" && action != "off":
			return svc.Subtitles(ctx, t, actions.StreamRequest{Language: sl.value("Language"), Listener: sl.locale})
		case action == "":
			action = string(media.StreamOn)
		}

		return svc.Subtitles(ctx, t, actions.StreamRequest{Step: media.StreamStep(action)})
	case "AudioStream":
		if lang := sl.value("Language"); lang != "" {
			return svc.AudioStream(ctx, t, actions.StreamRequest{Language: lang, Listener: sl.locale})
		}

		step := media.StreamNext
		if sl.id("Step", "STREAM_STEP") == "previous" {
			step = media.StreamPrevious
		}

		return svc.AudioStream(ctx, t, actions.StreamRequest{Step: step})
	case "Navigate":
		action, ok := navActions[sl.id("NavAction", "NAV_ACTION")]
		if !ok {
			return reprompt(fallbackMessage())
		}

		count, _ := sl.number("Count")

		return svc.Navigate(ctx, t, action, count)
	case "TypeText":
		return svc.SendText(ctx, t, sl.value("Text"))
	case "ViewLibrary":
		section, ok := sections[sl.id("Section", "LIBRARY_SECTION")]
		if !ok {
			section = media.SectionMovies
		}

		return svc.ShowSection(ctx, t, section, sl.value("Genre"))
	case "ViewPlaylist":
		return svc.ShowPlaylist(ctx, t, sl.value("Playlist"))
	case "OpenFavourite":
		return svc.OpenFavourite(ctx, t, sl.value("Favourite"))
	case "LaunchAddon":
		return svc.LaunchAddon(ctx, t, sl.value("Addon"))
	case "GlobalSearch":
		return svc.GlobalSearch(ctx, t, sl.value("Query"))
	case "Power":
		cmd, ok := powerCommands[sl.id("PowerAction", "POWER_ACTION")]
		if !ok {
			return reprompt(fallbackMessage())
		}

		return svc.Power(ctx, t, cmd)
	case "EjectMedia":
		return svc.Eject(ctx, t)
	case "UpdateLibrary", "CleanLibrary":
		var dbs []media.Database

		switch sl.id("Library", "LIBRARY_TYPE") {
		case "video":
			dbs = []media.Database{media.DatabaseVideo}
		case "music":
			dbs = []media.Database{media.DatabaseMusic}
		}

		return svc.UpdateLibrary(ctx, t, intent.Name == "CleanLibrary", dbs...)
	}

	return reprompt(fallbackMessage())
}

// seekRequest reads PlayerSeek's slots: a duration if given, else a small or big step, in the spoken direction (back for PlayerRewind).
func seekRequest(rewind bool, sl slots) actions.SeekRequest {
	back := rewind || sl.id("SeekDirection", "SEEK_DIRECTION") == "back"

	if d, ok := sl.duration("Duration"); ok {
		if back {
			d = -d
		}

		return actions.SeekRequest{By: d}
	}

	big := sl.id("Size", "SEEK_SIZE") == "big"

	switch {
	case back && big:
		return actions.SeekRequest{Step: media.SeekBigBackward}
	case back:
		return actions.SeekRequest{Step: media.SeekSmallBackward}
	case big:
		return actions.SeekRequest{Step: media.SeekBigForward}
	default:
		return actions.SeekRequest{Step: media.SeekSmallForward}
	}
}

func reprompt(msg actions.Message) actions.Outcome {
	return actions.Outcome{Say: msg, Reprompt: &msg}
}
