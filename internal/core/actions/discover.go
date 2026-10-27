package actions

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/nightah/encore/internal/core/media"
)

// Recommend suggests something unwatched and asks whether to play it. "Yes" plays it (ChooseFirst), "no" suggests another, and for a movie "play the trailer" plays its trailer.
func (s *Service) Recommend(ctx context.Context, t Target, req RecommendRequest, exclude []Choice) Outcome {
	kinds := slices.Clone(req.Kinds)
	s.shuffle(len(kinds), func(i, j int) { kinds[i], kinds[j] = kinds[j], kinds[i] })

	skip := map[media.Ref]bool{}
	for _, c := range exclude {
		skip[c.Ref] = true
	}

	genreMissing := false

	for _, kind := range kinds {
		c, msg, err := s.recommendKind(ctx, t, kind, req.Genre, skip)

		switch {
		case errors.Is(err, errNoGenre):
			genreMissing = true

			continue
		case errors.Is(err, media.ErrNotFound):
			continue
		case err != nil:
			return failure(t, err)
		}

		r := req

		return Outcome{Say: msg, Reprompt: &msg, Pending: []Choice{c}, Followup: &Followup{Recommend: &r}}
	}

	if genreMissing {
		return done(say("library.no_genre", map[string]string{"genre": req.Genre}))
	}

	return done(say("recommend.nothing", nil))
}

func (s *Service) recommendKind(ctx context.Context, t Target, kind media.Kind, genre string, skip map[media.Ref]bool) (Choice, Message, error) {
	poolKind := kind
	if kind == media.KindEpisode {
		poolKind = media.KindTVShow
	}

	items, err := s.items(ctx, t, poolKind)
	if err != nil {
		return Choice{}, Message{}, err
	}

	if genre != "" {
		g, ok := findGenre(t.language(), items, genre)
		if !ok {
			return Choice{}, Message{}, errNoGenre
		}

		items = inGenre(items, g)
	}

	items = filter(items, func(it media.Item) bool { return !skip[it.Ref()] })
	if len(items) == 0 {
		return Choice{}, Message{}, media.ErrNotFound
	}

	it := s.pick(preferUnwatched(items))

	if kind == media.KindEpisode {
		ep, err := t.Backend.NextEpisode(ctx, it.ID)
		if err != nil {
			return Choice{}, Message{}, err
		}

		it = ep
	}

	c := Choice{DocID: fmt.Sprintf("%s/%d", it.Kind, it.ID), Ref: it.Ref(), Title: it.Title, Year: it.Year}

	var msg Message

	switch it.Kind {
	case media.KindMovie:
		msg = say("recommend.movie", map[string]string{"title": it.Title})
	case media.KindTVShow:
		msg = say("recommend.show", map[string]string{"title": it.Title})
	case media.KindEpisode:
		msg = say("recommend.episode", map[string]string{"show": it.ShowTitle, "season": strconv.Itoa(it.Season), "episode": strconv.Itoa(it.Episode)})
	case media.KindArtist:
		msg = say("recommend.artist", map[string]string{"title": it.Title})
	case media.KindAlbum:
		msg = say("recommend.album", map[string]string{"title": it.Title, "artist": it.Artist})
	default:
		msg = say("recommend.song", map[string]string{"title": it.Title, "artist": it.Artist})
	}

	return c, msg, nil
}

