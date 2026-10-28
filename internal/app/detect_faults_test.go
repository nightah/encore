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

func (b *lockedBuffer) has(s string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return strings.Contains(b.buf.String(), s)
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

		a.refingerprint(t, bedroom, diverged)

		assert.True(t, logs.has(`msg="splitting a library group"`))
		assert.Equal(t, shared, runtimeMembers(a), "nothing changed")
		assert.Equal(t, shared, storedMembers(t, a))
	})

	t.Run("recording why a member left", func(t *testing.T) {
		// The split-off member's group is saved, but a passing failure loses the reason on the group it left.
		faults.Inject(testkit.Fault{Op: testkit.Update, Table: "library_groups", When: "NEW.name = 'Lounge' AND NEW.state = 'likely_shared'"})
		t.Cleanup(faults.Clear)

		a.refingerprint(t, bedroom, diverged)

		assert.True(t, logs.has(`msg="splitting a library group"`))
		assert.Equal(t, storedMembers(t, a), runtimeMembers(a), "the running groups are those saved")
	})
}
