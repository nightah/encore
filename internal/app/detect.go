package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// Library sharing is detected without ever writing to Kodi. A fingerprint (IDs, file paths and dates added of sampled movies and episodes) groups instances whose libraries agree as likely shared. Playback then confirms it: when one member saves a play count or resume point, the others must show the same change, since they read the same database row; a member that doesn't is split off.

func (r fpResult) vetoed() bool {
	return r.DatabaseNames[0] != "" && r.DatabaseNames[1] != "" && r.DatabaseNames[0] != r.DatabaseNames[1]
}

func (r fpResult) meaningful(t *tuning) bool { return r.Compared >= t.minSample }

func (r fpResult) match(t *tuning) bool {
	return !r.vetoed() && r.meaningful(t) && float64(r.Matched) >= t.minAgreement*float64(r.Compared)
}

func (r fpResult) summary() string {
	return fmt.Sprintf("%d of %d sampled items match", r.Matched, r.Compared)
}

func (r fpResult) evidence(now time.Time) *fingerprintEvidence {
	return &fingerprintEvidence{Matched: r.Matched, Compared: r.Compared, DatabaseName: r.DatabaseNames[0], At: now}
}

// compareFingerprints counts sampled items that agree on ID, file path and date added. Items without a file path never agree.
func compareFingerprints(a, b *kodi.LibraryFingerprint) fpResult {
	r := fpResult{DatabaseNames: [2]string{a.DatabaseName, b.DatabaseName}}

	count := func(x, y []kodi.FingerprintItem) {
		byID := make(map[int]kodi.FingerprintItem, len(y))
		for _, it := range y {
			byID[it.ID] = it
		}

		r.Compared += max(len(x), len(y))

		for _, it := range x {
			if other, ok := byID[it.ID]; ok && it.File != "" && other == it {
				r.Matched++
			}
		}
	}

	count(a.Movies, b.Movies)
	count(a.Episodes, b.Episodes)

	return r
}

func (a *App) watchInstance(ctx context.Context, inst *Instance) {
	observed := make(chan observation, 64)

	stop := inst.Client.Listen(kodi.Listener{Notification: func(n kodi.Notification) {
		if playerNotifications[n.Method] {
			a.playerChanged(inst.id)
		}

		if o, ok := a.observe(inst, n); ok {
			select {
			case observed <- o:
			default:
			}
		}
	}})
	defer stop()

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Go(func() { a.fingerprintLoop(ctx, inst) })

	pending := map[media.Ref]observation{}

	timer := time.NewTimer(time.Hour)
	timer.Stop()

	defer timer.Stop()

	rearm := func() {
		var next time.Time

		for _, o := range pending {
			if next.IsZero() || o.due.Before(next) {
				next = o.due
			}
		}

		if !next.IsZero() {
			timer.Reset(time.Until(next))
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case o := <-observed:
			if p, ok := pending[o.ref]; ok {
				o = p.merge(&o)
			}

			o.due = time.Now().Add(a.tune.confirmDebounce)
			pending[o.ref] = o

			rearm()
		case <-timer.C:
			now := time.Now()

			for ref, o := range pending {
				if !o.due.After(now) {
					delete(pending, ref)
					wg.Go(func() { a.confirm(ctx, inst, &o) })
				}
			}

			rearm()
		}
	}
}

// fingerprintLoop retries until one sample succeeds.
func (a *App) fingerprintLoop(ctx context.Context, inst *Instance) {
	for {
		fp, err := inst.Backend.Fingerprint(ctx, a.tune.fingerprintSample)
		if err == nil {
			a.planMu.Lock()
			// A runtime replaced while this waited for the lock is no longer the instance's.
			if ctx.Err() == nil {
				a.fingerprinted(ctx, inst, &fp)
			}
			a.planMu.Unlock()

			return
		}

		inst.log.Debug("couldn't sample the library yet", "error", err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(a.tune.fingerprintRetry):
		}
	}
}

