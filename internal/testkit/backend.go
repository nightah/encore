package testkit

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

var _ media.Backend = (*Backend)(nil)

// NewBackend returns a backend holding items.
func NewBackend(items ...media.Item) *Backend {
	return &Backend{items: items, volume: media.VolumeState{Level: 50}, repeat: media.RepeatOff}
}

// Calls returns a description of every call made, e.g. "Play movie/3 resume".
func (b *Backend) Calls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.calls)
}

// SetPlaying sets what NowPlaying reports.
func (b *Backend) SetPlaying(np *media.NowPlaying) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.playing = np
}

func (b *Backend) record(format string, args ...any) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.calls = append(b.calls, fmt.Sprintf(format, args...))

	return b.Err
}

// Info implements media.Backend.
func (b *Backend) Info(context.Context) (media.Info, error) {
	return media.Info{Name: "Fake", Version: "21.3", APIVersion: "13.5.0"}, b.record("Info")
}

// Library implements media.Backend.
func (b *Backend) Library(_ context.Context, kind media.Kind) ([]media.Item, error) {
	if err := b.record("Library %s", kind); err != nil {
		return nil, err
	}

	return b.filter(func(i media.Item) bool { return i.Kind == kind }), nil
}

// Episodes implements media.Backend.
func (b *Backend) Episodes(_ context.Context, showID int) ([]media.Item, error) {
	if err := b.record("Episodes %d", showID); err != nil {
		return nil, err
	}

	eps := b.filter(func(i media.Item) bool { return i.Kind == media.KindEpisode && i.ShowID == showID })
	sort.Slice(eps, func(i, j int) bool {
		if eps[i].Season != eps[j].Season {
			return eps[i].Season < eps[j].Season
		}

		return eps[i].Episode < eps[j].Episode
	})

	return eps, nil
}

// RecentlyAdded implements media.Backend.
func (b *Backend) RecentlyAdded(_ context.Context, kind media.Kind, since time.Time) ([]media.Item, error) {
	if err := b.record("RecentlyAdded %s", kind); err != nil {
		return nil, err
	}

	items := b.filter(func(i media.Item) bool { return i.Kind == kind && i.DateAdded.After(since) })
	sort.Slice(items, func(i, j int) bool { return items[i].DateAdded.After(items[j].DateAdded) })

	return items, nil
}

// Item implements media.Backend.
func (b *Backend) Item(_ context.Context, ref media.Ref) (media.Item, error) {
	if err := b.record("Item %s/%d", ref.Kind, ref.ID); err != nil {
		return media.Item{}, err
	}

	for _, i := range b.filter(func(i media.Item) bool { return i.Ref() == ref }) {
		return i, nil
	}

	return media.Item{}, media.ErrNotFound
}

// NextEpisode implements media.Backend.
func (b *Backend) NextEpisode(ctx context.Context, showID int) (media.Item, error) {
	eps, err := b.Episodes(ctx, showID)
	if err != nil {
		return media.Item{}, err
	}

	for _, e := range eps {
		if e.PlayCount == 0 {
			return e, nil
		}
	}

	return media.Item{}, media.ErrNotFound
}

// Play implements media.Backend.
func (b *Backend) Play(_ context.Context, ref media.Ref, opts media.PlayOptions) error {
	mode := ""
	if opts.Resume {
		mode += " resume"
	}

	if opts.Shuffle {
		mode += " shuffle"
	}

	if opts.Limit > 0 {
		mode += fmt.Sprintf(" limit %d", opts.Limit)
	}

	if err := b.record("Play %s/%d%s", ref.Kind, ref.ID, mode); err != nil {
		return err
	}

	for _, i := range b.filter(func(i media.Item) bool { return i.Ref() == ref }) {
		b.SetPlaying(&media.NowPlaying{Item: i, Duration: i.Runtime})

		return nil
	}

	return media.ErrNotFound
}

// Transport implements media.Backend.
func (b *Backend) Transport(_ context.Context, cmd media.Transport) error {
	if err := b.record("Transport %s", cmd); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.playing == nil {
		return media.ErrNothingPlaying
	}

	switch cmd {
	case media.TransportPause:
		b.playing.Paused = true
	case media.TransportResume:
		b.playing.Paused = false
	case media.TransportStop:
		b.playing = nil
	}

	return nil
}

// Seek implements media.Backend.
func (b *Backend) Seek(_ context.Context, s media.Seek) error {
	desc := string(s.Step)

	switch {
	case s.To != nil:
		desc = "to " + s.To.String()
	case s.By != 0:
		desc = "by " + s.By.String()
	}

	if err := b.record("Seek %s", desc); err != nil {
		return err
	}

	return b.playingErr()
}

