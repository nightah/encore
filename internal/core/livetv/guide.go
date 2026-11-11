package livetv

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// New returns a guide to src's live TV, matching names in lang.
func New(src media.LiveTV, lang string, opts Options) *Guide {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &Guide{src: src, lang: lang, now: opts.Now}
}

// Language is the language names are matched in.
func (g *Guide) Language() string { return g.lang }

// Available reports whether the media centre has live TV ready to watch.
func (g *Guide) Available(ctx context.Context) (bool, error) {
	now := g.now()

	g.mu.Lock()
	ttl := availableRecheck
	if g.available {
		ttl = availableTTL
	}

	if g.availableOK && now.Sub(g.availableAt) < ttl {
		ok := g.available
		g.mu.Unlock()

		return ok, nil
	}
	g.mu.Unlock()

	ok, err := g.src.LiveTVAvailable(ctx)
	if err != nil {
		return false, err
	}

	g.mu.Lock()
	g.available, g.availableAt, g.availableOK = ok, now, true
	g.mu.Unlock()

	return ok, nil
}

// Channels returns the channels in number order, fetching them when the list is missing or stale.
func (g *Guide) Channels(ctx context.Context) ([]media.Channel, error) {
	channels, _, err := g.list(ctx, false)

	return channels, err
}

// Names returns the channels' names, each once, for the voice model to expect.
func (g *Guide) Names(ctx context.Context) ([]string, error) {
	channels, err := g.Channels(ctx)
	if err != nil {
		return nil, err
	}

	var names []string

	seen := map[string]bool{}

	for _, c := range channels {
		key := match.Normalize(g.lang, c.Name)
		if c.Name == "" || seen[key] {
			continue
		}

		seen[key] = true
		names = append(names, c.Name)
	}

	return names, nil
}

// OnNow returns the channels with what the guide has on now and next.
func (g *Guide) OnNow(ctx context.Context) ([]media.Channel, error) {
	now := g.now()

	g.mu.Lock()
	if g.guide != nil && now.Before(g.guideUntil) {
		channels := g.guide
		g.mu.Unlock()

		return channels, nil
	}
	g.mu.Unlock()

	channels, err := g.src.Channels(ctx, true)
	if err != nil {
		return nil, err
	}

	until := now.Add(guideTTL)

	for _, c := range channels {
		if c.Now != nil && c.Now.End.After(now) && c.Now.End.Before(until) {
			until = c.Now.End
		}
	}

	g.mu.Lock()
	if floor := now.Add(guideMin); until.Before(floor) {
		until = floor
	}

	g.guide, g.guideUntil = channels, until
	g.mu.Unlock()

	return channels, nil
}

// ChannelNow returns the channel with id as the guide has it now.
func (g *Guide) ChannelNow(ctx context.Context, id int) (media.Channel, bool, error) {
	channels, err := g.OnNow(ctx)
	if err != nil {
		return media.Channel{}, false, err
	}

	i := slices.IndexFunc(channels, func(c media.Channel) bool { return c.ID == id })
	if i < 0 {
		return media.Channel{}, false, nil
	}

	return channels[i], true, nil
}

// list returns the channels and their index. With stale set, a list older than refetchAfter is fetched again.
func (g *Guide) list(ctx context.Context, stale bool) ([]media.Channel, *match.Index, error) {
	fresh := func(now time.Time) bool {
		age := now.Sub(g.loaded)

		return g.index != nil && age < channelTTL && (!stale || age < refetchAfter)
	}

	g.mu.Lock()
	if fresh(g.now()) {
		defer g.mu.Unlock()

		return g.channels, g.index, nil
	}
	g.mu.Unlock()

	g.load.Lock()
	defer g.load.Unlock()

	// Another request may have fetched it while this one waited.
	g.mu.Lock()
	if g.index != nil && g.now().Sub(g.loaded) < refetchAfter {
		defer g.mu.Unlock()

		return g.channels, g.index, nil
	}
	g.mu.Unlock()

	channels, err := g.src.Channels(ctx, false)
	if err != nil {
		return nil, nil, err
	}

	if len(channels) == 0 {
		return nil, nil, ErrNoChannels
	}

	ix, err := g.build(channels)
	if err != nil {
		return nil, nil, err
	}

	// The index replaced isn't closed: a search may still be running on it, and it holds only memory.
	g.mu.Lock()
	g.channels, g.index, g.loaded = channels, ix, g.now()
	g.mu.Unlock()

	return channels, ix, nil
}

func (g *Guide) build(channels []media.Channel) (*match.Index, error) {
	docs := make([]match.Doc, 0, len(channels))

	for _, c := range channels {
		if c.Name == "" {
			continue
		}

		docs = append(docs, match.Doc{
			ID: channelPrefix + strconv.Itoa(c.ID), Kind: media.KindChannel, Ref: media.Ref{Kind: media.KindChannel, ID: c.ID},
			Title: c.Name, Aliases: Aliases(g.lang, c.Name),
		})
	}

	return newIndex(g.lang, docs)
}

// newIndex is a small index of docs, as a guide's channels or the programmes on now and next are.
func newIndex(lang string, docs []match.Doc) (*match.Index, error) {
	ix, err := match.New(lang, match.Config{Retriever: match.RetrieverTrigram})
	if err != nil {
		return nil, err
	}

	if err := ix.Upsert(docs...); err != nil {
		_ = ix.Close()

		return nil, err
	}

	return ix, nil
}

// Forget drops what the guide holds, so the next request asks the media centre again, as after it reconnects.
func (g *Guide) Forget() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.channels, g.index, g.loaded, g.guide, g.availableOK = nil, nil, time.Time{}, nil, false
}

// IsNoLiveTV reports whether err means the media centre has no live TV to offer.
func IsNoLiveTV(err error) bool {
	return errors.Is(err, media.ErrNoLiveTV) || errors.Is(err, ErrNoChannels)
}
