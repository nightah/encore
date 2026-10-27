package library

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// fakeBackend is an in-memory media.Source that records calls and lets tests push events.
type fakeBackend struct {
	mu      sync.Mutex
	items   map[media.Ref]media.Item
	fail    map[media.Kind]error
	calls   map[string]int
	sinces  []time.Time
	fn      func(media.Event)
	subbed  chan struct{}
	subOnce sync.Once
}

func newFakeBackend(items ...media.Item) *fakeBackend {
	f := &fakeBackend{
		items:  map[media.Ref]media.Item{},
		fail:   map[media.Kind]error{},
		calls:  map[string]int{},
		subbed: make(chan struct{}),
	}

	for i := range items {
		f.items[items[i].Ref()] = items[i]
	}

	return f
}

func (f *fakeBackend) put(items ...media.Item) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range items {
		f.items[items[i].Ref()] = items[i]
	}
}

func (f *fakeBackend) del(ref media.Ref) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.items, ref)
}

func (f *fakeBackend) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls[method]
}

func (f *fakeBackend) record(method string) {
	f.mu.Lock()
	f.calls[method]++
	f.mu.Unlock()
}

func (f *fakeBackend) where(keep func(*media.Item) bool) []media.Item {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []media.Item

	for ref := range f.items {
		it := f.items[ref]
		if keep(&it) {
			out = append(out, it)
		}
	}

	slices.SortFunc(out, func(a, b media.Item) int { return cmp.Compare(a.ID, b.ID) })

	return out
}

func (f *fakeBackend) emit(ev media.Event) {
	f.mu.Lock()
	fn := f.fn
	f.mu.Unlock()

	fn(ev)
}

func (*fakeBackend) Info(context.Context) (media.Info, error) { return media.Info{Name: "fake"}, nil }

func (f *fakeBackend) Library(_ context.Context, kind media.Kind) ([]media.Item, error) {
	f.record("Library")
	f.record("Library:" + string(kind))

	f.mu.Lock()
	err := f.fail[kind]
	f.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return f.where(func(it *media.Item) bool { return it.Kind == kind }), nil
}

func (f *fakeBackend) Episodes(_ context.Context, showID int) ([]media.Item, error) {
	f.record("Episodes")

	eps := f.where(func(it *media.Item) bool { return it.Kind == media.KindEpisode && it.ShowID == showID })
	sortEpisodes(eps)

	return eps, nil
}

func (f *fakeBackend) RecentlyAdded(_ context.Context, kind media.Kind, since time.Time) ([]media.Item, error) {
	f.record("RecentlyAdded")

	f.mu.Lock()
	f.sinces = append(f.sinces, since)
	f.mu.Unlock()

	items := f.where(func(it *media.Item) bool { return it.Kind == kind && it.DateAdded.After(since) })
	slices.SortFunc(items, func(a, b media.Item) int { return b.DateAdded.Compare(a.DateAdded) })

	return items, nil
}

func (f *fakeBackend) Item(_ context.Context, ref media.Ref) (media.Item, error) {
	f.record("Item")

	f.mu.Lock()
	defer f.mu.Unlock()

	it, ok := f.items[ref]
	if !ok {
		return media.Item{}, media.ErrNotFound
	}

	return it, nil
}

func (*fakeBackend) NextEpisode(context.Context, int) (media.Item, error) {
	return media.Item{}, errors.New("unused")
}

func (*fakeBackend) Play(context.Context, media.Ref, media.PlayOptions) error { return nil }
func (*fakeBackend) Transport(context.Context, media.Transport) error         { return nil }
func (*fakeBackend) Seek(context.Context, media.Seek) error                   { return nil }

func (*fakeBackend) Volume(context.Context, media.VolumeChange) (media.VolumeState, error) {
	return media.VolumeState{}, nil
}

func (*fakeBackend) NowPlaying(context.Context) (*media.NowPlaying, error) { return nil, nil }

func (f *fakeBackend) Subscribe(ctx context.Context, fn func(media.Event)) error {
	f.mu.Lock()
	f.fn = fn
	f.mu.Unlock()

	f.subOnce.Do(func() { close(f.subbed) })
	<-ctx.Done()

	return nil
}

// searchingBackend adds server-side title search.
type searchingBackend struct {
	*fakeBackend
}

func (s searchingBackend) Search(_ context.Context, kind media.Kind, title string) ([]media.Item, error) {
	s.record("Search")

	title = strings.ToLower(title)

	return s.where(func(it *media.Item) bool {
		return it.Kind == kind && strings.Contains(strings.ToLower(it.Title), title)
	}), nil
}
