package app

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

// detachedInstance is a runtime for a fake that answers over HTTP without being run, enough to read items.
func detachedInstance(t *testing.T, a *App, name string, srv *fakekodi.Server) *Instance {
	t.Helper()

	rec := connectionRecord(t, srv)
	rec.Name, rec.WSPort = name, 0

	inst, err := a.newInstance(t.Context(), rec)
	require.NoError(t, err)

	return inst
}

func TestMirrors(t *testing.T) {
	shared := fakekodi.DefaultLibrary()
	separate := fakekodi.DefaultLibrary()
	separate.Movies[5].PlayCount = 9
	without := &fakekodi.Library{Movies: shared.Movies[:5]}

	hotFuzz := media.Ref{Kind: media.KindMovie, ID: 6}

	cases := []struct {
		name  string
		other *fakekodi.Library
		// stale makes the change m reported differ from what m shows now, as when a later write landed between the reads.
		stale  bool
		closeM bool
		closeP bool
		// immediate means the verdict needs no grace period.
		immediate bool
		want      verdict
	}{
		{name: "shared database shows the change", other: shared, immediate: true, want: mirrored},
		{name: "item missing from the other library", other: without, immediate: true, want: missed},
		{name: "other library disagrees after the grace period", other: separate, want: missed},
		{name: "a later write settles within the grace period", other: shared, stale: true, want: mirrored},
		{name: "other Kodi unreachable", other: shared, closeP: true, want: inconclusive},
		{name: "changed Kodi unreachable after the grace period", other: shared, stale: true, closeM: true, want: inconclusive},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newBareApp(t)
			a.tune.confirmGrace = 20 * time.Millisecond
			if c.immediate {
				a.tune.confirmGrace = time.Hour
			}

			msrv, psrv := fakekodi.New(t, fakekodi.Options{Library: shared}), fakekodi.New(t, fakekodi.Options{Library: c.other})
			m, p := detachedInstance(t, a, "Lounge", msrv), detachedInstance(t, a, "Bedroom", psrv)

			after, err := m.Backend.Item(t.Context(), hotFuzz)
			require.NoError(t, err)

			if c.stale {
				after.PlayCount++
			}

			if c.closeM {
				msrv.Close()
			}

			if c.closeP {
				psrv.Close()
			}

			ctx, cancel := context.WithTimeout(t.Context(), eventually)
			defer cancel()

			assert.Equal(t, c.want, a.mirrors(ctx, m, p, hotFuzz, &after))
		})
	}
}

func TestMirrorsRereadsTheOtherMemberAfterTheGracePeriod(t *testing.T) {
	hotFuzz := media.Ref{Kind: media.KindMovie, ID: 6}

	for _, c := range []struct {
		name  string
		again *fakekodi.Error
		want  verdict
	}{
		{"deleted meanwhile", &fakekodi.Error{Code: fakekodi.CodeNotFound, Message: "Not found."}, missed},
		{"unreachable meanwhile", &fakekodi.Error{Code: fakekodi.CodeUnavailable, Message: "busy"}, inconclusive},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newBareApp(t)
			a.tune.confirmGrace = 10 * time.Millisecond

			opts := fakekodi.Options{Version: [3]int{13, 1000, 0}}
			psrv := fakekodi.New(t, opts)
			m, p := detachedInstance(t, a, "Lounge", fakekodi.New(t, opts)), detachedInstance(t, a, "Bedroom", psrv)

			var reads atomic.Int32

			psrv.Handle("VideoLibrary.GetMovieDetails", func(json.RawMessage) (any, *fakekodi.Error) {
				if reads.Add(1) > 1 {
					return nil, c.again
				}

				return map[string]any{"moviedetails": map[string]any{"movieid": 6, "label": "Hot Fuzz", "title": "Hot Fuzz", "playcount": 7}}, nil
			})

			after, err := m.Backend.Item(t.Context(), hotFuzz)
			require.NoError(t, err)

			assert.Equal(t, c.want, a.mirrors(t.Context(), m, p, hotFuzz, &after))
			assert.EqualValues(t, 2, reads.Load())
		})
	}
}

func TestMirrorsGivesUpWhenCancelledDuringTheGracePeriod(t *testing.T) {
	a := newBareApp(t)
	a.tune.confirmGrace = time.Hour

	lib := fakekodi.DefaultLibrary()
	m := detachedInstance(t, a, "Lounge", fakekodi.New(t, fakekodi.Options{Library: lib}))
	p := detachedInstance(t, a, "Bedroom", fakekodi.New(t, fakekodi.Options{Library: lib}))
	ref := media.Ref{Kind: media.KindMovie, ID: 6}

	after, err := m.Backend.Item(t.Context(), ref)
	require.NoError(t, err)

	after.PlayCount++

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	assert.Equal(t, inconclusive, a.mirrors(ctx, m, p, ref, &after))
}

