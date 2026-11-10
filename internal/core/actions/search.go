package actions

import (
	"context"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// searchKinds are what a library search looks through, in the order a tie between them goes: video before music.
var searchKinds = []media.Kind{media.KindMovie, media.KindTVShow, media.KindMusicVideo, media.KindArtist, media.KindAlbum}

// searchSections are the library views that show each kind's titles.
var searchSections = map[media.Kind]media.Section{
	media.KindMovie:      media.SectionMovies,
	media.KindTVShow:     media.SectionTVShows,
	media.KindMusicVideo: media.SectionMusicVideos,
	media.KindArtist:     media.SectionArtists,
	media.KindAlbum:      media.SectionAlbums,
}

// librarySearch shows the library's titles that match query, of the kind whose title matches it best, in the media centre's own library view. A film and a TV show that both have the title asked for are asked about. kinds narrows the search; empty searches every kind the skill was set up with.
func (s *Service) librarySearch(ctx context.Context, t Target, query string, kinds []media.Kind) Outcome {
	if len(kinds) == 0 {
		kinds = searchKinds
	}

	if kinds = t.Allowed(kinds); len(kinds) == 0 {
		return ContentDisabled()
	}

	res, err := t.Matcher.Search(ctx, match.Query{Text: query, Kinds: kinds, Limit: maxCandidates})
	if err != nil {
		return failure(t, err)
	}

	best, other, ok := bestOfKinds(t.language(), query, res)
	if !ok || res.Decision == match.DecisionNotFound {
		// A word that's only part of titles ("star") may score too low to match any, yet still be in them.
		for _, k := range kinds {
			if t.Library != nil && anyContains(t.Library.Items(k), query) {
				o := s.showMatches(ctx, t, query, k, "")
				o.Match = &res

				return o
			}
		}

		o := done(say("match.not_found", map[string]string{"query": query}))
		o.Match = &res

		return o
	}

	if other != nil {
		choices := []Choice{choiceOf(best), choiceOf(*other)}
		msg := say("search.film_or_show", map[string]string{"title": best.Doc.Title})

		return Outcome{Say: msg, Reprompt: &msg, Pending: choices, Followup: &Followup{Search: &SearchRequest{Query: query}}, Match: &res}
	}

	o := s.showMatches(ctx, t, query, best.Doc.Kind, best.Doc.Title)
	o.Match = &res

	return o
}

// ChooseSearch answers "the film or the TV show?": it shows the library's titles of the kind chosen, as the search would have. An answer that names neither asks again.
func (s *Service) ChooseSearch(ctx context.Context, t Target, a Answer, pending []Choice, search SearchRequest) Outcome {
	c, ok := at(a.Which, pending)
	if !ok && a.Which == "" && a.Title != "" {
		if named := titled(t.language(), a.Title, pending); len(named) == 1 {
			c, ok = named[0], true
		}
	}

	if !ok {
		return Outcome{Say: say("match.which_one", nil), AskAgain: true}
	}

	return s.showMatches(ctx, t, search.Query, c.Ref.Kind, c.Title)
}

// showMatches opens the library view of kind narrowed to the titles containing query, or containing title when none does, as when the query was misheard.
func (s *Service) showMatches(ctx context.Context, t Target, query string, kind media.Kind, title string) Outcome {
	text := query
	if title != "" && t.Library != nil && !anyContains(t.Library.Items(kind), query) {
		text = title
	}

	err := withWake(ctx, t, nil, func() error {
		return t.Backend.ShowView(ctx, media.View{Section: searchSections[kind], Title: text})
	})
	if err != nil {
		return failure(t, err)
	}

	return done(say("addon.searching", map[string]string{"query": query}))
}

// bestOfKinds picks the candidate whose kind a search shows, from candidates the search already limited to the kinds asked for. Candidates that tie on score go to the one whose title is most exactly the query, then to the first kind in searchKinds, so a film beats its soundtrack. other is a TV show that ties with a winning film on both counts, or the reverse, which only asking can settle.
func bestOfKinds(lang, query string, res match.Result) (match.Candidate, *match.Candidate, bool) {
	var tied []match.Candidate

	for _, c := range res.Candidates {
		if len(tied) > 0 && tied[0].Score-c.Score > scoreTie {
			break
		}

		tied = append(tied, c)
	}

	if len(tied) == 0 {
		return match.Candidate{}, nil, false
	}

	// The words as said beat the same words once normalised, which drops articles: "Colony" is the show Colony before the film The Colony.
	exact := func(c match.Candidate) int {
		switch {
		case strings.EqualFold(strings.TrimSpace(c.Doc.Title), strings.TrimSpace(query)):
			return 2
		case match.Normalize(lang, c.Doc.Title) == res.Normalized:
			return 1
		default:
			return 0
		}
	}
	// The search asked only for kinds in searchKinds.
	rank := func(c match.Candidate) int { return slices.Index(searchKinds, c.Doc.Kind) }

	slices.SortStableFunc(tied, func(a, b match.Candidate) int {
		if exact(a) != exact(b) {
			return exact(b) - exact(a)
		}

		return rank(a) - rank(b)
	})

	// Only a title said exactly is worth asking about: "star" tying a Star Wars film with a Star Trek show shows the films.
	best := tied[0]
	if exact(best) == 0 || best.Doc.Kind != media.KindMovie && best.Doc.Kind != media.KindTVShow {
		return best, nil, true
	}

	for _, c := range tied[1:] {
		if exact(c) == exact(best) && c.Doc.Kind != best.Doc.Kind && (c.Doc.Kind == media.KindMovie || c.Doc.Kind == media.KindTVShow) {
			return best, &c, true
		}
	}

	return best, nil, true
}

// anyContains reports whether any item's title contains text, ignoring case, as the media centre's filter matches.
func anyContains(items []media.Item, text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))

	return slices.ContainsFunc(items, func(it media.Item) bool { return strings.Contains(strings.ToLower(it.Title), text) })
}

func choiceOf(c match.Candidate) Choice {
	return Choice{DocID: c.Doc.ID, Ref: c.Doc.Ref, Title: c.Doc.Title, Year: c.Doc.Year}
}
