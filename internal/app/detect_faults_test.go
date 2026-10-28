package app

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
	"github.com/nightah/encore/internal/testkit"
)

// lockedBuffer collects the logs of a running app.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) has(s string) bool { return b.count(s) > 0 }

func (b *lockedBuffer) count(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return strings.Count(b.buf.String(), s)
}

// refingerprint hands inst a fingerprint, as a fresh read of its library would.
func (a *App) refingerprint(t *testing.T, inst *Instance, fp *kodi.LibraryFingerprint) {
	t.Helper()

	a.planMu.Lock()
	defer a.planMu.Unlock()

	a.fingerprinted(t.Context(), inst, fp)
}

func (a *App) printOf(inst *Instance) *kodi.LibraryFingerprint {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.prints[inst.id]
}

// storedMembers returns the members of each library group in the database, by group name.
func storedMembers(t *testing.T, a *App) map[string][]int64 {
	t.Helper()

	recs, err := a.DB().LibraryGroups().List(t.Context())
	require.NoError(t, err)

	out := map[string][]int64{}

	for _, r := range recs {
		ids, err := a.DB().LibraryGroups().Members(t.Context(), r.ID)
		require.NoError(t, err)

		out[r.Name] = ids
	}

	return out
}

// runtimeMembers returns the members of each running library group, by group name.
func runtimeMembers(a *App) map[string][]int64 {
	out := map[string][]int64{}

	for _, g := range a.Groups() {
		for _, m := range g.Members {
			out[g.Name] = append(out[g.Name], m.ID)
		}
	}

	return out
}

func TestLibraryGroupChangesThatCannotBeSaved(t *testing.T) {
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	loungeKodi, bedroomKodi := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})

	cfg, faults := faultyConfig(t, kodiConfig("Lounge", loungeKodi, ""), kodiConfig("Bedroom", bedroomKodi, ""))
	logs := &lockedBuffer{}

	a, err := newApp(t.Context(), cfg, slog.New(slog.NewTextHandler(logs, nil)), fastTuning())
	require.NoError(t, err)

	// Joining the two instances' groups updates the one that leads.
	faults.Inject(testkit.Fault{Op: testkit.Update, Table: "library_groups"})
	startApp(t, a)

	lounge, bedroom := a.instanceNamed(t, "Lounge"), a.instanceNamed(t, "Bedroom")

	require.Eventually(t, func() bool { return logs.has(`msg="grouping Kodi instances that share a library"`) }, eventually, 10*time.Millisecond)
	assert.Len(t, a.Groups(), 2, "the instances keep libraries of their own")
	assert.Equal(t, map[string][]int64{"Lounge": {lounge.id}, "Bedroom": {bedroom.id}}, storedMembers(t, a))

	// The next fingerprint, once the database works, joins them.
	faults.Clear()
	a.refingerprint(t, bedroom, a.printOf(bedroom))

	shared := map[string][]int64{"Lounge": {lounge.id, bedroom.id}}
	require.Equal(t, shared, runtimeMembers(a))
	require.Equal(t, shared, storedMembers(t, a))

	before, ok := a.groupOf("Lounge")
	require.True(t, ok)

	t.Run("saving fresh evidence", func(t *testing.T) {
		faults.Inject(testkit.Fault{Op: testkit.Update, Table: "library_groups"})
		t.Cleanup(faults.Clear)

		a.refingerprint(t, lounge, a.printOf(lounge))

		assert.True(t, logs.has(`msg="saving library group evidence"`))

		stored, err := a.DB().LibraryGroups().Get(t.Context(), before.ID)
		require.NoError(t, err)

		a.mu.RLock()
		running := lounge.group.rec.Evidence
		a.mu.RUnlock()

		assert.JSONEq(t, string(stored.Evidence), string(running), "the running group keeps the evidence that was saved")
	})

	diverged := &kodi.LibraryFingerprint{
		Movies:     []kodi.FingerprintItem{{ID: 1, File: "nfs://nas/elsewhere/1.mkv", DateAdded: "2025-06-01 09:00:00"}},
		Episodes:   []kodi.FingerprintItem{{ID: 1, File: "nfs://nas/elsewhere/s01e01.mkv", DateAdded: "2025-06-01 09:00:00"}},
		MovieTotal: 1, EpisodeTotal: 1,
	}
	for i := 2; i <= 30; i++ {
		diverged.Movies = append(diverged.Movies, kodi.FingerprintItem{ID: i, File: "nfs://nas/elsewhere/x.mkv", DateAdded: "2025-06-01 09:00:00"})
	}

	t.Run("splitting off a member", func(t *testing.T) {
		faults.Inject(testkit.Fault{Op: testkit.Insert, Table: "library_groups"})
		t.Cleanup(faults.Clear)

		before := logs.count(`msg="splitting a library group"`)
		a.refingerprint(t, bedroom, diverged)

		assert.Equal(t, before+1, logs.count(`msg="splitting a library group"`), "the failure is logged")
		assert.Equal(t, shared, runtimeMembers(a), "nothing changed")
		assert.Equal(t, shared, storedMembers(t, a))
	})

	t.Run("recording why a member left", func(t *testing.T) {
		// The split-off member's group is saved, but a passing failure loses the reason on the group it left.
		faults.Inject(testkit.Fault{Op: testkit.Update, Table: "library_groups", When: "NEW.name = 'Lounge' AND NEW.state = 'likely_shared'"})
		t.Cleanup(faults.Clear)

		before := logs.count(`msg="splitting a library group"`)
		a.refingerprint(t, bedroom, diverged)

		assert.Equal(t, before+1, logs.count(`msg="splitting a library group"`), "the failure is logged")
		assert.Equal(t, map[string][]int64{"Lounge": {lounge.id}, "Bedroom": {bedroom.id}}, storedMembers(t, a), "the member left in the database")
		assert.Equal(t, storedMembers(t, a), runtimeMembers(a), "the running groups are those saved")
	})
}

