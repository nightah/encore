package testkit

import (
	"context"
	"fmt"
	"slices"

	"github.com/nightah/encore/internal/core/media"
)

// SetChannels gives the backend live TV with these channels; with none, it has no live TV.
func (b *Backend) SetChannels(c ...media.Channel) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.channels, b.liveTV = c, len(c) > 0
}

// LiveTVAvailable implements media.Backend.
func (b *Backend) LiveTVAvailable(context.Context) (bool, error) {
	b.mu.Lock()
	live := b.liveTV
	b.mu.Unlock()

	return live, b.record("LiveTVAvailable")
}

// Channels implements media.Backend; without guide, what's on is left out.
func (b *Backend) Channels(_ context.Context, guide bool) ([]media.Channel, error) {
	if err := b.record("Channels guide=%t", guide); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.liveTV {
		return nil, media.ErrNoLiveTV
	}

	out := slices.Clone(b.channels)
	if !guide {
		for i := range out {
			out[i].Now, out[i].Next = nil, nil
		}
	}

	return out, nil
}

// PlayChannel implements media.Backend; the channel then reports as playing.
func (b *Backend) PlayChannel(_ context.Context, id int) error {
	if err := b.record("PlayChannel %d", id); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	i := slices.IndexFunc(b.channels, func(c media.Channel) bool { return c.ID == id })
	if i < 0 {
		return fmt.Errorf("channel %d: %w", id, media.ErrNotFound)
	}

	c := b.channels[i]
	title := c.Name

	if c.Now != nil {
		title = c.Now.Title
	}

	b.playing = &media.NowPlaying{Item: media.Item{Kind: media.KindChannel, ID: c.ID, Title: title}}

	return nil
}
