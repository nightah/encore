package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nightah/encore/internal/app"
)

// playerChanged refreshes an instance's status shortly after Kodi reports a player or volume change, so the web interface doesn't wait for the next poll.
func (c *statusCache) playerChanged(id int64) {
	c.mu.Lock()
	ctx := c.ctx
	if ctx == nil || c.refreshing[id] {
		c.mu.Unlock()

		return
	}

	c.refreshing[id] = true
	c.mu.Unlock()

	time.AfterFunc(playerSettle, func() {
		c.mu.Lock()
		delete(c.refreshing, id)
		c.mu.Unlock()

		if ctx.Err() != nil {
			return
		}

		if _, err := c.probe(ctx, id); err != nil {
			return
		}

		if inst, err := c.a.instance(ctx, id); err == nil {
			c.publishIfChanged("instance", "instance/"+itoa(id), inst)
		}
	})
}

func newStatusCache(a *API) *statusCache {
	return &statusCache{a: a, entries: map[int64]statusEntry{}, published: map[string]string{}, refreshing: map[int64]bool{}}
}

// get returns a recent status, probing Kodi when the cached one is stale.
func (c *statusCache) get(ctx context.Context, id int64) (app.InstanceStatus, error) {
	c.mu.Lock()
	e, ok := c.entries[id]
	c.mu.Unlock()

	if ok && c.a.now().Sub(e.at) < 2*c.a.opts.StatusInterval {
		return e.st, nil
	}

	return c.probe(ctx, id)
}

// getAll returns the status of each id, probing stale ones concurrently.
func (c *statusCache) getAll(ctx context.Context, ids []int64) map[int64]app.InstanceStatus {
	out := make(map[int64]app.InstanceStatus, len(ids))

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for _, id := range ids {
		wg.Go(func() {
			st, err := c.get(ctx, id)
			if err != nil {
				return
			}

			mu.Lock()
			out[id] = st
			mu.Unlock()
		})
	}

	wg.Wait()

	return out
}

func (c *statusCache) probe(ctx context.Context, id int64) (app.InstanceStatus, error) {
	now := c.a.now()

	c.mu.Lock()
	prev, had := c.entries[id]
	c.mu.Unlock()

	info := !had || prev.st.KodiVersion == "" || now.Sub(prev.infoAt) >= infoEvery

	st, err := c.a.app.InstanceStatus(ctx, id, info)
	if err != nil {
		return st, err
	}

	e := statusEntry{st: st, at: now, infoAt: prev.infoAt}

	switch {
	case info && st.Reachable:
		e.infoAt = now
	case had:
		// Keep what the last full probe learned.
		e.st.KodiVersion, e.st.APIVersion = prev.st.KodiVersion, prev.st.APIVersion
		e.st.Counts.Episodes = max(e.st.Counts.Episodes, prev.st.Counts.Episodes)
	}

	c.mu.Lock()
	c.entries[id] = e
	c.mu.Unlock()

	return e.st, nil
}

func (c *statusCache) forget(id int64) {
	c.mu.Lock()
	delete(c.entries, id)
	c.mu.Unlock()
}

// run publishes instance, library and attention events only when something visible changed.
func (c *statusCache) run(ctx context.Context) {
	t := time.NewTicker(c.a.opts.StatusInterval)
	defer t.Stop()

	c.mu.Lock()
	c.ctx = ctx
	c.mu.Unlock()

	for {
		c.poll(ctx)

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *statusCache) poll(ctx context.Context) {
	ids := c.a.app.InstanceIDs()

	for _, id := range ids {
		if _, err := c.probe(ctx, id); err != nil {
			continue
		}

		if inst, err := c.a.instance(ctx, id); err == nil {
			c.publishIfChanged("instance", "instance/"+itoa(id), inst)
		}
	}

	if libs, err := c.a.libraries.List(ctx); err == nil {
		for _, l := range libs {
			c.publishIfChanged("library", "library/"+itoa(l.ID), l)
		}
	}

	if att, err := c.a.attention(ctx); err == nil {
		c.publishIfChanged("attention", "attention", att)
	}
}

// publishIfChanged publishes v under event when its JSON differs from what was last published for key.
func (c *statusCache) publishIfChanged(event, key string, v any) {
	b, err := json.Marshal(fillEmpty(v))
	if err != nil {
		return
	}

	c.mu.Lock()
	same := c.published[key] == string(b)
	c.published[key] = string(b)
	c.mu.Unlock()

	if !same {
		c.a.events.publishRaw(event, b)
	}
}