func TestAConfirmationThatCannotBeSavedLeavesTheGroupUnconfirmed(t *testing.T) {
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	loungeKodi, bedroomKodi := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})

	cfg, faults := faultyConfig(t, kodiConfig("Lounge", loungeKodi, ""), kodiConfig("Bedroom", bedroomKodi, ""))
	logs := &lockedBuffer{}

	a, err := newApp(t.Context(), cfg, slog.New(slog.NewTextHandler(logs, nil)), fastTuning())
	require.NoError(t, err)
	startApp(t, a)

	require.Eventually(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 2 }, eventually, 10*time.Millisecond)
	require.Eventually(t, func() bool { return a.instanceNamed(t, "Bedroom").Index.Len() == 31 }, eventually, 10*time.Millisecond)
	connected(t, bedroomKodi)

	faults.Inject(testkit.Fault{Op: testkit.Update, Table: "library_groups"})

	require.True(t, bedroomKodi.SetPlayState("episode", 7, fakekodi.PlayState{PlayCount: 1, LastPlayed: time.Now().Truncate(time.Second)}))
	require.Eventually(t, func() bool { return logs.has(`msg="recording library group confirmation"`) }, eventually, 10*time.Millisecond)

	g, _ := a.groupOf("Lounge")
	assert.Equal(t, store.GroupLikelyShared, g.State, "the running group isn't confirmed when the confirmation wasn't saved")

	for _, m := range g.Members {
		assert.False(t, m.Confirmed, m.Name)
	}

	stored, err := a.DB().LibraryGroups().Get(t.Context(), g.ID)
	require.NoError(t, err)
	assert.Equal(t, store.GroupLikelyShared, stored.State)
}

func TestResettingALibraryGroupReportsAFailedRebuild(t *testing.T) {
	loungeKodi := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	bedroomKodi := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})

	cfg, faults := faultyConfig(t)
	a, _ := runApp(t, cfg.DataDir, fastTuning(), kodiConfig("Lounge", loungeKodi, ""), kodiConfig("Bedroom", bedroomKodi, ""))
	lounge, bedroom := a.instanceNamed(t, "Lounge"), a.instanceNamed(t, "Bedroom")
	ctx := t.Context()

	_, err := a.GroupInstances(ctx, "Whole house", lounge.id, bedroom.id)
	require.NoError(t, err)

	// Saving the reset reads no instances; rebuilding the groups after it does.
	faults.Inject(testkit.Fault{Op: testkit.Read, Table: "kodi_instances"})

	err = a.ResetLibraryGroup(ctx, lounge.id)
	require.ErrorContains(t, err, "reconciling library groups: querying: "+testkit.ReadFaultMessage)

	// Trying again once the database works finishes the reset.
	faults.Clear()
	require.NoError(t, a.ResetLibraryGroup(ctx, lounge.id))

	assert.Equal(t, storedMembers(t, a), runtimeMembers(a))

	g, ok := a.groupOf("Lounge")
	require.True(t, ok)
	assert.False(t, g.Manual, "the instance is back with detection")
	assert.Equal(t, []string{"Lounge"}, memberNames(g))
}