// WhatsNew lists recently added titles that haven't been watched.
func (s *Service) WhatsNew(ctx context.Context, t Target, req WhatsNewRequest) Outcome {
	switch {
	case req.Show != "":
		return s.unwatchedEpisodes(ctx, t, req.Show)
	case req.Kind == media.KindTVShow || req.Kind == media.KindEpisode:
		return s.newEpisodes(ctx, t)
	case req.Kind == media.KindAlbum || req.Kind == media.KindArtist || req.Kind == media.KindSong:
		albums, err := s.items(ctx, t, media.KindAlbum)
		if err != nil {
			return failure(t, err)
		}

		recent := newestFirst(albums)
		names := make([]string, 0, whatsNewMax)

		for _, a := range recent[:min(whatsNewMax, len(recent))] {
			names = append(names, a.Title)
		}

		return listed("whatsnew.albums", "whatsnew.no_albums", names, len(recent) > whatsNewMax)
	}

	kind := cmp.Or(req.Kind, media.KindMovie)

	items, _, o, ok := s.pool(ctx, t, kind, req.Genre)
	if !ok {
		return o
	}

	recent := newestFirst(filter(items, unwatched))
	id := "whatsnew.movies"

	if kind == media.KindMusicVideo {
		id = "whatsnew.music_videos"
	}

	return listed(id, "whatsnew.nothing", titlesOf(recent[:min(whatsNewMax, len(recent))]), len(recent) > whatsNewMax)
}

// listed says a list of names with id, or empty when there are none. More adds "and more".
func listed(id, empty string, names []string, more bool) Outcome {
	if len(names) == 0 {
		return done(say(empty, nil))
	}

	if more {
		id += "_more"
	}

	msg := say(id, nil)
	msg.Lists = map[string][]string{"titles": names}

	return done(msg)
}

func (s *Service) unwatchedEpisodes(ctx context.Context, t Target, show string) Outcome {
	doc, o := s.show(ctx, t, show)
	if o != nil {
		return *o
	}

	eps, err := t.Backend.Episodes(ctx, doc.Ref.ID)
	if err != nil {
		return failure(t, err)
	}

	n := len(filter(eps, unwatched))

	switch n {
	case 0:
		return done(say("tv.no_unwatched", map[string]string{"show": doc.Title}))
	case 1:
		return done(say("tv.one_unwatched", map[string]string{"show": doc.Title}))
	default:
		return done(say("tv.unwatched", map[string]string{"show": doc.Title, "count": strconv.Itoa(n)}))
	}
}

func (s *Service) newEpisodes(ctx context.Context, t Target) Outcome {
	eps, err := t.Backend.RecentlyAdded(ctx, media.KindEpisode, s.now().Add(-newEpisodesWindow))
	if err != nil {
		return failure(t, err)
	}

	var (
		shows []string
		count = map[string]int{}
	)

	for _, e := range filter(eps, unwatched) {
		if count[e.ShowTitle] == 0 {
			shows = append(shows, e.ShowTitle)
		}

		count[e.ShowTitle]++
	}

	switch len(shows) {
	case 0:
		return done(say("whatsnew.no_episodes", nil))
	case 1:
		if count[shows[0]] == 1 {
			return done(say("tv.one_new_episode", map[string]string{"show": shows[0]}))
		}

		return done(say("tv.new_episodes", map[string]string{"show": shows[0], "count": strconv.Itoa(count[shows[0]])}))
	}

	return listed("whatsnew.shows", "whatsnew.no_episodes", shows[:min(whatsNewMax, len(shows))], len(shows) > whatsNewMax)
}

// AlbumsBy lists the albums by an artist, oldest first.
func (s *Service) AlbumsBy(ctx context.Context, t Target, artist string) Outcome {
	doc, o := s.artistItem(ctx, t, artist)
	if o != nil {
		return *o
	}

	albums, err := s.items(ctx, t, media.KindAlbum)
	if err != nil {
		return failure(t, err)
	}

	lang := t.language()
	albums = filter(albums, func(it media.Item) bool { return byArtist(lang, it, doc.Title) })
	slices.SortStableFunc(albums, func(a, b media.Item) int { return cmp.Or(cmp.Compare(a.Year, b.Year), cmp.Compare(a.Title, b.Title)) })

	if len(albums) == 0 {
		return done(say("music.no_albums_by", map[string]string{"artist": doc.Title}))
	}

	const most = 10

	id := "music.albums_by"
	if len(albums) > most {
		id += "_more"
	}

	msg := say(id, map[string]string{"artist": doc.Title})
	msg.Lists = map[string][]string{"titles": titlesOf(albums[:min(most, len(albums))])}

	return done(msg)
}
