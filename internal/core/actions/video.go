package actions

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// RandomMovie plays a random movie, unwatched if there are any, optionally from one genre.
func (s *Service) RandomMovie(ctx context.Context, t Target, genre string, progress Progress) Outcome {
	movies, g, o, ok := s.pool(ctx, t, media.KindMovie, genre)
	if !ok {
		return o
	}

	m := s.pick(preferUnwatched(movies))

	if err := withWake(ctx, t, progress, func() error { return t.Backend.Play(ctx, m.Ref(), media.PlayOptions{}) }); err != nil {
		return failure(t, err)
	}

	msg := say("playback.playing_movie", map[string]string{"title": m.Title})
	if g != "" {
		msg = say("movie.playing_genre", map[string]string{"title": m.Title, "genre": g})
	}

	o = done(msg)
	o.Played = &m

	return o
}

// pool returns every item of kind in a genre (all of them when genre is empty), with the library's name for the genre. When ok is false, o says what was missing.
func (s *Service) pool(ctx context.Context, t Target, kind media.Kind, genre string) (items []media.Item, libraryGenre string, o Outcome, ok bool) {
	items, err := s.items(ctx, t, kind)
	if err != nil {
		return nil, "", failure(t, err), false
	}

	if genre != "" {
		g, found := findGenre(t.language(), items, genre)
		if !found {
			return nil, "", done(say("library.no_genre", map[string]string{"genre": genre})), false
		}

		items, libraryGenre = inGenre(items, g), g
	}

	if len(items) == 0 {
		return nil, "", done(say("library.empty", nil)), false
	}

	return items, libraryGenre, Outcome{}, true
}

// Trailer plays a movie's trailer: the named movie, or the one just recommended when no title is given.
func (s *Service) Trailer(ctx context.Context, t Target, title string, pending []Choice) Outcome {
	var ref media.Ref

	switch {
	case title != "":
		doc, ok, err := s.find(ctx, t, title, media.KindMovie)
		if err != nil {
			return failure(t, err)
		}

		if !ok {
			return notFound(title)
		}

		ref = doc.Ref
	case len(pending) > 0 && pending[0].Ref.Kind == media.KindMovie:
		ref = pending[0].Ref
	default:
		return done(say("movie.which_trailer", nil))
	}

	movie, err := t.Backend.Item(ctx, ref)
	if err != nil {
		return failure(t, err)
	}

	if movie.Trailer == "" {
		return done(say("movie.no_trailer", map[string]string{"title": movie.Title}))
	}

	if err := t.Backend.PlayFile(ctx, movie.Trailer); err != nil {
		return failure(t, err)
	}

	return done(say("movie.playing_trailer", map[string]string{"title": movie.Title}))
}

// NextInCollection plays the next unwatched movie in the collection ("set") the named movie belongs to, in release order. Of several movies sharing the title, the first in a collection is used.
func (s *Service) NextInCollection(ctx context.Context, t Target, title string, progress Progress) Outcome {
	cands, err := s.candidates(ctx, t, title, media.KindMovie)
	if err != nil {
		return failure(t, err)
	}

	if len(cands) == 0 {
		return notFound(title)
	}

	var movie media.Item

	for i, c := range cands[:min(3, len(cands))] {
		it, err := t.Backend.Item(ctx, c.Doc.Ref)
		if err != nil {
			return failure(t, err)
		}

		if i == 0 || it.SetID != 0 && movie.SetID == 0 {
			movie = it
		}
	}

	if movie.SetID == 0 {
		return done(say("movie.no_collection", map[string]string{"title": movie.Title}))
	}

	all, err := s.items(ctx, t, media.KindMovie)
	if err != nil {
		return failure(t, err)
	}

	set := filter(all, func(m media.Item) bool { return m.SetID == movie.SetID })
	slices.SortStableFunc(set, func(a, b media.Item) int { return cmp.Or(cmp.Compare(a.Year, b.Year), cmp.Compare(a.ID, b.ID)) })

	next := slices.IndexFunc(set, media.Item.InProgress)
	if next < 0 {
		next = slices.IndexFunc(set, unwatched)
	}

	if next < 0 {
		return done(say("movie.collection_watched", map[string]string{"collection": movie.SetTitle}))
	}

	return s.playItem(ctx, t, set[next], progress)
}

