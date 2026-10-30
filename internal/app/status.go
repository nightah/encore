package app

import (
	"cmp"
	"slices"
)

// Groups describes every library group, ordered by name.
func (a *App) Groups() []GroupStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]GroupStatus, 0, len(a.groups))

	for _, g := range a.groups {
		stats := g.catalog.Stats()

		s := GroupStatus{
			ID:            g.id,
			Name:          g.rec.Name,
			State:         g.rec.State,
			Manual:        g.rec.Manual,
			ConfigManaged: g.configManaged(),
			SourceID:      g.source.id,
			ActiveID:      g.backend.current().id,
			Evidence:      g.ev.summary(len(g.members)),
			Reason:        g.ev.Reason,
			LastSync:      stats.LastSync,
			LastDelta:     stats.LastDelta,
			Counts:        stats.Counts,
			Episodes:      stats.Episodes,
			UpdatedAt:     g.rec.UpdatedAt,
		}

		for _, m := range g.members {
			s.Members = append(s.Members, GroupMember{
				ID: m.id, Name: m.name, Fingerprinted: a.prints[m.id] != nil, Confirmed: slices.Contains(g.ev.Confirmed, m.id),
			})
		}

		out = append(out, s)
	}

	slices.SortFunc(out, func(x, y GroupStatus) int { return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.ID, y.ID)) })

	return out
}

func (ev *evidence) summary(members int) string {
	switch {
	case ev.Origin == originConfig && ev.None:
		return "kept separate by the config file"
	case ev.Origin == originConfig:
		return "grouped by the config file"
	case ev.Origin == originUser && members == 1:
		return "kept separate by the user"
	case ev.Origin == originUser:
		return "grouped by the user"
	case members == 1:
		return ""
	case ev.Confirmation != "":
		return ev.Confirmation
	case ev.Fingerprint != nil:
		r := fpResult{Matched: ev.Fingerprint.Matched, Compared: ev.Fingerprint.Compared}

		return r.summary()
	}

	return ""
}

// Libraries lists every distinct library once, however many Kodi instances share it, ordered by group ID. Slot values for the skill's interaction model can be built per library without duplicates.
func (a *App) Libraries() []Library {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]Library, 0, len(a.groups))

	for _, g := range a.groups {
		lib := Library{GroupID: g.id, Name: g.rec.Name, Source: g.source.name, Language: g.index.Language(), Catalog: g.catalog}
		for _, m := range g.members {
			lib.Members = append(lib.Members, m.name)
		}

		out = append(out, lib)
	}

	slices.SortFunc(out, func(x, y Library) int { return cmp.Compare(x.GroupID, y.GroupID) })

	return out
}
