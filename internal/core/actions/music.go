package actions

import (
	"cmp"
	"context"
	"slices"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// findBy returns the best match for title among kinds whose catalog entry passes keep. Used for "X by Y" and "X on album Z".
func (s *Service) findBy(ctx context.Context, t Target, title string, keep func(media.Item) bool, kinds ...media.Kind) (media.Item, bool, error) {
	cands, err := s.candidates(ctx, t, title, kinds...)
	if err != nil || len(cands) == 0 {
		return media.Item{}, false, err
	}

	byRef := map[media.Ref]media.Item{}

	for _, kind := range kinds {
		items, err := s.items(ctx, t, kind)
		if err != nil {
			return media.Item{}, false, err
		}

		for _, it := range items {
			byRef[it.Ref()] = it
		}
	}

	for _, c := range cands {
		it, ok := byRef[c.Doc.Ref]
		if !ok {
			// Found live but not cached yet; ask the backend.
			if it, err = t.Backend.Item(ctx, c.Doc.Ref); err != nil {
				continue
			}
		}

		if keep(it) {
			return it, true, nil
		}
	}

	return media.Item{}, false, nil
}

func (s *Service) artistItem(ctx context.Context, t Target, name string) (match.Doc, *Outcome) {
	doc, ok, err := s.find(ctx, t, name, media.KindArtist)

	switch {
	case err != nil:
		o := failure(t, err)

		return doc, &o
	case !ok:
		o := notFound(name)

		return doc, &o
	}

	return doc, nil
}

// Artist plays an artist's music shuffled, as Kanzi did, optionally only one genre of it.
func (s *Service) Artist(ctx context.Context, t Target, req MusicRequest, progress Progress) Outcome {
	doc, o := s.artistItem(ctx, t, req.Artist)
	if o != nil {
		return *o
	}

	if req.Genre == "" {
		err := withWake(ctx, t, progress, func() error {
			return t.Backend.Play(ctx, doc.Ref, media.PlayOptions{Shuffle: true, Limit: t.queueLimit()})
		})
		if err != nil {
			return failure(t, err)
		}

		id := "music.playing_artist"
		if req.Shuffle {
			id = "playback.shuffling"
		}

		return done(say(id, map[string]string{"title": doc.Title}))
	}

	songs, err := s.items(ctx, t, media.KindSong)
	if err != nil {
		return failure(t, err)
	}

	lang := t.language()
	songs = filter(songs, func(it media.Item) bool { return byArtist(lang, it, doc.Title) })

	g, ok := findGenre(lang, songs, req.Genre)
	if !ok {
		return done(say("music.no_genre_by", map[string]string{"genre": req.Genre, "artist": doc.Title}))
	}

	return s.queue(ctx, t, s.shuffledRefs(inGenre(songs, g), t.queueLimit()), progress,
		say("music.playing_genre_by", map[string]string{"genre": g, "artist": doc.Title}))
}

// shuffled returns play options that shuffle, capped at the target's queue limit, or that play in order.
func shuffled(t Target, shuffle bool) media.PlayOptions {
	if !shuffle {
		return media.PlayOptions{}
	}

	return media.PlayOptions{Shuffle: true, Limit: t.queueLimit()}
}

func (s *Service) queue(ctx context.Context, t Target, refs []media.Ref, progress Progress, msg Message) Outcome {
	if len(refs) == 0 {
		return done(say("library.empty", nil))
	}

	if err := withWake(ctx, t, progress, func() error { return t.Backend.Queue(ctx, refs) }); err != nil {
		return failure(t, err)
	}

	return done(msg)
}

// Album plays an album, optionally by a named artist.
func (s *Service) Album(ctx context.Context, t Target, req MusicRequest, progress Progress) Outcome {
	lang := t.language()

	album, ok, err := s.findBy(ctx, t, req.Album, func(it media.Item) bool { return req.Artist == "" || byArtist(lang, it, req.Artist) }, media.KindAlbum)

	switch {
	case err != nil:
		return failure(t, err)
	case !ok && req.Artist != "":
		return done(say("music.not_found_by", map[string]string{"title": req.Album, "artist": req.Artist}))
	case !ok:
		return notFound(req.Album)
	}

	return s.playAlbum(ctx, t, album, req.Shuffle, progress)
}

func (s *Service) playAlbum(ctx context.Context, t Target, album media.Item, shuffle bool, progress Progress) Outcome {
	err := withWake(ctx, t, progress, func() error {
		return t.Backend.Play(ctx, album.Ref(), shuffled(t, shuffle))
	})
	if err != nil {
		return failure(t, err)
	}

	params := map[string]string{"title": album.Title, "artist": album.Artist}

	id := "music.playing_album"
	if shuffle {
		id = "music.shuffling_album"
	}

	o := done(say(id, params))
	o.Played = &album

	return o
}

// LatestAlbum plays an artist's newest album.
func (s *Service) LatestAlbum(ctx context.Context, t Target, req MusicRequest, progress Progress) Outcome {
	doc, o := s.artistItem(ctx, t, req.Artist)
	if o != nil {
		return *o
	}

	albums, err := s.items(ctx, t, media.KindAlbum)
	if err != nil {
		return failure(t, err)
	}

	lang := t.language()
	albums = filter(albums, func(it media.Item) bool { return byArtist(lang, it, doc.Title) })

	if len(albums) == 0 {
		return done(say("music.no_albums_by", map[string]string{"artist": doc.Title}))
	}

	latest := slices.MaxFunc(albums, func(a, b media.Item) int {
		return cmp.Or(cmp.Compare(a.Year, b.Year), a.DateAdded.Compare(b.DateAdded), cmp.Compare(a.ID, b.ID))
	})

	return s.playAlbum(ctx, t, latest, req.Shuffle, progress)
}

// Song plays a song, optionally by a named artist or from a named album.
func (s *Service) Song(ctx context.Context, t Target, req MusicRequest, progress Progress) Outcome {
	song, ok, err := s.song(ctx, t, req)

	switch {
	case err != nil:
		return failure(t, err)
	case !ok && req.Artist != "":
		return done(say("music.not_found_by", map[string]string{"title": req.Title, "artist": req.Artist}))
	case !ok && req.Album != "":
		return done(say("music.not_found_on", map[string]string{"title": req.Title, "album": req.Album}))
	case !ok:
		return notFound(req.Title)
	}

	return s.playSong(ctx, t, song, progress)
}

func (s *Service) song(ctx context.Context, t Target, req MusicRequest) (media.Item, bool, error) {
	lang := t.language()

	return s.findBy(ctx, t, req.Title, func(it media.Item) bool {
		return (req.Artist == "" || byArtist(lang, it, req.Artist)) && (req.Album == "" || sameName(lang, it.Album, req.Album))
	}, media.KindSong)
}

func (s *Service) playSong(ctx context.Context, t Target, song media.Item, progress Progress) Outcome {
	if err := withWake(ctx, t, progress, func() error { return t.Backend.Play(ctx, song.Ref(), media.PlayOptions{}) }); err != nil {
		return failure(t, err)
	}

	o := done(say("music.playing_song", map[string]string{"title": song.Title, "artist": song.Artist}))
	o.Played = &song

	return o
}

// SongOrAlbum plays "X by Y": a song called X by Y, else an album called X by Y.
func (s *Service) SongOrAlbum(ctx context.Context, t Target, req MusicRequest, progress Progress) Outcome {
	song, ok, err := s.song(ctx, t, req)
	if err != nil {
		return failure(t, err)
	}

	if ok {
		return s.playSong(ctx, t, song, progress)
	}

	req.Album = req.Title

	return s.Album(ctx, t, req, progress)
}

// Genre plays a genre's songs shuffled.
func (s *Service) Genre(ctx context.Context, t Target, genre string, progress Progress) Outcome {
	songs, g, o, ok := s.pool(ctx, t, media.KindSong, genre)
	if !ok {
		return o
	}

	return s.queue(ctx, t, s.shuffledRefs(songs, t.queueLimit()), progress, say("music.playing_genre", map[string]string{"genre": g}))
}

// RecentSongs plays the most recently added songs, shuffled.
func (s *Service) RecentSongs(ctx context.Context, t Target, progress Progress) Outcome {
	songs, err := s.items(ctx, t, media.KindSong)
	if err != nil {
		return failure(t, err)
	}

	if len(songs) == 0 {
		return done(say("music.no_recent_songs", nil))
	}

	recent := newestFirst(songs)

	return s.queue(ctx, t, s.shuffledRefs(recent[:min(len(recent), t.queueLimit())], t.queueLimit()), progress, say("music.playing_recent", nil))
}

// PartyMode plays random music from the whole library.
func (s *Service) PartyMode(ctx context.Context, t Target, progress Progress) Outcome {
	if err := withWake(ctx, t, progress, func() error { return t.Backend.PartyMode(ctx) }); err != nil {
		return failure(t, err)
	}

	return done(say("music.party_mode", nil))
}

// MusicVideo plays a music video, optionally by a named artist.
func (s *Service) MusicVideo(ctx context.Context, t Target, req MusicRequest, progress Progress) Outcome {
	lang := t.language()

	video, ok, err := s.findBy(ctx, t, req.Title, func(it media.Item) bool { return req.Artist == "" || byArtist(lang, it, req.Artist) }, media.KindMusicVideo)

	switch {
	case err != nil:
		return failure(t, err)
	case !ok && req.Artist != "":
		return done(say("music.not_found_by", map[string]string{"title": req.Title, "artist": req.Artist}))
	case !ok:
		return notFound(req.Title)
	}

	return s.playItem(ctx, t, video, progress)
}

// Genre and artist are both optional.
func (s *Service) musicVideos(ctx context.Context, t Target, req MusicRequest) ([]media.Item, Outcome, bool) {
	videos, g, o, ok := s.pool(ctx, t, media.KindMusicVideo, req.Genre)
	if !ok {
		return nil, o, false
	}

	if req.Artist != "" {
		lang := t.language()
		videos = filter(videos, func(it media.Item) bool { return byArtist(lang, it, req.Artist) })
	}

	if len(videos) == 0 {
		switch {
		case g != "" && req.Artist != "":
			return nil, done(say("music.no_videos_genre_by", map[string]string{"genre": g, "artist": req.Artist})), false
		case req.Artist != "":
			return nil, done(say("music.no_videos_by", map[string]string{"artist": req.Artist})), false
		default:
			return nil, done(say("library.empty", nil)), false
		}
	}

	return videos, Outcome{}, true
}

// RandomMusicVideo plays a random music video, optionally from a genre or by an artist.
func (s *Service) RandomMusicVideo(ctx context.Context, t Target, req MusicRequest, progress Progress) Outcome {
	videos, o, ok := s.musicVideos(ctx, t, req)
	if !ok {
		return o
	}

	return s.playItem(ctx, t, s.pick(videos), progress)
}

// ShuffleMusicVideos plays music videos in random order, optionally from a genre or by an artist.
func (s *Service) ShuffleMusicVideos(ctx context.Context, t Target, req MusicRequest, progress Progress) Outcome {
	videos, o, ok := s.musicVideos(ctx, t, req)
	if !ok {
		return o
	}

	return s.queue(ctx, t, s.shuffledRefs(videos, t.queueLimit()), progress, say("music.shuffling_videos", nil))
}

// Playlist plays a video or music playlist (smart playlists included), optionally shuffled.
func (s *Service) Playlist(ctx context.Context, t Target, name string, shuffle bool, progress Progress) Outcome {
	doc, ok, err := s.find(ctx, t, name, media.KindPlaylist)

	switch {
	case err != nil:
		return failure(t, err)
	case !ok:
		return done(say("playlist.not_found", map[string]string{"playlist": name}))
	}

	err = withWake(ctx, t, progress, func() error {
		return t.Backend.Play(ctx, doc.Ref, shuffled(t, shuffle))
	})
	if err != nil {
		return failure(t, err)
	}

	id := "playlist.playing"
	if shuffle {
		id = "playlist.shuffling"
	}

	return done(say(id, map[string]string{"playlist": doc.Title}))
}