// Volume implements media.Backend.
func (b *Backend) Volume(_ context.Context, c media.VolumeChange) (media.VolumeState, error) {
	if err := b.record("Volume %s", describeVolume(c)); err != nil {
		return media.VolumeState{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	switch {
	case c.Set != nil:
		b.volume.Level = *c.Set
	case c.Mute != nil:
		b.volume.Muted = *c.Mute
	case c.ToggleMute:
		b.volume.Muted = !b.volume.Muted
	default:
		b.volume.Level = min(max(b.volume.Level+c.Step, 0), 100)
	}

	return b.volume, nil
}

func describeVolume(c media.VolumeChange) string {
	switch {
	case c.Set != nil:
		return fmt.Sprintf("set %d", *c.Set)
	case c.Mute != nil:
		return fmt.Sprintf("mute %t", *c.Mute)
	case c.ToggleMute:
		return "toggle mute"
	default:
		return fmt.Sprintf("step %+d", c.Step)
	}
}

// NowPlaying implements media.Backend.
func (b *Backend) NowPlaying(context.Context) (*media.NowPlaying, error) {
	if err := b.record("NowPlaying"); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.playing == nil {
		return nil, nil //nolint:nilnil // nil means nothing is playing, as media.Backend documents
	}

	np := *b.playing

	return &np, nil
}

// Subscribe implements media.Backend; the fake never emits events.
func (b *Backend) Subscribe(ctx context.Context, _ func(media.Event)) error {
	<-ctx.Done()

	return ctx.Err()
}

func (b *Backend) filter(keep func(media.Item) bool) []media.Item {
	b.mu.Lock()
	defer b.mu.Unlock()

	var out []media.Item

	for _, i := range b.items {
		if keep(i) {
			out = append(out, i)
		}
	}

	return out
}

// Docs turns library items into match documents with IDs "<kind>/<id>".
func Docs(items ...media.Item) []match.Doc {
	var docs []match.Doc

	for _, i := range items {
		if i.Kind == media.KindEpisode {
			continue
		}

		docs = append(docs, match.Doc{ID: fmt.Sprintf("%s/%d", i.Kind, i.ID), Kind: i.Kind, Ref: i.Ref(), Title: i.Title, Year: i.Year})
	}

	return docs
}

// Search implements actions.Matcher.
func (m *Matcher) Search(_ context.Context, q match.Query) (match.Result, error) {
	text := strings.ToLower(strings.TrimSpace(q.Text))
	res := match.Result{Normalized: text}

	var exact, partial []match.Candidate

	for _, d := range m.Docs {
		if len(q.Kinds) > 0 && !slices.Contains(q.Kinds, d.Kind) {
			continue
		}

		title := strings.ToLower(d.Title)

		switch {
		case title == text:
			exact = append(exact, match.Candidate{Doc: d, Score: 1 + q.Hints[d.ID]})
		case text != "" && strings.Contains(title, text):
			partial = append(partial, match.Candidate{Doc: d, Score: 0.7 + q.Hints[d.ID]})
		}
	}

	byScore := func(c []match.Candidate) {
		sort.SliceStable(c, func(i, j int) bool { return c[i].Score > c[j].Score })
	}

	byScore(exact)
	byScore(partial)

	switch {
	case len(exact) == 1, len(exact) > 1 && exact[0].Score-exact[1].Score >= 0.05:
		res.Candidates, res.Decision = exact, match.DecisionPlay
	case len(exact) > 1:
		res.Candidates, res.Decision = exact, match.DecisionAsk
	case len(partial) > 0:
		res.Candidates, res.Decision = partial, match.DecisionAsk
	}

	if len(res.Candidates) > 1 {
		res.Margin = res.Candidates[0].Score - res.Candidates[1].Score
	}

	return res, nil
}

// Items implements actions.Library.
func (l *Library) Items(kind media.Kind) []media.Item {
	var out []media.Item

	for _, i := range l.Cached {
		if i.Kind == kind {
			out = append(out, i)
		}
	}

	return out
}

// Lookup implements actions.Library.
func (l *Library) Lookup(_ context.Context, kind media.Kind, title string) ([]media.Item, error) {
	var found []media.Item

	for _, i := range l.Hidden {
		if i.Kind == kind && strings.Contains(strings.ToLower(i.Title), strings.ToLower(title)) {
			found = append(found, i)
		}
	}

	l.Matcher.Docs = append(l.Matcher.Docs, Docs(found...)...)

	return found, nil
}
