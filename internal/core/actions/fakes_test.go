package actions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

var errBoom = errors.New("boom")

// flaky is the in-memory backend with some methods failing: a method named in fail returns its error instead of running. Failed records the calls that failed.
type flaky struct {
	*testkit.Backend

	fail   map[string]error
	failed []string
}

func (f *flaky) err(method string) error {
	err := f.fail[method]
	if err != nil {
		f.failed = append(f.failed, method)
	}

	return err
}

// failing returns target with its backend wrapped so the named methods fail with err.
func failing(target Target, backend *testkit.Backend, err error, methods ...string) (Target, *flaky) {
	f := &flaky{Backend: backend, fail: map[string]error{}}
	for _, m := range methods {
		f.fail[m] = err
	}

	target.Backend = f

	return target, f
}

func (f *flaky) Library(ctx context.Context, kind media.Kind) ([]media.Item, error) {
	if err := f.err("Library"); err != nil {
		return nil, err
	}

	return f.Backend.Library(ctx, kind)
}

func (f *flaky) Episodes(ctx context.Context, showID int) ([]media.Item, error) {
	if err := f.err("Episodes"); err != nil {
		return nil, err
	}

	return f.Backend.Episodes(ctx, showID)
}

func (f *flaky) RecentlyAdded(ctx context.Context, kind media.Kind, since time.Time) ([]media.Item, error) {
	if err := f.err("RecentlyAdded"); err != nil {
		return nil, err
	}

	return f.Backend.RecentlyAdded(ctx, kind, since)
}

func (f *flaky) Item(ctx context.Context, ref media.Ref) (media.Item, error) {
	if err := f.err("Item"); err != nil {
		return media.Item{}, err
	}

	return f.Backend.Item(ctx, ref)
}

func (f *flaky) NextEpisode(ctx context.Context, showID int) (media.Item, error) {
	if err := f.err("NextEpisode"); err != nil {
		return media.Item{}, err
	}

	return f.Backend.NextEpisode(ctx, showID)
}

func (f *flaky) Play(ctx context.Context, ref media.Ref, opts media.PlayOptions) error {
	if err := f.err("Play"); err != nil {
		return err
	}

	return f.Backend.Play(ctx, ref, opts)
}

func (f *flaky) Queue(ctx context.Context, refs []media.Ref) error {
	if err := f.err("Queue"); err != nil {
		return err
	}

	return f.Backend.Queue(ctx, refs)
}

func (f *flaky) PlayFile(ctx context.Context, path string) error {
	if err := f.err("PlayFile"); err != nil {
		return err
	}

	return f.Backend.PlayFile(ctx, path)
}

func (f *flaky) PartyMode(ctx context.Context) error {
	if err := f.err("PartyMode"); err != nil {
		return err
	}

	return f.Backend.PartyMode(ctx)
}

func (f *flaky) Transport(ctx context.Context, cmd media.Transport) error {
	if err := f.err("Transport"); err != nil {
		return err
	}

	return f.Backend.Transport(ctx, cmd)
}

func (f *flaky) Seek(ctx context.Context, s media.Seek) error {
	if err := f.err("Seek"); err != nil {
		return err
	}

	return f.Backend.Seek(ctx, s)
}

func (f *flaky) Volume(ctx context.Context, c media.VolumeChange) (media.VolumeState, error) {
	if err := f.err("Volume"); err != nil {
		return media.VolumeState{}, err
	}

	return f.Backend.Volume(ctx, c)
}

func (f *flaky) NowPlaying(ctx context.Context) (*media.NowPlaying, error) {
	if err := f.err("NowPlaying"); err != nil {
		return nil, err
	}

	return f.Backend.NowPlaying(ctx)
}

func (f *flaky) Picture(ctx context.Context, cmd media.PictureCommand) error {
	if err := f.err("Picture"); err != nil {
		return err
	}

	return f.Backend.Picture(ctx, cmd)
}

func (f *flaky) Streams(ctx context.Context) (media.Streams, error) {
	if err := f.err("Streams"); err != nil {
		return media.Streams{}, err
	}

	return f.Backend.Streams(ctx)
}

