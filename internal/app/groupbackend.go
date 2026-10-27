package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/library"
	"github.com/nightah/encore/internal/core/media"
)

var _ library.Searcher = (*groupBackend)(nil)

func newGroupBackend(members []*Instance, source *Instance, tune tuning, log *slog.Logger) *groupBackend {
	b := &groupBackend{Backend: source.Backend, members: members, source: source, tune: tune, log: log, nudge: make(chan struct{}, 1)}
	b.active.Store(source)

	return b
}

func (b *groupBackend) current() *Instance { return b.active.Load() }

// Library implements media.Backend.
func (b *groupBackend) Library(ctx context.Context, kind media.Kind) ([]media.Item, error) {
	return b.current().Backend.Library(ctx, kind)
}

// Episodes implements media.Backend.
func (b *groupBackend) Episodes(ctx context.Context, showID int) ([]media.Item, error) {
	return b.current().Backend.Episodes(ctx, showID)
}

// RecentlyAdded implements media.Backend.
func (b *groupBackend) RecentlyAdded(ctx context.Context, kind media.Kind, since time.Time) ([]media.Item, error) {
	return b.current().Backend.RecentlyAdded(ctx, kind, since)
}

// Item implements media.Backend.
func (b *groupBackend) Item(ctx context.Context, ref media.Ref) (media.Item, error) {
	return b.current().Backend.Item(ctx, ref)
}

// NextEpisode implements media.Backend.
func (b *groupBackend) NextEpisode(ctx context.Context, showID int) (media.Item, error) {
	return b.current().Backend.NextEpisode(ctx, showID)
}

// Search implements library.Searcher.
func (b *groupBackend) Search(ctx context.Context, kind media.Kind, title string) ([]media.Item, error) {
	return b.current().Backend.Search(ctx, kind, title)
}

// Subscribe implements media.Backend, merging every member's events. Connection events count only from the active member; the others just prompt a check of which member should be active.
func (b *groupBackend) Subscribe(ctx context.Context, fn func(media.Event)) error {
	b.mu.Lock()
	b.emit = fn
	b.mu.Unlock()

	var wg sync.WaitGroup

	for _, m := range b.members {
		wg.Go(func() {
			for {
				err := m.Backend.Subscribe(ctx, func(ev media.Event) { b.forward(m, ev) })
				if ctx.Err() != nil {
					return
				}

				b.log.Warn("kodi subscription ended", "member", m.name, "error", err)

				select {
				case <-ctx.Done():
					return
				case <-time.After(resubscribeDelay):
				}
			}
		})
	}

	wg.Wait()

	return ctx.Err()
}

func (b *groupBackend) forward(m *Instance, ev media.Event) {
	switch ev.Kind {
	case media.EventConnected, media.EventDisconnected:
		b.poke()

		if m != b.current() {
			return
		}
	}

	b.send(ev)
}

func (b *groupBackend) send(ev media.Event) {
	b.mu.Lock()
	fn := b.emit
	b.mu.Unlock()

	if fn != nil {
		fn(ev)
	}
}

func (b *groupBackend) poke() {
	select {
	case b.nudge <- struct{}{}:
	default:
	}
}

func (b *groupBackend) supervise(ctx context.Context) {
	ticker := time.NewTicker(b.tune.failoverInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-b.nudge:
		}

		b.evaluate(ctx)
	}
}

// evaluate picks the member syncs go to: the source when it's reachable, else the current member while it stays reachable, else the first reachable member. With nothing reachable it keeps the current one.
func (b *groupBackend) evaluate(ctx context.Context) {
	cur, want := b.current(), b.current()

	switch {
	case b.reachable(ctx, b.source):
		want = b.source
	case cur != b.source && b.reachable(ctx, cur):
	default:
		for _, m := range b.members {
			if m != b.source && m != cur && b.reachable(ctx, m) {
				want = m

				break
			}
		}
	}

	if want == cur || ctx.Err() != nil {
		return
	}

	b.active.Store(want)

	if want == b.source {
		b.log.Info("library source is back; syncing from it again", "member", want.name)
	} else {
		b.log.Info("library source unreachable; syncing from another member", "source", b.source.name, "member", want.name)
	}

	// The new member's view is synced in full, which a recent sync makes a no-op.
	b.send(media.Event{Kind: media.EventConnected})
}

// reachable reports whether a member answers without being woken: its WebSocket is up, or it answers a ping.
func (b *groupBackend) reachable(ctx context.Context, m *Instance) bool {
	if m.Client.Transport().Connected() {
		return true
	}

	ctx, cancel := context.WithTimeout(ctx, b.tune.pingTimeout)
	defer cancel()

	_, err := m.Client.JSONRPCPing(ctx)

	return err == nil
}
