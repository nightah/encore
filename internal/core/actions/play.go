package actions

import (
	"context"
	"errors"
	"strconv"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// PlayTitle finds a title and plays it. TV shows play their next unwatched episode.
func (s *Service) PlayTitle(ctx context.Context, t Target, req TitleRequest, progress Progress) Outcome {
	q := match.Query{Text: req.Query, Kinds: req.Kinds, Hints: map[string]float64{}}

	for id, b := range req.Hints {
		q.Hints[id] = b
	}

	for _, c := range req.Pending {
		q.Hints[c.DocID] += pendingBoost
	}

	res, err := t.Matcher.Search(ctx, q)
	if err != nil {
		return failure(t, err)
	}

	if res.Decision == match.DecisionNotFound && t.Library != nil {
		if found := s.lookup(ctx, t, req); found {
			if res, err = t.Matcher.Search(ctx, q); err != nil {
				return failure(t, err)
			}
		}
	}

	var o Outcome

	switch res.Decision {
	case match.DecisionPlay:
		best, _ := res.Best()
		o = s.playDoc(ctx, t, best.Doc, progress)
	case match.DecisionAsk:
		o = ask(res)
	default:
		o = done(say("match.not_found", map[string]string{"query": req.Query}))
	}

	o.Match = &res

	return o
}

// ChooseFirst plays the first pending choice, answering "yes" to a question.
func (s *Service) ChooseFirst(ctx context.Context, t Target, pending []Choice, progress Progress) Outcome {
	if len(pending) == 0 {
		return done(say("match.cancelled", nil))
	}

	c := pending[0]

	return s.playDoc(ctx, t, match.Doc{ID: c.DocID, Kind: c.Ref.Kind, Ref: c.Ref, Title: c.Title, Year: c.Year}, progress)
}

// lookup asks the library for titles the index may not have yet, reporting whether anything was found.
func (s *Service) lookup(ctx context.Context, t Target, req TitleRequest) bool {
	kinds := req.Kinds
	if len(kinds) == 0 {
		kinds = []media.Kind{media.KindMovie, media.KindTVShow}
	}

	found := false

	for _, kind := range kinds {
		items, err := t.Library.Lookup(ctx, kind, req.Query)
		if err != nil {
			s.log.WarnContext(ctx, "live library lookup failed", "kind", kind, "error", err)

			continue
		}

		found = found || len(items) > 0
	}

	return found
}

func ask(res match.Result) Outcome {
	var choices []Choice

	for _, c := range res.Candidates {
		if len(choices) == maxChoices {
			break
		}

		choices = append(choices, Choice{DocID: c.Doc.ID, Ref: c.Doc.Ref, Title: c.Doc.Title, Year: c.Doc.Year})
	}

	var msg Message

	switch len(choices) {
	case 0:
		return done(say("match.not_found", map[string]string{"query": res.Normalized}))
	case 1:
		// A single uncertain candidate becomes a yes/no question.
		msg = describe(Message{ID: "match.confirm"}, "title", choices[0])
	default:
		msg = describe(describe(Message{ID: "match.did_you_mean"}, "first", choices[0]), "second", choices[1])
	}

	return Outcome{Say: msg, Reprompt: &msg, Pending: choices}
}

// describe fills msg's placeholder key with a choice named unambiguously enough to choose between remakes: its title, with its year worded in the listener's language when it has one.
func describe(msg Message, key string, c Choice) Message {
	if c.Year == 0 {
		if msg.Params == nil {
			msg.Params = map[string]string{}
		}

		msg.Params[key] = c.Title

		return msg
	}

	if msg.Messages == nil {
		msg.Messages = map[string]Message{}
	}

	msg.Messages[key] = say("match.title_year", map[string]string{"title": c.Title, "year": strconv.Itoa(c.Year)})

	return msg
}

func (s *Service) playDoc(ctx context.Context, t Target, doc match.Doc, progress Progress) Outcome {
	ref := doc.Ref

	if ref.Kind == media.KindTVShow {
		var ep media.Item

		err := withWake(ctx, t, progress, func() error {
			var err error

			ep, err = t.Backend.NextEpisode(ctx, ref.ID)

			return err
		})

		switch {
		case errors.Is(err, media.ErrNotFound):
			return done(say("playback.all_watched", map[string]string{"show": doc.Title}))
		case err != nil:
			return failure(t, err)
		}

		return s.playItem(ctx, t, ep, progress)
	}

	item := media.Item{Kind: ref.Kind, ID: ref.ID, Title: doc.Title, Year: doc.Year}

	// The message for an episode, album, song or music video names more than the title.
	switch ref.Kind {
	case media.KindEpisode, media.KindAlbum, media.KindSong, media.KindMusicVideo:
		err := withWake(ctx, t, progress, func() error {
			var err error

			item, err = t.Backend.Item(ctx, ref)

			return err
		})
		if err != nil {
			return failure(t, err)
		}
	}

	return s.playItem(ctx, t, item, progress)
}

// playItem plays one item, resuming where it was left. Artists play shuffled and capped, as Kanzi did.
func (s *Service) playItem(ctx context.Context, t Target, item media.Item, progress Progress) Outcome {
	opts := media.PlayOptions{Resume: true}
	if item.Kind == media.KindArtist {
		opts = media.PlayOptions{Shuffle: true, Limit: t.queueLimit()}
	}

	err := withWake(ctx, t, progress, func() error {
		return t.Backend.Play(ctx, item.Ref(), opts)
	})
	if err != nil {
		return failure(t, err)
	}

	o := done(playingMessage(item))
	o.Played = &item

	return o
}

func playingMessage(item media.Item) Message {
	switch item.Kind {
	case media.KindEpisode:
		return say("playback.playing_episode", map[string]string{
			"show":    item.ShowTitle,
			"season":  strconv.Itoa(item.Season),
			"episode": strconv.Itoa(item.Episode),
			"title":   item.Title,
		})
	case media.KindAlbum:
		return say("music.playing_album", map[string]string{"title": item.Title, "artist": item.Artist})
	case media.KindSong:
		return say("music.playing_song", map[string]string{"title": item.Title, "artist": item.Artist})
	case media.KindMusicVideo:
		return say("music.playing_video", map[string]string{"title": item.Title, "artist": item.Artist})
	case media.KindArtist:
		return say("music.playing_artist", map[string]string{"title": item.Title})
	case media.KindPlaylist:
		return say("playlist.playing", map[string]string{"playlist": item.Title})
	}

	return say("playback.playing_movie", map[string]string{"title": item.Title})
}
