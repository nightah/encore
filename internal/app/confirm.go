package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

// join moves inst, on its own in group g, into the group of p, whose library matches. Two instances on their own form a group led by the one created first, which keeps its group and so its snapshot. Callers hold planMu.
func (a *App) join(ctx context.Context, inst *Instance, g *group, p *Instance, r *fpResult) error {
	h := p.group

	var (
		rec     store.LibraryGroup
		ev      evidence
		members []int64
	)

	if len(h.members) == 1 {
		first, firstGroup := p, h
		if inst.id < p.id {
			first, firstGroup = inst, g
		}

		rec = firstGroup.rec
		rec.Name, rec.SourceInstanceID = first.name, first.id
		members = sortedIDs([]int64{inst.id, p.id})
	} else {
		rec, ev = h.rec, h.ev.clone()
		members = append(h.memberIDs(), inst.id)
	}

	for _, id := range append(slices.Clone(g.ev.Vetoed), h.ev.Vetoed...) {
		if !slices.Contains(ev.Vetoed, id) {
			ev.Vetoed = append(ev.Vetoed, id)
		}
	}

	ev.Origin, ev.Reason, ev.Fingerprint = originDetected, "", r.evidence(time.Now())
	rec.State, rec.Evidence = store.GroupLikelyShared, ev.encode()

	if err := a.db.LibraryGroups().Save(ctx, &rec, members); err != nil {
		return err
	}

	inst.log.Info("Kodi instances look like they share a library database; they now share one library", "other", p.name, "evidence", r.summary())

	return a.plan(ctx)
}

// split moves each of leaving out of g into a group of its own that detection won't merge back. Callers hold planMu.
func (a *App) split(ctx context.Context, g *group, leaving []*Instance, reason string) error {
	repo := a.db.LibraryGroups()
	rec, ev := g.rec, g.ev.clone()

	saved := false
	fail := func(err error) error {
		// Members already saved in groups of their own run that way, so the running groups match the database.
		if saved {
			err = errors.Join(err, a.plan(ctx))
		}

		return err
	}

	for _, inst := range leaving {
		var others []int64

		for _, m := range g.members {
			if m != inst {
				others = append(others, m.id)
			}
		}

		own := evidence{Origin: originDetected, Reason: reason, Vetoed: others}
		solo := store.LibraryGroup{Name: inst.name, State: store.GroupSolo, SourceInstanceID: inst.id, Evidence: own.encode()}

		if err := repo.Save(ctx, &solo, []int64{inst.id}); err != nil {
			return fail(err)
		}

		saved = true
		ev.Confirmed = slices.DeleteFunc(ev.Confirmed, func(id int64) bool { return id == inst.id })
		if !slices.Contains(ev.Vetoed, inst.id) {
			ev.Vetoed = append(ev.Vetoed, inst.id)
		}

		g.log.Warn("Kodi instance doesn't share this library after all; it keeps its own", "other", inst.name, "reason", reason)
	}

	ev.Reason = reason
	rec.Evidence = ev.encode()

	if err := repo.Save(ctx, &rec, nil); err != nil {
		return fail(err)
	}

	return a.plan(ctx)
}

// merge folds a later observation of the same item into o, keeping the oldest prior state and the newest play count.
func (o *observation) merge(later *observation) observation {
	out := *o

	if later.playCount != nil {
		out.playCount = later.playCount
	}

	if !out.havePrior {
		out.prior, out.havePrior = later.prior, later.havePrior
	}

	return out
}

