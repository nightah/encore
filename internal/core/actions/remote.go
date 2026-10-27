package actions

import (
	"context"
	"errors"
	"strings"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// Navigate presses a remote control button count times and keeps the session open for the next one, so "down", "down", "select" works without the wake word in between.
func (s *Service) Navigate(ctx context.Context, t Target, action media.InputAction, count int) Outcome {
	count = min(max(count, 1), maxPresses)

	for range count {
		if err := t.Backend.Input(ctx, action); err != nil {
			return failure(t, err)
		}
	}

	return keepOpen()
}

// SendText types text into the focused field. Success is silent.
func (s *Service) SendText(ctx context.Context, t Target, text string) Outcome {
	if strings.TrimSpace(text) == "" {
		return done(say("remote.no_text", nil))
	}

	if err := t.Backend.SendText(ctx, text); err != nil {
		return failure(t, err)
	}

	return silent()
}

// sectionKinds are the library kinds whose genres narrow each section.
var sectionKinds = map[media.Section]media.Kind{
	media.SectionMovies:      media.KindMovie,
	media.SectionTVShows:     media.KindTVShow,
	media.SectionMusicVideos: media.KindMusicVideo,
	media.SectionMusic:       media.KindSong,
}

// ShowSection opens a library view, optionally narrowed to a genre, and keeps the session open for navigating it.
func (s *Service) ShowSection(ctx context.Context, t Target, section media.Section, genre string) Outcome {
	v := media.View{Section: section}

	if genre != "" {
		kind, ok := sectionKinds[section]
		if !ok {
			// Albums, artists and recently added views have no genre folders; open the whole view.
			kind = ""
		}

		if kind != "" {
			items, err := s.items(ctx, t, kind)
			if err != nil {
				return failure(t, err)
			}

			g, found := findGenre(t.language(), items, genre)
			if !found {
				return done(say("library.no_genre", map[string]string{"genre": genre}))
			}

			v.Genre = g
		}
	}

	if err := t.Backend.ShowView(ctx, v); err != nil {
		if errors.Is(err, media.ErrNotFound) {
			return done(say("library.no_genre", map[string]string{"genre": genre}))
		}

		return failure(t, err)
	}

	return keepOpen()
}

// ShowPlaylist opens a playlist's view.
func (s *Service) ShowPlaylist(ctx context.Context, t Target, name string) Outcome {
	doc, ok, err := s.find(ctx, t, name, media.KindPlaylist)

	switch {
	case err != nil:
		return failure(t, err)
	case !ok:
		return done(say("playlist.not_found", map[string]string{"playlist": name}))
	}

	if err := t.Backend.ShowView(ctx, media.View{Section: media.SectionPlaylist, Playlist: doc.Ref}); err != nil {
		return failure(t, err)
	}

	return keepOpen()
}

// OpenFavourite opens one of Kodi's favourites by name, or the favourites view when no name is given.
func (s *Service) OpenFavourite(ctx context.Context, t Target, name string) Outcome {
	if strings.TrimSpace(name) == "" {
		return s.ShowSection(ctx, t, media.SectionFavourites, "")
	}

	favs, err := t.Backend.Favourites(ctx)
	if err != nil {
		return failure(t, err)
	}

	titles := make([]string, len(favs))
	for i := range favs {
		titles[i] = favs[i].Title
	}

	i, ok := bestName(t.language(), titles, name)
	if !ok {
		return done(say("favourite.not_found", map[string]string{"favourite": name}))
	}

	if err := t.Backend.OpenFavourite(ctx, favs[i]); err != nil {
		if errors.Is(err, media.ErrUnreachable) {
			return failure(t, err)
		}

		o := done(say("favourite.cant_open", map[string]string{"favourite": favs[i].Title}))
		o.Err = err

		return o
	}

	return done(say("favourite.opening", map[string]string{"favourite": favs[i].Title}))
}

// bestName finds the closest of names to spoken: an exact match, else one that starts with it, else one that contains it or that it contains. It's for short lists such as favourites and add-ons that the title index doesn't hold.
func bestName(lang string, names []string, spoken string) (int, bool) {
	want := match.Normalize(lang, spoken)
	if want == "" {
		return 0, false
	}

	best, bestRank := -1, 0
	squash := func(s string) string { return strings.ReplaceAll(s, " ", "") }

	for i, n := range names {
		have := match.Normalize(lang, n)

		var rank int

		switch {
		case have == "":
			continue
		case have == want:
			rank = 5
		case squash(have) == squash(want):
			rank = 4
		case strings.HasPrefix(have, want):
			rank = 3
		case strings.Contains(have, want):
			rank = 2
		case strings.Contains(want, have):
			rank = 1
		}

		if rank > bestRank {
			best, bestRank = i, rank
		}
	}

	return best, best >= 0
}
