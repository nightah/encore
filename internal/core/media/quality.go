package media

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// QualityOf classifies a video by its frame size. Width decides for wide films (3840×1600 is 2160p) and height for narrow ones (1440×1080 is 1080p); an unknown size is empty.
func QualityOf(width, height int) Quality {
	switch {
	case width <= 0 && height <= 0:
		return ""
	case width >= 3200 || height >= 1800:
		return Quality2160p
	case width >= 1600 || height >= 900:
		return Quality1080p
	case width >= 1100 || height >= 650:
		return Quality720p
	}

	return QualitySD
}

// IsPreference reports whether q can be asked for: a quality, QualityHighest, QualityScreen, or empty for the best.
func (q Quality) IsPreference() bool {
	return q == "" || q == QualityHighest || q == QualityScreen || slices.Contains(Qualities, q)
}

// Quality is the quality that fills s, or empty when s isn't known.
func (s Screen) Quality() Quality { return QualityOf(s.Width, s.Height) }

// ForScreen resolves QualityScreen to the quality s can show, or to the best copy when s isn't known; any other preference stands.
func (q Quality) ForScreen(s Screen) Quality {
	if q != QualityScreen {
		return q
	}

	return cmp.Or(s.Quality(), QualityHighest)
}

// Copy is what choosing between copies of i looks at.
func (i Item) Copy() Copy { return Copy{Quality: i.Quality, Resume: i.Resume} }

// BestCopy returns the index of the copy to play, or -1 when there are none: the one closest to prefer, which is prefer itself, else the best below it, else the least above it. Copies of unknown quality come last. Between copies of the same quality one part watched wins, then the earliest. Where another copy stopped further along is FurthestResume's to find, so playback can carry on from there in the copy chosen.
func BestCopy(prefer Quality, copies []Copy) int {
	if len(copies) == 0 {
		return -1
	}

	best := 0

	for i := 1; i < len(copies); i++ {
		a, b := copies[i], copies[best]

		switch ra, rb := copyRank(prefer, a.Quality), copyRank(prefer, b.Quality); {
		case ra < rb, ra == rb && a.Resume > 0 && b.Resume == 0:
			best = i
		}
	}

	return best
}

// FurthestResume is the furthest position any copy stopped at, or zero.
func FurthestResume(copies []Copy) time.Duration {
	var d time.Duration
	for _, c := range copies {
		d = max(d, c.Resume)
	}

	return d
}

// copyRank orders qualities for prefer, lowest first.
func copyRank(prefer, q Quality) int {
	r, p, n := q.rank(), prefer.rank(), len(Qualities)

	switch {
	case r == 0:
		return 3 * n
	case p == 0:
		return n - r
	case r <= p:
		return p - r
	}

	return n + r - p
}

// rank is q's place in Qualities counting from 1, or 0 for an unknown quality or QualityHighest.
func (q Quality) rank() int { return slices.Index(Qualities, q) + 1 }

// OneCopyEach keeps one copy of each episode that eps lists more than once, as when a library holds it in both 2160p and 1080p, chosen as BestCopy chooses. The copy kept stands where the episode's first copy stood, counts as watched when any copy was, and has the furthest resume point of any copy. eps is returned as it is when it has no copies.
func OneCopyEach(eps []Item, prefer Quality) []Item {
	type key struct{ show, season, episode int }

	groups := make(map[key][]int, len(eps))
	copies := false

	for i, e := range eps {
		if e.Kind != KindEpisode || e.Episode <= 0 {
			continue
		}

		k := key{e.ShowID, e.Season, e.Episode}
		groups[k] = append(groups[k], i)
		copies = copies || len(groups[k]) > 1
	}

	if !copies {
		return eps
	}

	out := make([]Item, 0, len(eps))

	for i, e := range eps {
		g := groups[key{e.ShowID, e.Season, e.Episode}]
		if e.Kind != KindEpisode || e.Episode <= 0 || len(g) < 2 {
			out = append(out, e)

			continue
		}

		if g[0] != i {
			continue
		}

		cs := make([]Copy, len(g))
		played := 0

		for j, idx := range g {
			cs[j] = eps[idx].Copy()
			played = max(played, eps[idx].PlayCount)
		}

		kept := eps[g[BestCopy(prefer, cs)]]
		kept.PlayCount, kept.Resume = played, FurthestResume(cs)
		out = append(out, kept)
	}

	return out
}

// PreferCopies wraps b so that it lists one copy of each episode, chosen for quality as OneCopyEach chooses, shuffles a show and picks a film's versions from those copies, and starts an item from points when it has a resume point there, as when another copy stopped further along. Films are chosen by the match index, which knows their copies. points may be nil.
func PreferCopies(b Backend, quality Quality, points *ResumePoints) Backend {
	if b == nil {
		return nil
	}

	if points == nil {
		points = &ResumePoints{}
	}

	return copyChooser{Backend: b, quality: quality, points: points}
}

// Set records that ref should start from d; a later, further position wins.
func (p *ResumePoints) Set(ref Ref, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.points == nil {
		p.points = map[Ref]time.Duration{}
	}

	p.points[ref] = max(p.points[ref], d)
}

// Get is where ref should start, or zero.
func (p *ResumePoints) Get(ref Ref) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.points[ref]
}

// Episodes implements Backend.
func (b copyChooser) Episodes(ctx context.Context, showID int) ([]Item, error) {
	eps, err := b.Backend.Episodes(ctx, showID)

	return b.oneCopyEach(eps), err
}

// oneCopyEach is OneCopyEach, remembering where each kept copy should start when another copy stopped further along.
func (b copyChooser) oneCopyEach(eps []Item) []Item {
	own := make(map[Ref]time.Duration, len(eps))
	for _, e := range eps {
		own[e.Ref()] = e.Resume
	}

	kept := OneCopyEach(eps, b.quality)
	for _, e := range kept {
		if e.Resume > own[e.Ref()] {
			b.points.Set(e.Ref(), e.Resume)
		}
	}

	return kept
}

// NextEpisode implements Backend. The backend's answer stands unless the show has copies: then an episode is watched when any copy is, and its preferred copy plays.
func (b copyChooser) NextEpisode(ctx context.Context, showID int) (Item, error) {
	next, err := b.Backend.NextEpisode(ctx, showID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return next, err
	}

	all, lerr := b.Backend.Episodes(ctx, showID)
	if lerr != nil {
		return next, err
	}

	eps := b.oneCopyEach(all)
	if len(eps) == len(all) {
		return next, err
	}

	var first, firstSpecial *Item

	for i := range eps {
		ep := &eps[i]

		switch {
		case ep.PlayCount > 0:
		case ep.InProgress():
			return *ep, nil
		case ep.Season > 0 && first == nil:
			first = ep
		case ep.Season == 0 && firstSpecial == nil:
			firstSpecial = ep
		}
	}

	switch {
	case first != nil:
		return *first, nil
	case firstSpecial != nil:
		return *firstSpecial, nil
	}

	return Item{}, fmt.Errorf("show %d has no unwatched episodes: %w", showID, ErrNotFound)
}

// Play implements Backend.
func (b copyChooser) Play(ctx context.Context, ref Ref, opts PlayOptions) error {
	opts.Quality = b.quality
	if opts.Resume {
		opts.From = max(opts.From, b.points.Get(ref))
	}

	return b.Backend.Play(ctx, ref, opts)
}
