package actions

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// find returns the best library match for name among kinds. Unlike PlayTitle it doesn't ask "did you mean": flows built on a lookup take the best guess, as Kanzi did. Titles the index doesn't know yet are looked up live first.
func (s *Service) find(ctx context.Context, t Target, name string, kinds ...media.Kind) (match.Doc, bool, error) {
	cands, err := s.candidates(ctx, t, name, kinds...)
	if err != nil || len(cands) == 0 {
		return match.Doc{}, false, err
	}

	return cands[0].Doc, true, nil
}

// Best first.
func (s *Service) candidates(ctx context.Context, t Target, name string, kinds ...media.Kind) ([]match.Candidate, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}

	q := match.Query{Text: name, Kinds: kinds, Limit: candidateLimit}

	res, err := t.Matcher.Search(ctx, q)
	if err != nil {
		return nil, err
	}

	if res.Decision == match.DecisionNotFound && t.Library != nil && s.lookup(ctx, t, TitleRequest{Query: name, Kinds: kinds}) {
		if res, err = t.Matcher.Search(ctx, q); err != nil {
			return nil, err
		}
	}

	if res.Decision == match.DecisionNotFound {
		return nil, nil
	}

	return res.Candidates, nil
}

// items returns every item of kind, from the catalog when it has them and from the backend otherwise.
func (s *Service) items(ctx context.Context, t Target, kind media.Kind) ([]media.Item, error) {
	if t.Library != nil {
		if items := t.Library.Items(kind); len(items) > 0 {
			return items, nil
		}
	}

	return t.Backend.Library(ctx, kind)
}

func sameName(lang, a, b string) bool {
	na, nb := match.Normalize(lang, a), match.Normalize(lang, b)

	return na != "" && (na == nb || strings.ReplaceAll(na, " ", "") == strings.ReplaceAll(nb, " ", ""))
}

// byArtist reports whether an item's artist field (which may list several, "Queen, David Bowie") names artist.
func byArtist(lang string, item media.Item, artist string) bool {
	if sameName(lang, item.Artist, artist) {
		return true
	}

	for _, part := range strings.FieldsFunc(item.Artist, func(r rune) bool { return r == ',' || r == '/' || r == ';' }) {
		if sameName(lang, part, artist) {
			return true
		}
	}

	return false
}

// genreAliases maps spoken shorthand to the normalised genre names libraries use.
var genreAliases = map[string]string{
	"scifi":   "sciencefiction",
	"sf":      "sciencefiction",
	"kids":    "children",
	"kid":     "children",
	"rnb":     "randb",
	"hiphop":  "hiphop",
	"romcom":  "romance",
	"cartoon": "animation",
	"anime":   "animation",
	"docos":   "documentary",
	"doco":    "documentary",
	"docs":    "documentary",
}

// genreKey reduces a genre name to a comparable key: normalised, without spaces, singular.
func genreKey(lang, g string) string {
	k := strings.ReplaceAll(match.Normalize(lang, g), " ", "")
	if a, ok := genreAliases[k]; ok {
		return a
	}

	switch {
	case strings.HasSuffix(k, "ies") && len(k) > 4:
		k = strings.TrimSuffix(k, "ies") + "y"
	case strings.HasSuffix(k, "s") && len(k) > 3 && !strings.HasSuffix(k, "ss"):
		k = strings.TrimSuffix(k, "s")
	}

	if a, ok := genreAliases[k]; ok {
		return a
	}

	return k
}

// findGenre returns the library's own name for a spoken genre, looking at the genres of items.
func findGenre(lang string, items []media.Item, spoken string) (string, bool) {
	want := genreKey(lang, spoken)
	if want == "" {
		return "", false
	}

	var partial string

	for _, it := range items {
		for _, g := range it.Genres {
			k := genreKey(lang, g)

			switch {
			case k == want:
				return g, true
			case partial == "" && len(want) >= 4 && strings.HasPrefix(k, want):
				partial = g
			}
		}
	}

	return partial, partial != ""
}

// genre is the library's own name, not the spoken one.
func inGenre(items []media.Item, genre string) []media.Item {
	if genre == "" {
		return items
	}

	return filter(items, func(it media.Item) bool {
		return slices.ContainsFunc(it.Genres, func(g string) bool { return strings.EqualFold(g, genre) })
	})
}

func filter(items []media.Item, keep func(media.Item) bool) []media.Item {
	var out []media.Item

	for _, it := range items {
		if keep(it) {
			out = append(out, it)
		}
	}

	return out
}

func unwatched(it media.Item) bool { return it.PlayCount == 0 }

// preferUnwatched returns the unwatched items, or all of them when everything has been watched.
func preferUnwatched(items []media.Item) []media.Item {
	if u := filter(items, unwatched); len(u) > 0 {
		return u
	}

	return items
}

func (s *Service) pick(items []media.Item) media.Item {
	return items[s.intN(len(items))]
}

func (s *Service) shuffledRefs(items []media.Item, limit int) []media.Ref {
	refs := make([]media.Ref, len(items))
	for i := range items {
		refs[i] = items[i].Ref()
	}

	s.shuffle(len(refs), func(i, j int) { refs[i], refs[j] = refs[j], refs[i] })

	return refs[:min(limit, len(refs))]
}

func newestFirst(items []media.Item) []media.Item {
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(a, b media.Item) int { return cmp.Or(b.DateAdded.Compare(a.DateAdded), cmp.Compare(b.ID, a.ID)) })

	return out
}

func titlesOf(items []media.Item) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].Title
	}

	return out
}

func notFound(query string) Outcome {
	return done(say("match.not_found", map[string]string{"query": query}))
}
