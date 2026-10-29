package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/library"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

func defaultTuning() tuning {
	return tuning{
		fingerprintSample: fingerprintSample,
		minSample:         minFingerprintItems,
		minAgreement:      minFingerprintAgreement,
		fingerprintRetry:  time.Minute,
		confirmDebounce:   2 * time.Second,
		confirmGrace:      15 * time.Second,
		failoverInterval:  30 * time.Second,
		pingTimeout:       2 * time.Second,
		snapshotEvery:     5 * time.Minute,
		snapshotCheck:     15 * time.Second,
		learnMACDelay:     5 * time.Second,
	}
}

func decodeEvidence(raw json.RawMessage) evidence {
	var ev evidence
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &ev) // evidence is advisory; a damaged value just starts over
	}

	return ev
}

func (ev *evidence) encode() json.RawMessage {
	raw, _ := json.Marshal(ev) // a plain struct of strings, numbers and times always encodes

	return raw
}

func (ev *evidence) clone() evidence {
	c := *ev
	c.Confirmed, c.Vetoed = slices.Clone(ev.Confirmed), slices.Clone(ev.Vetoed)

	return c
}

func (g *group) member(id int64) *Instance {
	for _, m := range g.members {
		if m.id == id {
			return m
		}
	}

	return nil
}

func (g *group) memberIDs() []int64 {
	ids := make([]int64, len(g.members))
	for i, m := range g.members {
		ids[i] = m.id
	}

	return ids
}

func (g *group) configManaged() bool { return g.ev.Origin == originConfig }

func (g *group) detectable() bool { return !g.rec.Manual }

func (g *group) stop() {
	if g.cancel == nil {
		return
	}

	g.cancel()
	<-g.done
	g.cancel = nil
}

func (g *group) close() error {
	if g.unlisten != nil {
		g.unlisten()
	}

	return g.index.Close()
}

func (a *App) newGroup(rec store.LibraryGroup, ev evidence, members []*Instance, source *Instance) (*group, error) {
	// The library is named after its source instance (see library.New), so instance is the source's name.
	log := a.log.With("library_group", rec.ID, "instance", source.name, "backend", "kodi")

	index, err := match.New(source.language(), match.DefaultConfig())
	if err != nil {
		return nil, err
	}

	backend := newGroupBackend(members, source, a.tune, log)

	opts := a.tune.catalog
	opts.Logger = log.With("component", "library")

	if len(opts.Kinds) == 0 {
		opts.Kinds = catalogKinds(a.voiceSettings(context.Background()).Content)
	}

	shared := len(members) > 1 && rec.State != store.GroupSolo
	g := &group{id: rec.ID, shared: shared, rec: rec, ev: ev, members: members, source: source, index: index, backend: backend, log: log}
	// The catalog is named after the source, so a snapshot taken while another member was the source isn't restored by mistake.
	g.catalog = library.New(source.name, backend, opts)
	g.unlisten = g.catalog.OnChange(g.indexChanges)

	return g, nil
}

// startGroup is a no-op until Run starts. Callers hold planMu.
func (a *App) startGroup(g *group) {
	if a.runCtx == nil {
		return
	}

	ctx, cancel := context.WithCancel(a.runCtx)
	g.cancel, g.done = cancel, make(chan struct{})

	go func() {
		defer close(g.done)

		a.runGroup(ctx, g)
	}()
}

func (a *App) runGroup(ctx context.Context, g *group) {
	// A catalog seeded from the runtime this one replaced is already indexed, without aliases.
	if err := a.refreshAliases(ctx, g); err != nil {
		g.log.Warn("reading title aliases", "error", err)
	}

	if !g.seeded {
		a.restoreSnapshot(ctx, g)
	}

	multi := len(g.members) > 1
	if multi {
		// Sync from a reachable member from the start.
		g.backend.evaluate(ctx)
	}

	var wg sync.WaitGroup

	wg.Go(func() {
		if err := g.catalog.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			g.log.Error("library sync stopped", "error", err)
		}
	})

	if multi {
		wg.Go(func() { g.backend.supervise(ctx) })
	}

	wg.Go(func() { a.keepSnapshots(ctx, g) })

	wg.Wait()
}

