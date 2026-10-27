package library

import (
	"context"
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

func newPending() *pending {
	return &pending{refresh: map[media.Ref]bool{}, remove: map[media.Ref]bool{}, signal: make(chan struct{}, 1)}
}

func (p *pending) add(ev media.Event) {
	p.mu.Lock()

	switch ev.Kind {
	case media.EventItemUpdated:
		delete(p.remove, ev.Ref)
		p.refresh[ev.Ref] = true
	case media.EventItemRemoved:
		delete(p.refresh, ev.Ref)
		p.remove[ev.Ref] = true
	case media.EventLibraryScanned:
		p.delta = true
	case media.EventConnected:
		p.full = true
	default:
		p.mu.Unlock()

		return
	}

	p.mu.Unlock()

	select {
	case p.signal <- struct{}{}:
	default:
	}
}

// take also resets p.
func (p *pending) take() (full, delta bool, refresh, remove []media.Ref) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for ref := range p.refresh {
		refresh = append(refresh, ref)
	}

	for ref := range p.remove {
		remove = append(remove, ref)
	}

	full, delta = p.full, p.delta
	p.full, p.delta = false, false
	clear(p.refresh)
	clear(p.remove)

	return full, delta, refresh, remove
}

// Run syncs the catalog, then keeps it current from backend events and periodic deltas until ctx is done.
func (c *Catalog) Run(ctx context.Context) error {
	if err := c.Sync(ctx); err != nil {
		c.warn(ctx, "initial library sync incomplete", err)
	}

	p := newPending()

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Go(func() { c.subscribe(ctx, p) })

	var tick <-chan time.Time

	if c.opts.RefreshInterval > 0 {
		ticker := time.NewTicker(c.opts.RefreshInterval)
		defer ticker.Stop()

		tick = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			if err := c.Delta(ctx); err != nil && ctx.Err() == nil {
				c.warn(ctx, "periodic library delta failed", err)
			}
		case <-p.signal:
			// Let a burst of events settle before acting on it.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(c.opts.Debounce):
			}

			c.process(ctx, p)
		}
	}
}

func (c *Catalog) subscribe(ctx context.Context, p *pending) {
	for {
		err := c.backend.Subscribe(ctx, func(ev media.Event) {
			c.mu.Lock()
			c.lastEvent = c.opts.Now()
			c.mu.Unlock()

			p.add(ev)
		})

		if ctx.Err() != nil {
			return
		}

		c.warn(ctx, "backend subscription ended", err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(resubscribeDelay):
		}
	}
}

func (c *Catalog) process(ctx context.Context, p *pending) {
	full, delta, refresh, remove := p.take()

	c.write.Lock()
	defer c.write.Unlock()

	if full {
		c.mu.RLock()
		recent := c.opts.Now().Sub(c.lastSync) < c.opts.MinResync
		c.mu.RUnlock()

		if !recent {
			if err := c.syncLocked(ctx); err != nil && ctx.Err() == nil {
				c.warn(ctx, "library resync failed", err)
			}

			// A full sync covers every pending change.
			return
		}
	}

	if delta {
		if err := c.deltaLocked(ctx); err != nil && ctx.Err() == nil {
			c.warn(ctx, "library delta failed", err)
		}
	}

	for _, ref := range remove {
		c.removeLocked(ref)
	}

	for _, ref := range refresh {
		if err := c.refreshLocked(ctx, ref); err != nil && ctx.Err() == nil {
			c.warn(ctx, "library item refresh failed", err)
		}
	}
}

// warn logs a failed sync step, unless ctx has ended: then Encore is stopping, and nothing is wrong with the library.
func (c *Catalog) warn(ctx context.Context, msg string, err error) {
	if ctx.Err() != nil {
		return
	}

	c.log.Warn(msg, "error", err)
}