// fingerprinted records an instance's fingerprint and acts on it: an instance on its own joins a group whose library matches; a member of a detected group whose library clearly doesn't match is split off. Manual groups are left alone. Callers hold planMu.
func (a *App) fingerprinted(ctx context.Context, inst *Instance, fp *kodi.LibraryFingerprint) {
	a.mu.Lock()
	a.prints[inst.id] = fp
	g := inst.group
	a.mu.Unlock()

	if g == nil || !g.detectable() || fp.Empty() {
		return
	}

	if len(g.members) > 1 {
		a.recheckMember(ctx, inst, g, fp)

		return
	}

	a.mu.RLock()
	candidates := make([]*Instance, 0, len(a.instances))

	for _, p := range a.instances {
		if p != inst && a.prints[p.id] != nil && p.group != nil && p.group != g && p.group.detectable() {
			candidates = append(candidates, p)
		}
	}
	a.mu.RUnlock()

	slices.SortFunc(candidates, func(x, y *Instance) int { return cmp.Compare(x.id, y.id) })

	for _, p := range candidates {
		pf := a.prints[p.id]
		if pf.Empty() || vetoed(inst, g, p.group) {
			continue
		}

		r := compareFingerprints(fp, pf)
		if !r.match(&a.tune) {
			inst.log.Debug("library differs from another Kodi instance", "other", p.name, "evidence", r.summary(), "vetoed", r.vetoed())

			continue
		}

		if err := a.join(ctx, inst, g, p, &r); err != nil {
			inst.log.Warn("grouping Kodi instances that share a library", "other", p.name, "error", err)
		}

		return
	}
}

// recheckMember splits inst off when its new fingerprint clearly differs from another member's.
func (a *App) recheckMember(ctx context.Context, inst *Instance, g *group, fp *kodi.LibraryFingerprint) {
	for _, p := range g.members {
		pf := a.prints[p.id]
		if p == inst || pf == nil || pf.Empty() {
			continue
		}

		r := compareFingerprints(fp, pf)

		var reason string

		switch {
		case r.vetoed():
			reason = fmt.Sprintf("%s uses database %s but %s uses %s", inst.name, r.DatabaseNames[0], p.name, r.DatabaseNames[1])
		case r.meaningful(&a.tune) && !r.match(&a.tune):
			reason = fmt.Sprintf("the libraries of %s and %s no longer agree: %s", inst.name, p.name, r.summary())
		case r.match(&a.tune):
			rec, ev := g.rec, g.ev.clone()
			ev.Fingerprint = r.evidence(time.Now())

			if err := a.saveGroup(ctx, g, rec, ev); err != nil {
				g.log.Warn("saving library group evidence", "error", err)
			}

			return
		default:
			return
		}

		if err := a.split(ctx, g, []*Instance{inst}, reason); err != nil {
			g.log.Warn("splitting a library group", "error", err)
		}

		return
	}
}

// vetoed reports whether an earlier split ruled out grouping inst with the members of h.
func vetoed(inst *Instance, g, h *group) bool {
	if slices.Contains(h.ev.Vetoed, inst.id) {
		return true
	}

	return slices.ContainsFunc(h.members, func(m *Instance) bool { return slices.Contains(g.ev.Vetoed, m.id) })
}

var playerNotifications = map[kodirpc.NotificationName]bool{
	kodirpc.NotificationPlayerOnPlay:               true,
	kodirpc.NotificationPlayerOnPause:              true,
	kodirpc.NotificationPlayerOnResume:             true,
	kodirpc.NotificationPlayerOnStop:               true,
	kodirpc.NotificationPlayerOnSeek:               true,
	kodirpc.NotificationPlayerOnSpeedChanged:       true,
	kodirpc.NotificationApplicationOnVolumeChanged: true,
}

// OnPlayerChange registers fn to hear when a Kodi instance starts, pauses, resumes, stops or seeks playback or changes volume, as Kodi reports it, so status can be refreshed at once instead of at the next poll. fn must not block.
func (a *App) OnPlayerChange(fn func(instanceID int64)) {
	a.mu.Lock()
	a.onPlayer = fn
	a.mu.Unlock()
}

func (a *App) playerChanged(id int64) {
	a.mu.RLock()
	fn := a.onPlayer
	a.mu.RUnlock()

	if fn != nil {
		fn(id)
	}
}