// observe turns a notification that may have changed a movie's or episode's play count or resume point into an observation, when the instance's group is a detected one with other members. It runs on the connection's reader goroutine.
func (a *App) observe(inst *Instance, n kodi.Notification) (observation, bool) {
	if n.Method != kodirpc.NotificationVideoLibraryOnUpdate && n.Method != kodirpc.NotificationPlayerOnStop {
		return observation{}, false
	}

	var p struct {
		Data struct {
			ID        int    `json:"id"`
			Type      string `json:"type"`
			PlayCount *int   `json:"playcount"`
			Item      *struct {
				ID   int    `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
		} `json:"data"`
	}

	if json.Unmarshal(n.Params, &p) != nil {
		return observation{}, false
	}

	id, typ := p.Data.ID, p.Data.Type
	if p.Data.Item != nil && p.Data.Item.ID > 0 {
		id, typ = p.Data.Item.ID, p.Data.Item.Type
	}

	kind := media.Kind(typ)
	if id <= 0 || (kind != media.KindMovie && kind != media.KindEpisode) {
		return observation{}, false
	}

	o := observation{ref: media.Ref{Kind: kind, ID: id}}
	if n.Method == kodirpc.NotificationVideoLibraryOnUpdate {
		o.playCount = p.Data.PlayCount
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	g := inst.group
	if g == nil || !g.detectable() || len(g.members) < 2 {
		return observation{}, false
	}

	o.prior, o.havePrior = g.catalog.Get(o.ref)

	return o, true
}

// confirm records which members of m's group mirrored the change and splits off those that clearly didn't.
func (a *App) confirm(ctx context.Context, m *Instance, o *observation) {
	after, err := m.Backend.Item(ctx, o.ref)
	if err != nil {
		m.log.Debug("reading a changed item for library group confirmation", "error", err)

		return
	}

	if o.playCount == nil && (!o.havePrior || samePlayState(&o.prior, &after)) {
		// Nothing shows the item changed, so agreement would prove nothing.
		return
	}

	a.mu.RLock()
	g := m.group
	peers := make([]*Instance, 0, len(g.members))

	for _, p := range g.members {
		if p != m {
			peers = append(peers, p)
		}
	}
	a.mu.RUnlock()

	var seen, notSeen []*Instance

	for _, p := range peers {
		switch a.mirrors(ctx, m, p, o.ref, &after) {
		case mirrored:
			seen = append(seen, p)
		case missed:
			notSeen = append(notSeen, p)
		case inconclusive:
		}
	}

	if len(seen)+len(notSeen) == 0 || ctx.Err() != nil {
		return
	}

	a.planMu.Lock()
	defer a.planMu.Unlock()

	if ctx.Err() != nil {
		return
	}

	if err := a.recordConfirmation(ctx, m, g, after.Title, seen, notSeen); err != nil {
		g.log.Warn("recording library group confirmation", "error", err)
	}
}

// mirrors gives a difference a grace period, in case a write landed between the two reads.
func (a *App) mirrors(ctx context.Context, m, p *Instance, ref media.Ref, after *media.Item) verdict {
	got, err := p.Backend.Item(ctx, ref)

	switch {
	case errors.Is(err, media.ErrNotFound):
		return missed
	case err != nil:
		return inconclusive
	case samePlayState(after, &got):
		return mirrored
	}

	select {
	case <-ctx.Done():
		return inconclusive
	case <-time.After(a.tune.confirmGrace):
	}

	now, err := m.Backend.Item(ctx, ref)
	if err != nil {
		return inconclusive
	}

	got, err = p.Backend.Item(ctx, ref)

	switch {
	case errors.Is(err, media.ErrNotFound):
		return missed
	case err != nil:
		return inconclusive
	case samePlayState(&now, &got):
		return mirrored
	}

	return missed
}

func samePlayState(a, b *media.Item) bool {
	return a.PlayCount == b.PlayCount && a.Resume == b.Resume && a.LastPlayed.Equal(b.LastPlayed)
}

// recordConfirmation stores which members mirrored a change made on m, promoting the group to confirmed once every member has, and splits off members that didn't. When most members missed m's change, m is the odd one out and leaves instead. Callers hold planMu.
func (a *App) recordConfirmation(ctx context.Context, m *Instance, g *group, title string, seen, notSeen []*Instance) error {
	a.mu.RLock()
	current := m.group == g && g.detectable() && a.groups[g.id] == g
	a.mu.RUnlock()

	if !current {
		return nil
	}

	if len(notSeen) > 0 {
		reason := fmt.Sprintf("a change to “%s” on %s wasn't seen on %s", title, m.name, names(notSeen))

		leaving := notSeen
		if len(seen) == 0 && len(notSeen) > 1 {
			leaving = []*Instance{m}
		}

		return a.split(ctx, g, leaving, reason)
	}

	rec, ev := g.rec, g.ev.clone()

	for _, inst := range append([]*Instance{m}, seen...) {
		if !slices.Contains(ev.Confirmed, inst.id) {
			ev.Confirmed = append(ev.Confirmed, inst.id)
		}
	}

	slices.Sort(ev.Confirmed)
	ev.Confirmation = fmt.Sprintf("a change to “%s” on %s was seen on %s", title, m.name, names(seen))

	if rec.State == store.GroupLikelyShared && !slices.ContainsFunc(g.members, func(i *Instance) bool { return !slices.Contains(ev.Confirmed, i.id) }) {
		rec.State = store.GroupConfirmedShared
		g.log.Info("confirmed that Kodi instances share a library database", "evidence", ev.Confirmation)
	}

	return a.saveGroup(ctx, g, rec, ev)
}

func names(insts []*Instance) string {
	out := make([]string, len(insts))
	for i, inst := range insts {
		out[i] = inst.name
	}

	return strings.Join(out, ", ")
}