// applyConfiguredGroups follows kodi[].library_group. Groups from an earlier config that no longer apply are dissolved, leaving their members to detection.
func (a *App) applyConfiguredGroups(ctx context.Context) error {
	kodis, err := a.db.KodiInstances().List(ctx)
	if err != nil {
		return err
	}

	recs, err := a.db.LibraryGroups().List(ctx)
	if err != nil {
		return err
	}

	byName := map[string]store.KodiInstance{}
	for _, k := range kodis {
		byName[k.Name] = k
	}

	var (
		names []string
		named = map[string][]int64{}
		none  []store.KodiInstance
	)

	for i := range a.cfg.Kodi {
		c := &a.cfg.Kodi[i]

		k, ok := byName[c.Name]
		if !ok {
			continue
		}

		switch {
		case c.NoLibraryGroup():
			none = append(none, k)
		case c.LibraryGroup != "":
			if _, ok := named[c.LibraryGroup]; !ok {
				names = append(names, c.LibraryGroup)
			}

			named[c.LibraryGroup] = append(named[c.LibraryGroup], k.ID)
		}
	}

	used := map[int64]bool{}

	find := func(match func(*store.LibraryGroup) bool) *store.LibraryGroup {
		for i := range recs {
			if r := &recs[i]; decodeEvidence(r.Evidence).Origin == originConfig && match(r) {
				return r
			}
		}

		return nil
	}

	for _, name := range names {
		members := named[name]

		rec := find(func(r *store.LibraryGroup) bool { return !decodeEvidence(r.Evidence).None && r.Name == name })
		if rec == nil {
			rec = &store.LibraryGroup{Name: name, State: store.GroupManual, Manual: true, Evidence: (&evidence{Origin: originConfig}).encode()}
		}

		if !slices.Contains(members, rec.SourceInstanceID) {
			rec.SourceInstanceID = members[0]
		}

		if err := a.db.LibraryGroups().Save(ctx, rec, members); err != nil {
			return fmt.Errorf("library group %q: %w", name, err)
		}

		used[rec.ID] = true
	}

	for _, k := range none {
		rec := find(func(r *store.LibraryGroup) bool { return decodeEvidence(r.Evidence).None && r.ID == k.LibraryGroupID })
		if rec == nil {
			ev := evidence{Origin: originConfig, None: true}
			rec = &store.LibraryGroup{Name: k.Name, State: store.GroupSolo, Manual: true, SourceInstanceID: k.ID, Evidence: ev.encode()}
		}

		if err := a.db.LibraryGroups().Save(ctx, rec, []int64{k.ID}); err != nil {
			return fmt.Errorf("kodi instance %q: %w", k.Name, err)
		}

		used[rec.ID] = true
	}

	for _, r := range recs {
		if !used[r.ID] && decodeEvidence(r.Evidence).Origin == originConfig {
			if err := a.db.LibraryGroups().Delete(ctx, r.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

// reconcileGroups gives every instance a group, points each group's source at a member, keeps solo states honest and removes empty groups, whose IDs it returns.
func (a *App) reconcileGroups(ctx context.Context) ([]int64, error) {
	repo := a.db.LibraryGroups()

	kodis, err := a.db.KodiInstances().List(ctx)
	if err != nil {
		return nil, err
	}

	recs, err := repo.List(ctx)
	if err != nil {
		return nil, err
	}

	members := map[int64][]int64{}

	for _, k := range kodis {
		if k.LibraryGroupID != 0 {
			members[k.LibraryGroupID] = append(members[k.LibraryGroupID], k.ID)

			continue
		}

		rec := store.LibraryGroup{Name: k.Name, State: store.GroupSolo, SourceInstanceID: k.ID}
		if err := repo.Save(ctx, &rec, []int64{k.ID}); err != nil {
			return nil, fmt.Errorf("kodi instance %q: %w", k.Name, err)
		}
	}

	for i := range recs {
		rec := &recs[i]

		ids := members[rec.ID]
		if len(ids) == 0 {
			continue
		}

		slices.Sort(ids)

		changed := false

		if !slices.Contains(ids, rec.SourceInstanceID) {
			rec.SourceInstanceID, changed = ids[0], true
		}

		switch {
		case rec.Manual && len(ids) > 1 && rec.State != store.GroupManual:
			rec.State, changed = store.GroupManual, true
		case rec.Manual && len(ids) == 1 && rec.State != store.GroupSolo:
			rec.State, changed = store.GroupSolo, true
		case !rec.Manual && len(ids) == 1 && rec.State != store.GroupSolo:
			ev := decodeEvidence(rec.Evidence)
			ev.Confirmed, ev.Fingerprint, ev.Confirmation = nil, nil, ""
			rec.State, rec.Evidence, changed = store.GroupSolo, ev.encode(), true
		case !rec.Manual && len(ids) > 1 && rec.State == store.GroupSolo:
			rec.State, changed = store.GroupLikelyShared, true
		}

		if changed {
			if err := repo.Save(ctx, rec, nil); err != nil {
				return nil, err
			}
		}
	}

	return repo.DeleteEmpty(ctx)
}

// plan brings the stored library groups in line with their invariants, then rebuilds the runtimes of groups whose members or source changed. Callers hold planMu.
func (a *App) plan(ctx context.Context) error {
	deleted, err := a.reconcileGroups(ctx)
	if err != nil {
		return fmt.Errorf("reconciling library groups: %w", err)
	}

	if err := a.rebuildGroups(ctx); err != nil {
		return fmt.Errorf("rebuilding library groups: %w", err)
	}

	for _, id := range deleted {
		a.deleteSnapshot(ctx, id)
	}

	return nil
}

// rebuildGroups keeps unchanged groups running. Callers hold planMu.
func (a *App) rebuildGroups(ctx context.Context) error {
	recs, err := a.db.LibraryGroups().List(ctx)
	if err != nil {
		return err
	}

	kodis, err := a.db.KodiInstances().List(ctx)
	if err != nil {
		return err
	}

	membersOf := map[int64][]*Instance{}

	a.mu.RLock()
	for _, k := range kodis {
		if inst, ok := a.instances[k.ID]; ok && k.LibraryGroupID != 0 {
			membersOf[k.LibraryGroupID] = append(membersOf[k.LibraryGroupID], inst)
		}
	}
	a.mu.RUnlock()

	var (
		next    = map[int64]*group{}
		kept    = map[*group]groupUpdate{}
		fresh   []*group
		stale   []*group
		failure error
	)

	for _, rec := range recs {
		members := membersOf[rec.ID]
		if len(members) == 0 {
			continue
		}

		slices.SortFunc(members, func(x, y *Instance) int { return cmp.Compare(x.id, y.id) })

		source := members[0]
		if m := slices.IndexFunc(members, func(m *Instance) bool { return m.id == rec.SourceInstanceID }); m >= 0 {
			source = members[m]
		}

		ev := decodeEvidence(rec.Evidence)

		if old, ok := a.groups[rec.ID]; ok && old.source == source && slices.Equal(old.members, members) {
			next[rec.ID], kept[old] = old, groupUpdate{rec, ev}

			continue
		}

		g, err := a.newGroup(rec, ev, members, source)
		if err != nil {
			failure = fmt.Errorf("library group %d: %w", rec.ID, err)

			break
		}

		next[rec.ID] = g
		fresh = append(fresh, g)
	}

	if failure != nil {
		for _, g := range fresh {
			_ = g.close()
		}

		return failure
	}

	for id, g := range a.groups {
		if next[id] != g {
			stale = append(stale, g)
		}
	}

	// Stop replaced runtimes first so their last changes are in their catalogs, then carry those over.
	for _, g := range stale {
		g.stop()
	}

	for _, g := range fresh {
		a.seed(g, stale)
	}

	a.mu.Lock()
	for g, u := range kept {
		g.rec, g.ev = u.rec, u.ev
	}

	a.groups = next

	for _, g := range next {
		for _, m := range g.members {
			m.group, m.Catalog, m.Index = g, g.catalog, g.index
		}
	}
	a.mu.Unlock()

	for _, g := range stale {
		if err := g.close(); err != nil {
			g.log.Warn("closing library group", "error", err)
		}
	}

	for _, g := range fresh {
		a.startGroup(g)
	}

	return nil
}

// seed restores a new group's catalog from a runtime it replaces, so the title index stays warm while the group changes shape.
func (a *App) seed(g *group, stale []*group) {
	for _, old := range stale {
		if !g.snapshotFits(old.catalog.Instance()) || old.catalog.Stats().LastSync.IsZero() {
			continue
		}

		snap := old.catalog.Snapshot()
		snap.Instance = g.catalog.Instance()

		if err := g.catalog.Restore(snap); err != nil {
			g.log.Warn("carrying the library over", "error", err)

			continue
		}

		g.seeded = true

		return
	}
}

// snapshotFits reports whether a catalog snapshot taken with instance as the source describes this group's library: it's the group's source, or another member of a group that shares one library.
func (g *group) snapshotFits(instance string) bool {
	if instance == g.source.name {
		return true
	}

	if !g.shared {
		return false
	}

	return slices.ContainsFunc(g.members, func(m *Instance) bool { return m.name == instance })
}

// Callers hold planMu.
func (a *App) saveGroup(ctx context.Context, g *group, rec store.LibraryGroup, ev evidence) error {
	rec.Evidence = ev.encode()

	if err := a.db.LibraryGroups().Save(ctx, &rec, nil); err != nil {
		return err
	}

	a.mu.Lock()
	g.rec, g.ev = rec, ev
	a.mu.Unlock()

	return nil
}

// Callers hold planMu.
func (a *App) instanceAndGroup(id int64) (*Instance, *group, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	inst, ok := a.instances[id]
	if !ok || inst.group == nil {
		return nil, nil, fmt.Errorf("kodi instance %d: %w", id, store.ErrNotFound)
	}

	return inst, inst.group, nil
}

// SetGroupSource makes a member the library group's source, the Kodi instance that runs its library syncs. The group's runtime restarts.
func (a *App) SetGroupSource(ctx context.Context, groupID, instanceID int64) error {
	a.planMu.Lock()
	defer a.planMu.Unlock()

	g, ok := a.groups[groupID]
	if !ok {
		return fmt.Errorf("library group %d: %w", groupID, store.ErrNotFound)
	}

	if g.member(instanceID) == nil {
		return fmt.Errorf("kodi instance %d is not in library group %d: %w", instanceID, groupID, store.ErrInvalid)
	}

	if g.source.id == instanceID {
		return nil
	}

	rec := g.rec
	rec.SourceInstanceID = instanceID

	if err := a.saveGroup(ctx, g, rec, g.ev); err != nil {
		return err
	}

	return a.plan(ctx)
}

// GroupInstances puts Kodi instances in one library group by the user's choice, which detection never undoes. The first instance is the source unless the instances already share a group with another member as the source. It returns the group's ID.
func (a *App) GroupInstances(ctx context.Context, name string, instanceIDs ...int64) (int64, error) {
	a.planMu.Lock()
	defer a.planMu.Unlock()

	if len(instanceIDs) == 0 {
		return 0, fmt.Errorf("no kodi instances to group: %w", store.ErrInvalid)
	}

	var ids []int64

	for _, id := range instanceIDs {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}

	var groups []*group

	for _, id := range ids {
		_, g, err := a.instanceAndGroup(id)
		if err != nil {
			return 0, err
		}

		if g.configManaged() {
			return 0, fmt.Errorf("kodi instance %d: %w", id, ErrManagedByConfig)
		}

		if !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}

	rec := store.LibraryGroup{SourceInstanceID: ids[0]}
	ev := evidence{Origin: originUser}

	// Instances already together keep their group, and so their snapshot.
	if len(groups) == 1 && slices.Equal(groups[0].memberIDs(), sortedIDs(ids)) {
		rec, ev = groups[0].rec, groups[0].ev.clone()
		ev.Origin, ev.Reason = originUser, ""
	}

	if name == "" {
		name = a.instances[rec.SourceInstanceID].name
	}

	rec.Name, rec.Manual, rec.State, rec.Evidence = name, true, store.GroupManual, ev.encode()
	if len(ids) == 1 {
		rec.State = store.GroupSolo
	}

	if err := a.db.LibraryGroups().Save(ctx, &rec, ids); err != nil {
		return 0, err
	}

	return rec.ID, a.plan(ctx)
}

// SplitInstance gives a Kodi instance a library of its own by the user's choice, which detection never undoes.
func (a *App) SplitInstance(ctx context.Context, instanceID int64) error {
	a.planMu.Lock()
	defer a.planMu.Unlock()

	inst, g, err := a.instanceAndGroup(instanceID)
	if err != nil {
		return err
	}

	if g.configManaged() {
		return fmt.Errorf("kodi instance %d: %w", instanceID, ErrManagedByConfig)
	}

	ev := evidence{Origin: originUser, Reason: "kept separate by the user"}
	rec := store.LibraryGroup{Name: inst.name, State: store.GroupSolo, Manual: true, SourceInstanceID: inst.id, Evidence: ev.encode()}

	if len(g.members) == 1 {
		rec.ID, rec.CreatedAt = g.rec.ID, g.rec.CreatedAt
	}

	if err := a.db.LibraryGroups().Save(ctx, &rec, []int64{inst.id}); err != nil {
		return err
	}

	return a.plan(ctx)
}

// ResetLibraryGroup hands a Kodi instance back to detection, undoing GroupInstances or SplitInstance for it.
func (a *App) ResetLibraryGroup(ctx context.Context, instanceID int64) error {
	a.planMu.Lock()
	defer a.planMu.Unlock()

	inst, g, err := a.instanceAndGroup(instanceID)
	if err != nil {
		return err
	}

	switch {
	case g.configManaged():
		return fmt.Errorf("kodi instance %d: %w", instanceID, ErrManagedByConfig)
	case !g.rec.Manual:
		return nil
	}

	rec := store.LibraryGroup{Name: inst.name, State: store.GroupSolo, SourceInstanceID: inst.id, Evidence: (&evidence{Origin: originDetected}).encode()}
	if len(g.members) == 1 {
		rec.ID, rec.CreatedAt = g.rec.ID, g.rec.CreatedAt
	}

	if err := a.db.LibraryGroups().Save(ctx, &rec, []int64{inst.id}); err != nil {
		return err
	}

	if err := a.plan(ctx); err != nil {
		return err
	}

	a.mu.RLock()
	fp := a.prints[inst.id]
	a.mu.RUnlock()

	if fp != nil {
		a.fingerprinted(ctx, inst, fp)
	}

	return nil
}

func sortedIDs(ids []int64) []int64 {
	out := slices.Clone(ids)
	slices.Sort(out)

	return out
}

// contentKinds leaves out episodes, which are fetched per show on demand.
var contentKinds = map[media.Content][]media.Kind{
	media.ContentMovies: {media.KindMovie},
	media.ContentTV:     {media.KindTVShow},
	media.ContentMusic:  {media.KindMusicVideo, media.KindArtist, media.KindAlbum, media.KindSong},
}

// catalogKinds returns the library kinds to load for the content groups the skill plays (alexa.content), plus playlists, which can hold anything.
func catalogKinds(content []string) []media.Kind {
	var out []media.Kind

	for _, c := range media.Contents {
		if slices.Contains(content, string(c)) {
			out = append(out, contentKinds[c]...)
		}
	}

	return append(out, media.KindPlaylist)
}

// ApplyContent brings every library in line with the content groups the skill plays: titles of a group turned off leave the index, and a group turned on is fetched from Kodi straight away.
func (a *App) ApplyContent(ctx context.Context) {
	kinds := catalogKinds(a.voiceSettings(ctx).Content)

	// runCtx is guarded by planMu. It isn't held while catalogs change, as SetKinds waits for a running sync; a group replaced meanwhile gets the kinds from newGroup.
	a.planMu.Lock()
	runCtx := a.runCtx

	a.mu.RLock()
	groups := make([]*group, 0, len(a.groups))
	for _, g := range a.groups {
		groups = append(groups, g)
	}
	a.mu.RUnlock()
	a.planMu.Unlock()

	for _, g := range groups {
		if slices.Equal(g.catalog.Kinds(), kinds) {
			continue
		}

		g.catalog.SetKinds(kinds)

		if runCtx != nil {
			go func() {
				if err := g.catalog.Delta(runCtx); err != nil && runCtx.Err() == nil {
					g.log.Warn("fetching newly included titles", "error", err)
				}
			}()
		}
	}
}
