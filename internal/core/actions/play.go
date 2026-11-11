package actions

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// PlayTitle finds a title and plays it. TV shows play their next unwatched episode.
func (s *Service) PlayTitle(ctx context.Context, t Target, req TitleRequest, progress Progress) Outcome {
	q := match.Query{Text: req.Query, Kinds: req.Kinds, Hints: map[string]float64{}, Resolved: req.Resolved, Quality: t.Quality}

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

// ChooseFirst plays the first pending choice, answering "yes" to a question that offered one. "Yes" to a question offering several chooses none of them, and the adapter asks again.
func (s *Service) ChooseFirst(ctx context.Context, t Target, pending []Choice, progress Progress) Outcome {
	if len(pending) == 0 {
		return done(say("match.cancelled", nil))
	}

	return s.playChoice(ctx, t, pending[0], progress)
}

// Choose answers a "did you mean" question: it plays the title offered at a position ("the second one", "the older one"), or one the question found that has the title said. A position no title holds, or a title several share, asks the question again; any other title is a new search, favouring the titles offered. more are the titles the question found but hasn't offered.
func (s *Service) Choose(ctx context.Context, t Target, a Answer, pending, more []Choice, progress Progress) Outcome {
	if c, ok := at(a.Which, pending); ok {
		return s.playChoice(ctx, t, c, progress)
	}

	all := append(slices.Clone(pending), more...)

	switch {
	case a.Which != "", a.Title == "" && a.Year == 0:
		return Outcome{Say: say("match.which_one", nil), AskAgain: true}
	case a.Title == "":
		if c, ok := fromYear(all, a.Year); ok {
			return s.playChoice(ctx, t, c, progress)
		}

		return Outcome{Say: say("match.which_one", nil), AskAgain: true}
	}

	title := a.Title
	if a.Year > 0 {
		title += " " + strconv.Itoa(a.Year)
	}

	switch named := titled(t.language(), title, all); len(named) {
	case 0:
		if c, ok := yearTitle(title, all); ok {
			return s.playChoice(ctx, t, c, progress)
		}

		return s.PlayTitle(ctx, t, TitleRequest{Query: title, Kinds: kindsOf(all), Pending: pending}, progress)
	case 1:
		return s.playChoice(ctx, t, named[0], progress)
	default:
		return Outcome{Say: say("match.which_one", nil), AskAgain: true}
	}
}

// fromYear returns the only choice released in year; with none or several, the answer chooses nothing.
func fromYear(choices []Choice, year int) (Choice, bool) {
	var found []Choice

	for _, c := range choices {
		if c.Year == year {
			found = append(found, c)
		}
	}

	if len(found) != 1 {
		return Choice{}, false
	}

	return found[0], true
}

// ofKind returns the only choice offered of kind; with none or several, the answer chooses nothing.
func ofKind(offered []Choice, kind media.Kind) (Choice, bool) {
	var found []Choice

	for _, c := range offered {
		if c.Ref.Kind == kind {
			found = append(found, c)
		}
	}

	if len(found) != 1 {
		return Choice{}, false
	}

	return found[0], true
}

// at returns the choice at a position among those offered.
func at(which string, offered []Choice) (Choice, bool) {
	if len(offered) == 0 {
		return Choice{}, false
	}

	switch which {
	case AnswerFirst:
		return offered[0], true
	case AnswerSecond:
		if len(offered) > 1 {
			return offered[1], true
		}
	case AnswerLast:
		return offered[len(offered)-1], true
	case AnswerNewer, AnswerOlder:
		return byYear(offered, which == AnswerNewer)
	case AnswerFilm:
		return ofKind(offered, media.KindMovie)
	case AnswerShow:
		return ofKind(offered, media.KindTVShow)
	}

	return Choice{}, false
}

// byYear returns the newest (or oldest) of several choices, when its year is theirs alone.
func byYear(offered []Choice, newest bool) (Choice, bool) {
	if len(offered) < 2 {
		return Choice{}, false
	}

	sorted := slices.SortedFunc(slices.Values(offered), func(a, b Choice) int { return cmp.Compare(a.Year, b.Year) })
	if newest {
		slices.Reverse(sorted)
	}

	if sorted[0].Year == 0 || sorted[0].Year == sorted[1].Year || slices.ContainsFunc(offered, func(c Choice) bool { return c.Year == 0 }) {
		return Choice{}, false
	}

	return sorted[0], true
}

// titled returns the choices called title, with or without their year ("dune 2021").
func titled(lang, title string, choices []Choice) []Choice {
	var out, literal []Choice

	for _, c := range choices {
		same, lit := match.SameTitle(lang, title, c.Title, c.Year)
		if same {
			out = append(out, c)
		}

		if lit {
			literal = append(literal, c)
		}
	}

	// An article said picks the titles that start with it, as the matcher does: "the odyssey" is The Odyssey, not Odyssey.
	if len(literal) > 0 {
		return literal
	}

	return out
}

// AnswerTitle plays the one choice of a "did you mean" question that title names, as Choose does for an answer, and reports whether one did. It lets an answer Amazon sent as another intent, such as "the odyssey from 2026" as the next episode of a show, answer the question.
func (s *Service) AnswerTitle(ctx context.Context, t Target, title string, pending, more []Choice, progress Progress) (Outcome, bool) {
	if len(pending) == 0 || title == "" {
		return Outcome{}, false
	}

	all := append(slices.Clone(pending), more...)

	named := titled(t.language(), title, all)
	if len(named) == 0 {
		if c, ok := yearTitle(title, all); ok {
			return s.playChoice(ctx, t, c, progress), true
		}
	}

	if len(named) != 1 {
		return Outcome{}, false
	}

	return s.playChoice(ctx, t, named[0], progress), true
}

// yearTitle returns the only choice from the year a title made only of one names ("1984" for "die uit 1984", which Amazon heard as a film called 1984). Callers try it once no choice has that title, so a film called 1984 still wins.
func yearTitle(title string, choices []Choice) (Choice, bool) {
	y, err := strconv.Atoi(strings.TrimSpace(title))
	if err != nil || y < minYear || y > maxYear {
		return Choice{}, false
	}

	return fromYear(choices, y)
}

func kindsOf(choices []Choice) []media.Kind {
	var kinds []media.Kind

	for _, c := range choices {
		if !slices.Contains(kinds, c.Ref.Kind) {
			kinds = append(kinds, c.Ref.Kind)
		}
	}

	return kinds
}

func (s *Service) playChoice(ctx context.Context, t Target, c Choice, progress Progress) Outcome {
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
	// Past the titles asked about, only those the search would have asked about on their own are worth offering after a "no".
	floor := res.AskScore

	var choices []Choice

	for _, c := range res.Candidates {
		if len(choices) == maxCandidates || len(choices) >= maxChoices && c.Score < floor {
			break
		}

		choices = append(choices, Choice{DocID: c.Doc.ID, Ref: c.Doc.Ref, Title: c.Doc.Title, Year: c.Doc.Year})
	}

	if len(choices) == 0 {
		return done(say("match.not_found", map[string]string{"query": res.Normalized}))
	}

	return offer(choices)
}

// OtherChoices answers "no" to a "did you mean" question: it offers the titles the question kept back, or says there are no more.
func (s *Service) OtherChoices(more []Choice) Outcome {
	if len(more) == 0 {
		return done(say("match.no_more", nil))
	}

	return offer(more)
}

// offer asks about the first choices, keeping the rest for "no".
func offer(choices []Choice) Outcome {
	offered := choices[:min(maxChoices, len(choices))]

	var msg Message

	if len(offered) == 1 {
		// A single uncertain candidate becomes a yes/no question.
		msg = describe(Message{ID: "match.confirm"}, "title", offered[0])
	} else {
		msg = describe(describe(Message{ID: "match.did_you_mean"}, "first", offered[0]), "second", offered[1])
	}

	o := Outcome{Say: msg, Reprompt: &msg, Pending: offered}

	if rest := choices[len(offered):]; len(rest) > 0 {
		o.Followup = &Followup{More: rest}
	}

	return o
}

// describe fills msg's placeholder key with a choice named unambiguously enough to choose between remakes: its title, with its year worded in the listener's language when it has one.
func describe(msg Message, key string, c Choice) Message {
	// A title that ends with its year already, as Kodi names a remake ("Avatar: The Last Airbender (2024)"), isn't given it twice.
	if c.Year == 0 || strings.HasSuffix(strings.TrimSpace(c.Title), "("+strconv.Itoa(c.Year)+")") {
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

	if ref.Kind == media.KindChannel {
		return s.tune(ctx, t, media.Channel{ID: ref.ID, Name: doc.Title}, progress)
	}

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