func (s *Service) show(ctx context.Context, t Target, name string) (match.Doc, *Outcome) {
	doc, ok, err := s.find(ctx, t, name, media.KindTVShow)

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

// Wakes the backend if needed.
func (s *Service) episodes(ctx context.Context, t Target, showID int, progress Progress) ([]media.Item, error) {
	var eps []media.Item

	err := withWake(ctx, t, progress, func() error {
		var err error

		eps, err = t.Backend.Episodes(ctx, showID)

		return err
	})

	return eps, err
}

// Episode plays a specific episode, e.g. season 2 episode 4.
func (s *Service) Episode(ctx context.Context, t Target, show string, season, episode int, progress Progress) Outcome {
	doc, o := s.show(ctx, t, show)
	if o != nil {
		return *o
	}

	eps, err := s.episodes(ctx, t, doc.Ref.ID, progress)
	if err != nil {
		return failure(t, err)
	}

	i := slices.IndexFunc(eps, func(e media.Item) bool { return e.Season == season && e.Episode == episode })
	if i < 0 {
		return done(say("tv.no_episode", map[string]string{"show": doc.Title, "season": strconv.Itoa(season), "episode": strconv.Itoa(episode)}))
	}

	return s.playItem(ctx, t, eps[i], progress)
}

// FirstEpisode plays a show's first episode, ignoring specials.
func (s *Service) FirstEpisode(ctx context.Context, t Target, show string, progress Progress) Outcome {
	doc, o := s.show(ctx, t, show)
	if o != nil {
		return *o
	}

	eps, err := s.episodes(ctx, t, doc.Ref.ID, progress)
	if err != nil {
		return failure(t, err)
	}

	regular := filter(eps, func(e media.Item) bool { return e.Season > 0 })
	if len(regular) == 0 {
		regular = eps
	}

	if len(regular) == 0 {
		return done(say("tv.no_episodes", map[string]string{"show": doc.Title}))
	}

	return s.playItem(ctx, t, regular[0], progress)
}

// LatestEpisode plays a show's most recent episode, ignoring specials.
func (s *Service) LatestEpisode(ctx context.Context, t Target, show string, progress Progress) Outcome {
	doc, o := s.show(ctx, t, show)
	if o != nil {
		return *o
	}

	eps, err := s.episodes(ctx, t, doc.Ref.ID, progress)
	if err != nil {
		return failure(t, err)
	}

	regular := filter(eps, func(e media.Item) bool { return e.Season > 0 })
	if len(regular) == 0 {
		regular = eps
	}

	if len(regular) == 0 {
		return done(say("tv.no_episodes", map[string]string{"show": doc.Title}))
	}

	return s.playItem(ctx, t, regular[len(regular)-1], progress)
}

// ContinueWatching resumes the most recently watched show that still has something left to watch.
func (s *Service) ContinueWatching(ctx context.Context, t Target, progress Progress) Outcome {
	shows, err := s.items(ctx, t, media.KindTVShow)
	if err != nil {
		return failure(t, err)
	}

	watched := filter(shows, func(it media.Item) bool { return !it.LastPlayed.IsZero() })
	slices.SortStableFunc(watched, func(a, b media.Item) int { return b.LastPlayed.Compare(a.LastPlayed) })

	if len(watched) == 0 {
		return done(say("tv.nothing_to_continue", nil))
	}

	for _, show := range watched[:min(lastShows, len(watched))] {
		ep, err := t.Backend.NextEpisode(ctx, show.ID)

		switch {
		case errors.Is(err, media.ErrNotFound):
			continue
		case err != nil:
			return failure(t, err)
		}

		return s.playItem(ctx, t, ep, progress)
	}

	return done(say("playback.all_watched", map[string]string{"show": watched[0].Title}))
}

// RandomEpisode plays any episode of a show.
func (s *Service) RandomEpisode(ctx context.Context, t Target, show string, progress Progress) Outcome {
	doc, o := s.show(ctx, t, show)
	if o != nil {
		return *o
	}

	return s.randomEpisode(ctx, t, doc.Ref.ID, doc.Title, progress)
}

func (s *Service) randomEpisode(ctx context.Context, t Target, showID int, title string, progress Progress) Outcome {
	eps, err := s.episodes(ctx, t, showID, progress)
	if err != nil {
		return failure(t, err)
	}

	if len(eps) == 0 {
		return done(say("tv.no_episodes", map[string]string{"show": title}))
	}

	ep := s.pick(eps)

	if err := t.Backend.Play(ctx, ep.Ref(), media.PlayOptions{}); err != nil {
		return failure(t, err)
	}

	o := done(playingMessage(ep))
	o.Played = &ep

	return o
}

// RandomShow plays the next episode of a random show with something unwatched, optionally from one genre; if everything has been watched, a random episode of a random show.
func (s *Service) RandomShow(ctx context.Context, t Target, genre string, progress Progress) Outcome {
	shows, _, o, ok := s.pool(ctx, t, media.KindTVShow, genre)
	if !ok {
		return o
	}

	show := s.pick(preferUnwatched(shows))

	ep, err := t.Backend.NextEpisode(ctx, show.ID)

	switch {
	case errors.Is(err, media.ErrNotFound):
		return s.randomEpisode(ctx, t, show.ID, show.Title, progress)
	case err != nil:
		return failure(t, err)
	}

	return s.playItem(ctx, t, ep, progress)
}

// ShuffleShow plays a show's episodes in random order.
func (s *Service) ShuffleShow(ctx context.Context, t Target, show string, progress Progress) Outcome {
	doc, o := s.show(ctx, t, show)
	if o != nil {
		return *o
	}

	err := withWake(ctx, t, progress, func() error {
		return t.Backend.Play(ctx, doc.Ref, media.PlayOptions{Shuffle: true, Limit: t.queueLimit()})
	})
	if err != nil {
		return failure(t, err)
	}

	return done(say("playback.shuffling", map[string]string{"title": doc.Title}))
}

// Binge queues every remaining unwatched episode of a show, from the next one on, capped at the queue limit.
func (s *Service) Binge(ctx context.Context, t Target, show string, progress Progress) Outcome {
	doc, o := s.show(ctx, t, show)
	if o != nil {
		return *o
	}

	var next media.Item

	err := withWake(ctx, t, progress, func() error {
		var err error

		next, err = t.Backend.NextEpisode(ctx, doc.Ref.ID)

		return err
	})

	switch {
	case errors.Is(err, media.ErrNotFound):
		return done(say("playback.all_watched", map[string]string{"show": doc.Title}))
	case err != nil:
		return failure(t, err)
	}

	eps, err := t.Backend.Episodes(ctx, doc.Ref.ID)
	if err != nil {
		return failure(t, err)
	}

	start := max(0, slices.IndexFunc(eps, func(e media.Item) bool { return e.ID == next.ID }))

	refs := []media.Ref{next.Ref()}

	for _, e := range eps[start:] {
		if len(refs) == t.queueLimit() {
			break
		}

		if e.ID != next.ID && e.PlayCount == 0 && e.Season > 0 {
			refs = append(refs, e.Ref())
		}
	}

	if err := t.Backend.Queue(ctx, refs); err != nil {
		return failure(t, err)
	}

	o2 := done(say("tv.binge", map[string]string{
		"show":    doc.Title,
		"count":   strconv.Itoa(len(refs)),
		"season":  strconv.Itoa(next.Season),
		"episode": strconv.Itoa(next.Episode),
	}))
	o2.Played = &next

	return o2
}