func (f *flaky) SetSubtitle(ctx context.Context, c media.StreamChange) error {
	if err := f.err("SetSubtitle"); err != nil {
		return err
	}

	return f.Backend.SetSubtitle(ctx, c)
}

func (f *flaky) SetAudioStream(ctx context.Context, c media.StreamChange) error {
	if err := f.err("SetAudioStream"); err != nil {
		return err
	}

	return f.Backend.SetAudioStream(ctx, c)
}

func (f *flaky) Input(ctx context.Context, action media.InputAction) error {
	if err := f.err("Input"); err != nil {
		return err
	}

	return f.Backend.Input(ctx, action)
}

func (f *flaky) SendText(ctx context.Context, text string) error {
	if err := f.err("SendText"); err != nil {
		return err
	}

	return f.Backend.SendText(ctx, text)
}

func (f *flaky) ShowView(ctx context.Context, v media.View) error {
	if err := f.err("ShowView"); err != nil {
		return err
	}

	return f.Backend.ShowView(ctx, v)
}

func (f *flaky) Favourites(ctx context.Context) ([]media.Favourite, error) {
	if err := f.err("Favourites"); err != nil {
		return nil, err
	}

	return f.Backend.Favourites(ctx)
}

func (f *flaky) OpenFavourite(ctx context.Context, fav media.Favourite) error {
	if err := f.err("OpenFavourite"); err != nil {
		return err
	}

	return f.Backend.OpenFavourite(ctx, fav)
}

func (f *flaky) Addons(ctx context.Context) ([]media.Addon, error) {
	if err := f.err("Addons"); err != nil {
		return nil, err
	}

	return f.Backend.Addons(ctx)
}

func (f *flaky) ExecuteAddon(ctx context.Context, id string, params map[string]string) error {
	if err := f.err("ExecuteAddon"); err != nil {
		return err
	}

	return f.Backend.ExecuteAddon(ctx, id, params)
}

func (f *flaky) Clock(ctx context.Context) (time.Time, error) {
	if err := f.err("Clock"); err != nil {
		return time.Time{}, err
	}

	return f.Backend.Clock(ctx)
}

func (f *flaky) System(ctx context.Context, cmd media.SystemCommand) error {
	if err := f.err("System"); err != nil {
		return err
	}

	return f.Backend.System(ctx, cmd)
}

func (f *flaky) ScanLibrary(ctx context.Context, db media.Database) error {
	if err := f.err("ScanLibrary"); err != nil {
		return err
	}

	return f.Backend.ScanLibrary(ctx, db)
}

func (f *flaky) CleanLibrary(ctx context.Context, db media.Database) error {
	if err := f.err("CleanLibrary"); err != nil {
		return err
	}

	return f.Backend.CleanLibrary(ctx, db)
}

// brokenMatcher fails every search after the first ok ones, as when the index breaks after a live lookup found something.
type brokenMatcher struct {
	ok       int
	searches int
}

func (m *brokenMatcher) Search(context.Context, match.Query) (match.Result, error) {
	m.searches++
	if m.searches <= m.ok {
		return match.Result{}, nil
	}

	return match.Result{}, errBoom
}

// matcherFunc answers searches with whatever the function returns.
type matcherFunc func(match.Query) match.Result

func (f matcherFunc) Search(_ context.Context, q match.Query) (match.Result, error) { return f(q), nil }

// lookupLibrary is a Library whose live lookup returns found for every kind, or fails, and whose cache holds cached.
type lookupLibrary struct {
	cached []media.Item
	found  []media.Item
	err    error
	kinds  []media.Kind
}

func (l *lookupLibrary) Items(kind media.Kind) []media.Item {
	return filter(l.cached, func(it media.Item) bool { return it.Kind == kind })
}

func (l *lookupLibrary) Lookup(_ context.Context, kind media.Kind, _ string) ([]media.Item, error) {
	l.kinds = append(l.kinds, kind)

	return l.found, l.err
}

// assertFailed checks o is the generic failure caused by errBoom.
func assertFailed(t *testing.T, o Outcome) {
	t.Helper()

	assert.Equal(t, "error.generic", o.Say.ID)
	assert.True(t, o.EndSession)
	require.ErrorIs(t, o.Err, errBoom)
}