func TestConfirmIgnoresChangesThatProveNothing(t *testing.T) {
	lib := fakekodi.DefaultLibrary()
	hotFuzz := media.Ref{Kind: media.KindMovie, ID: 6}
	one := 1

	cases := []struct {
		name   string
		o      func(now media.Item) observation
		closeM bool
	}{
		{name: "the changed Kodi can't be read", o: func(media.Item) observation { return observation{ref: hotFuzz, playCount: &one} }, closeM: true},
		{name: "no play count and nothing known before", o: func(media.Item) observation { return observation{ref: hotFuzz} }},
		{name: "no play count and nothing changed", o: func(now media.Item) observation { return observation{ref: hotFuzz, prior: now, havePrior: true} }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newBareApp(t)
			msrv, psrv := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})
			m, p := detachedInstance(t, a, "Lounge", msrv), detachedInstance(t, a, "Bedroom", psrv)
			m.group = &group{members: []*Instance{m, p}}

			now, err := m.Backend.Item(t.Context(), hotFuzz)
			require.NoError(t, err)

			if c.closeM {
				msrv.Close()
			}

			o := c.o(now)
			a.confirm(t.Context(), m, &o)

			assert.Empty(t, psrv.CallsTo("VideoLibrary.GetMovieDetails"), "the other members aren't asked")
		})
	}
}

func TestAConfirmationForAReplacedGroupIsDropped(t *testing.T) {
	a := newBareApp(t)
	lounge, bedroom := &Instance{id: 1, name: "Lounge"}, &Instance{id: 2, name: "Bedroom"}
	g := &group{id: 7, members: []*Instance{lounge, bedroom}}
	lounge.group, bedroom.group = g, g

	require.NoError(t, a.recordConfirmation(t.Context(), lounge, g, "Hot Fuzz", nil, []*Instance{bedroom}))

	groups, err := a.DB().LibraryGroups().List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, groups, "a group no longer running isn't split")
	assert.Empty(t, g.ev.Reason)
}

func TestSamePlayState(t *testing.T) {
	played := time.Date(2026, 10, 27, 20, 0, 0, 0, time.UTC)
	base := media.Item{PlayCount: 1, Resume: time.Minute, LastPlayed: played}

	same := base
	same.LastPlayed = played.In(time.FixedZone("AEDT", 11*60*60))
	assert.True(t, samePlayState(&base, &same), "the same instant in another zone")

	for name, change := range map[string]func(*media.Item){
		"play count":  func(it *media.Item) { it.PlayCount++ },
		"resume":      func(it *media.Item) { it.Resume = 0 },
		"last played": func(it *media.Item) { it.LastPlayed = played.Add(time.Second) },
	} {
		other := base
		change(&other)
		assert.False(t, samePlayState(&base, &other), name)
	}
}

func TestRecheckOnRestart(t *testing.T) {
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	// Kodi 22 (API 13.1000) and later name their database.
	piers := func(db string) *fakekodi.Server {
		return fakekodi.New(t, fakekodi.Options{Version: [3]int{13, 1000, 0}, Library: lib, DatabaseName: db})
	}
	lounge, bedroom := piers("MyVideos149"), piers("MyVideos149")
	dir := t.TempDir()

	a, stop := runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
	require.Eventually(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 2 }, eventually, 10*time.Millisecond)

	g, _ := a.groupOf("Lounge")
	stop()

	sampled := func(a *App) time.Time {
		rec, err := a.DB().LibraryGroups().Get(t.Context(), g.ID)
		if err != nil {
			return time.Time{}
		}

		if ev := decodeEvidence(rec.Evidence); ev.Fingerprint != nil {
			return ev.Fingerprint.At
		}

		return time.Time{}
	}

	restarted := time.Now()

	// Still sharing after a restart: the members' fingerprints refresh the evidence.
	a, stop = runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
	require.Eventually(t, func() bool { return sampled(a).After(restarted) }, eventually, 10*time.Millisecond)

	again, _ := a.groupOf("Lounge")
	assert.Equal(t, g.ID, again.ID)
	assert.Equal(t, "60 of 60 sampled items match", again.Evidence)
	stop()

	// The bedroom Kodi was given a database of its own, with the same files.
	own := piers("MyVideos149_bedroom")

	a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", own, ""))
	require.Eventually(t, func() bool { return len(a.Groups()) == 2 }, eventually, 10*time.Millisecond)

	split, _ := a.groupOf("Bedroom")
	assert.Equal(t, store.GroupSolo, split.State)
	assert.Contains(t, split.Reason, "uses database MyVideos149")
	assert.Contains(t, split.Reason, "MyVideos149_bedroom")
}