func TestASplitThatFailsPartWayRunsTheGroupsItSaved(t *testing.T) {
	testCases := []struct {
		name    string
		leaving []string
		faults  []testkit.Fault
		want    []string
		// stored is the members of each group left in the database, by name.
		stored map[string][]string
	}{
		{
			name:    "saving the second member's own group",
			leaving: []string{"Bedroom", "Kitchen"},
			faults:  []testkit.Fault{{Op: testkit.Insert, Table: "library_groups", Nth: 2}},
			want:    []string{testkit.FaultMessage + " on INSERT library_groups"},
			stored:  map[string][]string{"Lounge": {"Lounge", "Kitchen"}, "Bedroom": {"Bedroom"}},
		},
		{
			name:    "recording the reason and then rebuilding the groups",
			leaving: []string{"Bedroom"},
			faults: []testkit.Fault{
				{Op: testkit.Update, Table: "library_groups", When: "NEW.name = 'Lounge'"},
				{Op: testkit.Read, Table: "kodi_instances"},
			},
			want:   []string{testkit.FaultMessage + " on UPDATE library_groups", "reconciling library groups: querying: " + testkit.ReadFaultMessage},
			stored: map[string][]string{"Lounge": {"Lounge", "Kitchen"}, "Bedroom": {"Bedroom"}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, faults := faultyConfig(t,
				config.KodiInstance{Name: "Lounge", URL: "http://127.0.0.1:1/jsonrpc"},
				config.KodiInstance{Name: "Bedroom", URL: "http://127.0.0.1:2/jsonrpc"},
				config.KodiInstance{Name: "Kitchen", URL: "http://127.0.0.1:3/jsonrpc"},
			)

			a, err := newApp(t.Context(), cfg, slog.New(slog.DiscardHandler), fastTuning())
			require.NoError(t, err)

			t.Cleanup(func() { assert.NoError(t, a.Close()) })

			ids := map[string]int64{}
			for _, name := range []string{"Lounge", "Bedroom", "Kitchen"} {
				ids[name] = a.instanceNamed(t, name).id
			}

			ctx := t.Context()

			a.planMu.Lock()
			defer a.planMu.Unlock()

			// Detection grouped all three.
			shared := store.LibraryGroup{Name: "Lounge", State: store.GroupLikelyShared, SourceInstanceID: ids["Lounge"], Evidence: (&evidence{Origin: originDetected}).encode()}
			require.NoError(t, a.DB().LibraryGroups().Save(ctx, &shared, []int64{ids["Lounge"], ids["Bedroom"], ids["Kitchen"]}))
			require.NoError(t, a.plan(ctx))
			require.Equal(t, map[string][]int64{"Lounge": {ids["Lounge"], ids["Bedroom"], ids["Kitchen"]}}, runtimeMembers(a))

			a.mu.RLock()
			g := a.instances[ids["Lounge"]].group
			a.mu.RUnlock()

			var leaving []*Instance
			for _, name := range tc.leaving {
				leaving = append(leaving, a.instanceNamed(t, name))
			}

			faults.Inject(tc.faults...)

			err = a.split(ctx, g, leaving, "the libraries differ")
			for _, want := range tc.want {
				require.ErrorContains(t, err, want)
			}

			faults.Clear()

			want := map[string][]int64{}
			for group, members := range tc.stored {
				for _, name := range members {
					want[group] = append(want[group], ids[name])
				}
			}

			assert.Equal(t, want, storedMembers(t, a), "the members saved before the failure left in the database")

			if len(tc.want) == 1 {
				assert.Equal(t, want, runtimeMembers(a), "and run in the groups they were saved in")
			}

			// Once the database works the running groups catch up with it.
			require.NoError(t, a.plan(ctx))
			assert.Equal(t, want, runtimeMembers(a))
		})
	}
}
