package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/library"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

// kodis carries every test's fake Kodi instances in memory, so tests can run in synctest bubbles.
var kodis = fakekodi.NewNetwork()

// newKodi starts a fake Kodi instance on kodis.
func newKodi(t *testing.T, opts fakekodi.Options) *fakekodi.Server {
	t.Helper()

	return kodis.New(t, opts)
}

// onFakes routes tune's Kodi traffic to the fakes on kodis.
func onFakes(tune tuning) tuning {
	tune.kodiClient = kodis.Client()

	return tune
}

func fastTuning() tuning {
	t := defaultTuning()
	t.fingerprintRetry = 50 * time.Millisecond
	t.confirmDebounce = 50 * time.Millisecond
	t.confirmGrace = 200 * time.Millisecond
	t.failoverInterval = 50 * time.Millisecond
	t.pingTimeout = time.Second
	t.snapshotEvery = 0
	t.snapshotCheck = 20 * time.Millisecond
	t.catalog = library.Options{Debounce: 20 * time.Millisecond}

	return onFakes(t)
}

// testLibrary has n movies and n episodes of one show. Libraries built with the same offset and date added look like one shared database; a different offset or date added looks like a separate database built from the same files.
func testLibrary(n, offset int, added string) *fakekodi.Library {
	lib := &fakekodi.Library{TVShows: []fakekodi.TVShow{{TVShowID: offset + 1, Title: "Shared Show", Label: "Shared Show", DateAdded: added}}}

	for i := 1; i <= n; i++ {
		lib.Movies = append(lib.Movies, fakekodi.Movie{
			MovieID: offset + i, Title: fmt.Sprintf("Film Number %d", i), Label: fmt.Sprintf("Film Number %d", i), Year: 2000 + i,
			File: fmt.Sprintf("nfs://nas/movies/film-%d.mkv", i), DateAdded: added,
		})
		lib.Episodes = append(lib.Episodes, fakekodi.Episode{
			EpisodeID: offset + i, TVShowID: offset + 1, ShowTitle: "Shared Show", Season: 1, Episode: i, Title: fmt.Sprintf("Part %d", i),
			File: fmt.Sprintf("nfs://nas/tv/show-s01e%02d.mkv", i), DateAdded: added,
		})
	}

	return lib
}

func kodiConfig(name string, srv *fakekodi.Server, group string) config.KodiInstance {
	return config.KodiInstance{Name: name, URL: srv.URL(), WebSocketURL: srv.WebSocketURL(), LibraryGroup: group}
}

// runApp's returned func stops and closes the app; cleanup does so too.
func runApp(t *testing.T, dataDir string, tune tuning, kodis ...config.KodiInstance) (*App, func()) {
	t.Helper()

	cfg := config.Defaults()
	cfg.DataDir = dataDir
	cfg.Kodi = kodis

	a := openApp(t, &cfg, tune)

	return a, startApp(t, a)
}

func openApp(t *testing.T, cfg *config.Config, tune tuning) *App {
	t.Helper()

	log := slog.New(slog.DiscardHandler)
	if os.Getenv("ENCORE_TEST_LOG") != "" {
		log = slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	a, err := newApp(t.Context(), cfg, log, tune)
	require.NoError(t, err)

	return a
}

// startApp runs a; the returned func stops and closes it, as cleanup does.
func startApp(t *testing.T, a *App) func() {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = a.Run(ctx)
	}()

	stop := sync.OnceFunc(func() {
		cancel()
		<-done
		assert.NoError(t, a.Close())
	})
	t.Cleanup(stop)

	return stop
}

func (a *App) instanceNamed(t *testing.T, name string) *Instance {
	t.Helper()

	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, inst := range a.instances {
		if inst.name == name {
			return inst
		}
	}

	t.Fatalf("no instance %q", name)

	return nil
}

func (a *App) groupOf(name string) (GroupStatus, bool) {
	for _, g := range a.Groups() {
		for _, m := range g.Members {
			if m.Name == name {
				return g, true
			}
		}
	}

	return GroupStatus{}, false
}

func (a *App) fingerprintedAll(names ...string) bool {
	for _, name := range names {
		g, ok := a.groupOf(name)
		if !ok {
			return false
		}

		for _, m := range g.Members {
			if m.Name == name && !m.Fingerprinted {
				return false
			}
		}
	}

	return true
}

// connected requires, once the bubble has settled, that the app listens to a fake's notifications.
func connected(t *testing.T, srv *fakekodi.Server) {
	t.Helper()

	settled(t, func() bool { return srv.WebSocketClients() > 0 }, "the app listens to the Kodi instance")
}

// librarySyncs counts the library listings a fake answered, leaving out fingerprint samples.
func librarySyncs(srv *fakekodi.Server) int {
	n := 0

	for _, c := range srv.CallsTo("VideoLibrary.GetMovies") {
		if !strings.Contains(string(c.Params), `"properties":["file","dateadded"]`) {
			n++
		}
	}

	return n
}

// scanned reports a finished library scan, then requires done once the catalog's debounce has passed.
func scanned(t *testing.T, srv *fakekodi.Server, debounce time.Duration, done func() bool) {
	t.Helper()

	connected(t, srv)
	srv.Notify("VideoLibrary.OnScanFinished", nil)
	after(t, debounce, done, "the scan is synced")
}

func memberNames(g GroupStatus) []string {
	out := make([]string, len(g.Members))
	for i, m := range g.Members {
		out[i] = m.Name
	}

	return out
}

func hasTitle(c *library.Catalog, title string) bool {
	for _, it := range c.Items(media.KindMovie) {
		if it.Title == title {
			return true
		}
	}

	return false
}

func TestCompareFingerprints(t *testing.T) {
	sample := func(n, offset int, added string) *kodi.LibraryFingerprint {
		fp := &kodi.LibraryFingerprint{}
		for i := 1; i <= n; i++ {
			fp.Movies = append(fp.Movies, kodi.FingerprintItem{ID: offset + i, File: fmt.Sprintf("/m/%d.mkv", i), DateAdded: added})
		}

		return fp
	}

	tune := defaultTuning()

	same := compareFingerprints(sample(200, 0, "2024-01-01 10:00:00"), sample(200, 0, "2024-01-01 10:00:00"))
	assert.True(t, same.match(&tune))
	assert.Equal(t, "200 of 200 sampled items match", same.summary())

	otherIDs := compareFingerprints(sample(200, 0, "2024-01-01 10:00:00"), sample(200, 5, "2024-01-01 10:00:00"))
	assert.False(t, otherIDs.match(&tune), "the same files under different IDs are a different database")
	assert.True(t, otherIDs.meaningful(&tune))

	otherDates := compareFingerprints(sample(200, 0, "2024-01-01 10:00:00"), sample(200, 0, "2024-01-02 10:00:00"))
	assert.False(t, otherDates.match(&tune), "the same files added at other times are a different database")

	nearly := sample(200, 0, "2024-01-01 10:00:00")
	nearly.Movies = nearly.Movies[:195]
	assert.True(t, compareFingerprints(sample(200, 0, "2024-01-01 10:00:00"), nearly).match(&tune), "a scan in progress may leave a few items behind")

	nearly.Movies = nearly.Movies[:190]
	assert.True(t, compareFingerprints(sample(200, 0, "2024-01-01 10:00:00"), nearly).match(&tune), "exactly the minimum agreement is enough")

	nearly.Movies = nearly.Movies[:180]
	assert.False(t, compareFingerprints(sample(200, 0, "2024-01-01 10:00:00"), nearly).match(&tune))
	assert.False(t, compareFingerprints(nearly, sample(200, 0, "2024-01-01 10:00:00")).match(&tune), "the larger sample counts, whichever side it's on")

	assert.True(t, compareFingerprints(sample(minFingerprintItems, 0, "2024-01-01 10:00:00"), sample(minFingerprintItems, 0, "2024-01-01 10:00:00")).meaningful(&tune), "the minimum sample is enough to tell")

	small := compareFingerprints(sample(10, 0, "2024-01-01 10:00:00"), sample(10, 0, "2024-01-01 10:00:00"))
	assert.False(t, small.meaningful(&tune), "too few items to tell")
	assert.False(t, small.match(&tune))

	empty := compareFingerprints(&kodi.LibraryFingerprint{}, &kodi.LibraryFingerprint{})
	assert.False(t, empty.match(&tune), "empty libraries never group")

	noFiles := sample(50, 0, "")
	for i := range noFiles.Movies {
		noFiles.Movies[i].File = ""
	}

	assert.False(t, compareFingerprints(noFiles, noFiles).match(&tune), "items without a file path prove nothing")

	a, b := sample(200, 0, "2024-01-01 10:00:00"), sample(200, 0, "2024-01-01 10:00:00")
	a.DatabaseName, b.DatabaseName = "MyVideos149", "MyVideos149"
	assert.True(t, compareFingerprints(a, b).match(&tune), "matching names are only a prerequisite")

	b.DatabaseName = "MyVideos149_bedroom"
	vetoed := compareFingerprints(a, b)
	assert.True(t, vetoed.vetoed())
	assert.False(t, vetoed.match(&tune), "different database names rule sharing out")

	b.DatabaseName = ""
	assert.True(t, compareFingerprints(a, b).match(&tune), "a Kodi that can't name its database doesn't veto")
}

func TestSharedLibraryIsDetectedAndConfirmed(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge, bedroom := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})
		tune := onFakes(defaultTuning())

		a, _ := runApp(t, t.TempDir(), tune, kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

		var g GroupStatus

		settled(t, func() bool {
			var ok bool
			g, ok = a.groupOf("Lounge")

			return ok && len(g.Members) == 2
		}, "matching fingerprints group the instances as soon as both are sampled")

		assert.Equal(t, store.GroupLikelyShared, g.State)
		assert.Equal(t, "60 of 60 sampled items match", g.Evidence)
		assert.False(t, g.Manual)
		assert.Len(t, a.Groups(), 1)
		assert.Equal(t, "Lounge", g.Name, "the instance created first leads the group")
		assert.Equal(t, a.instanceNamed(t, "Lounge").id, g.SourceID)

		a.mu.RLock()
		tl, tb := a.instanceNamed(t, "Lounge").target(), a.instanceNamed(t, "Bedroom").target()
		a.mu.RUnlock()

		assert.Same(t, tl.Matcher, tb.Matcher)
		assert.Same(t, tl.Library, tb.Library)
		assert.NotSame(t, tl.Backend, tb.Backend, "each member plays on its own Kodi")

		libs := a.Libraries()
		require.Len(t, libs, 1)
		assert.ElementsMatch(t, []string{"Lounge", "Bedroom"}, libs[0].Members)
		assert.Equal(t, 31, a.instanceNamed(t, "Bedroom").Index.Len(), "the group's index serves every member")

		// Watching on the bedroom Kodi changes the shared row, which the lounge Kodi reads too.
		require.Positive(t, bedroom.WebSocketClients())
		require.True(t, bedroom.SetPlayState("episode", 7, fakekodi.PlayState{PlayCount: 1, LastPlayed: time.Now().Truncate(time.Second)}))

		after(t, tune.confirmDebounce-time.Millisecond, func() bool { g, _ = a.groupOf("Lounge"); return g.State == store.GroupLikelyShared },
			"the members are compared once playback notifications settle")
		after(t, time.Millisecond, func() bool { g, _ = a.groupOf("Lounge"); return g.State == store.GroupConfirmedShared },
			"a mirrored change confirms the group without waiting out the grace period")

		assert.Equal(t, `a change to “Part 7” on Bedroom was seen on Lounge`, g.Evidence)

		for _, m := range g.Members {
			assert.True(t, m.Confirmed, m.Name)
		}

		stored, err := a.DB().LibraryGroups().Get(t.Context(), g.ID)
		require.NoError(t, err)
		assert.Equal(t, store.GroupConfirmedShared, stored.State)
	})
}

// Each changed item is compared once its own notifications settle: a later change to the same item restarts its wait, and one item settling doesn't bring another forward.
func TestChangesAreComparedOnceEachItemSettles(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge, bedroom := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})
		tune := onFakes(defaultTuning())

		a, _ := runApp(t, t.TempDir(), tune, kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
		settled(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 2 })
		connected(t, bedroom)

		// The catalog reads changed items from the group's source, the lounge Kodi, so the bedroom Kodi is asked for an item only to compare a change made there.
		reads := func(id int) int {
			n := 0

			for _, c := range bedroom.CallsTo("VideoLibrary.GetEpisodeDetails") {
				var p struct {
					EpisodeID int `json:"episodeid"`
				}

				require.NoError(t, json.Unmarshal(c.Params, &p))

				if p.EpisodeID == id {
					n++
				}
			}

			return n
		}

		play := func(id, count int) {
			t.Helper()
			require.True(t, bedroom.SetPlayState("episode", id, fakekodi.PlayState{PlayCount: count, LastPlayed: time.Now().Truncate(time.Second)}))
		}

		const gap = 500 * time.Millisecond

		play(7, 1)
		time.Sleep(gap)
		play(8, 1)
		time.Sleep(gap)
		play(7, 2)

		// Episode 8 changed once, gap after the first change to episode 7.
		after(t, tune.confirmDebounce-gap-time.Millisecond, func() bool { return reads(7) == 0 && reads(8) == 0 }, "nothing is compared while notifications are still arriving")
		after(t, time.Millisecond, func() bool { return reads(8) > 0 }, "episode 8 is compared once its notification settles")
		assert.Zero(t, reads(7), "episode 7 changed again since, so it isn't compared yet")

		after(t, gap-time.Millisecond, func() bool { return reads(7) == 0 }, "the second change to episode 7 restarted its wait")
		after(t, time.Millisecond, func() bool { return reads(7) > 0 }, "episode 7 is compared once its last change settles")
	})
}

func TestSeparateLibrariesOfTheSameFilesStayApart(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lounge := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
		bedroom := newKodi(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})
		empty1, empty2 := newKodi(t, fakekodi.Options{Library: &fakekodi.Library{}}), newKodi(t, fakekodi.Options{Library: &fakekodi.Library{}})

		a, _ := runApp(t, t.TempDir(), fastTuning(),
			kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""), kodiConfig("Garage", empty1, ""), kodiConfig("Shed", empty2, ""))

		settled(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom", "Garage", "Shed") })

		// Give a mistaken merge time to show.
		time.Sleep(time.Minute)
		synctest.Wait()

		groups := a.Groups()
		require.Len(t, groups, 4)

		for _, g := range groups {
			assert.Len(t, g.Members, 1)
			assert.Equal(t, store.GroupSolo, g.State)
		}
	})
}

func TestUnmirroredChangeSplitsTheGroup(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		// Two databases built identically from the same files fool the fingerprint, but not playback.
		lounge := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
		bedroom := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
		dir := t.TempDir()

		tune := fastTuning()
		a, stop := runApp(t, dir, tune, kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

		settled(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 2 })
		settled(t, func() bool { return hasTitle(a.instanceNamed(t, "Lounge").Catalog, "Film Number 3") })

		shared, _ := a.groupOf("Lounge")

		connected(t, lounge)
		require.True(t, lounge.SetPlayState("movie", 3, fakekodi.PlayState{Resume: 1200, LastPlayed: time.Now().Truncate(time.Second)}))

		// The bedroom Kodi gets the grace period to show the change before it's split off.
		after(t, tune.confirmDebounce+tune.confirmGrace-time.Millisecond, func() bool { return len(a.Groups()) == 1 }, "a member isn't split off before its grace period ends")
		after(t, time.Millisecond, func() bool { return len(a.Groups()) == 2 }, "a member that never showed the change is split off")

		for _, name := range []string{"Lounge", "Bedroom"} {
			g, ok := a.groupOf(name)
			require.True(t, ok)
			assert.Equal(t, store.GroupSolo, g.State, name)
			assert.Equal(t, name, g.Name)
			assert.Equal(t, `a change to “Film Number 3” on Lounge wasn't seen on Bedroom`, g.Reason, name)
		}

		kept, _ := a.groupOf("Lounge")
		assert.Equal(t, shared.ID, kept.ID, "the member that missed the change leaves; the one that made it keeps the group")

		a.mu.RLock()
		assert.NotSame(t, a.instanceNamed(t, "Lounge").Index, a.instanceNamed(t, "Bedroom").Index)
		a.mu.RUnlock()

		// The split survives a restart, when the fingerprints match again.
		stop()

		a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

		settled(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom") })
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Len(t, a.Groups(), 2)
	})
}

func TestDivergedFingerprintSplitsTheGroup(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge, bedroom := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})
		dir := t.TempDir()

		a, stop := runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
		settled(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 2 })
		stop()

		// The bedroom Kodi was pointed at a database of its own while Encore was down.
		rebuilt := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2025-06-01 09:00:00")})

		a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", rebuilt, ""))
		settled(t, func() bool { return len(a.Groups()) == 2 })

		g, _ := a.groupOf("Bedroom")
		assert.Equal(t, store.GroupSolo, g.State)
		assert.Contains(t, g.Reason, "no longer agree: 0 of 60 sampled items match")
	})
}

func TestSourceFailoverAndFailback(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge, bedroom := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})

		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, "upstairs"), kodiConfig("Bedroom", bedroom, "upstairs"))
		loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id
		catalog := a.instanceNamed(t, "Bedroom").Catalog

		settled(t, func() bool { return catalog.Stats().Counts[media.KindMovie] == 30 })

		g, _ := a.groupOf("Lounge")
		assert.Equal(t, loungeID, g.SourceID)
		assert.Equal(t, loungeID, g.ActiveID)

		// With a shared database only the Kodi that scanned reports it, and the source syncs the change.
		connected(t, bedroom)
		bedroom.Update(func(l *fakekodi.Library) {
			l.Movies = append(l.Movies, fakekodi.Movie{MovieID: 400, Title: "Bedroom Scan", Label: "Bedroom Scan", File: "nfs://nas/movies/scan.mkv", DateAdded: time.Now().Format(fakekodi.TimeLayout)})
		})
		scanned(t, bedroom, fastTuning().catalog.Debounce, func() bool { return hasTitle(catalog, "Bedroom Scan") })
		assert.Zero(t, librarySyncs(bedroom), "the bedroom Kodi isn't asked while the source is up")

		// The source goes away; the bedroom Kodi takes over syncing without anything being woken.
		lounge.Close()

		settled(t, func() bool { g, _ = a.groupOf("Lounge"); return g.ActiveID == bedroomID })
		assert.Equal(t, loungeID, g.SourceID, "the chosen source doesn't change")

		// A scan reported by any member is synced from the active one.
		bedroom.Update(func(l *fakekodi.Library) {
			l.Movies = append(l.Movies, fakekodi.Movie{MovieID: 500, Title: "Brand New Film", Label: "Brand New Film", File: "nfs://nas/movies/new.mkv", DateAdded: time.Now().Format(fakekodi.TimeLayout)})
		})
		scanned(t, bedroom, fastTuning().catalog.Debounce, func() bool { return hasTitle(catalog, "Brand New Film") })
		assert.Empty(t, a.Groups()[0].Reason)

		// The source returns and takes over again at the next check, which pings it.
		lounge.Restart(t)

		after(t, fastTuning().failoverInterval, func() bool { g, _ = a.groupOf("Lounge"); return g.ActiveID == loungeID })
	})
}

func TestConfiguredGroups(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		shared := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge := newKodi(t, fakekodi.Options{Library: shared})
		bedroom := newKodi(t, fakekodi.Options{Library: shared})
		// The study reads the same database but is told to keep its own library; the kitchen's library is different but configured into the group.
		study := newKodi(t, fakekodi.Options{Library: shared})
		kitchen := newKodi(t, fakekodi.Options{})
		dir := t.TempDir()

		kodis := []config.KodiInstance{
			kodiConfig("Lounge", lounge, "upstairs"), kodiConfig("Kitchen", kitchen, "upstairs"),
			kodiConfig("Study", study, "none"), kodiConfig("Bedroom", bedroom, ""),
		}

		a, stop := runApp(t, dir, fastTuning(), kodis...)

		settled(t, func() bool { return a.fingerprintedAll("Lounge", "Kitchen", "Study", "Bedroom") })
		time.Sleep(time.Minute)
		synctest.Wait()

		up, _ := a.groupOf("Lounge")
		assert.Equal(t, "upstairs", up.Name)
		assert.ElementsMatch(t, []string{"Lounge", "Kitchen"}, memberNames(up))
		assert.Equal(t, store.GroupManual, up.State)
		assert.True(t, up.Manual)
		assert.True(t, up.ConfigManaged)
		assert.Equal(t, "grouped by the config file", up.Evidence)
		assert.Equal(t, a.instanceNamed(t, "Lounge").id, up.SourceID, "the first configured member is the source")

		st, _ := a.groupOf("Study")
		assert.Len(t, st.Members, 1)
		assert.Equal(t, store.GroupSolo, st.State)
		assert.True(t, st.ConfigManaged)
		assert.Equal(t, "kept separate by the config file", st.Evidence)

		// The bedroom shares the lounge's database, but the lounge's group is manual, so detection leaves both alone.
		bd, _ := a.groupOf("Bedroom")
		assert.Len(t, bd.Members, 1)
		assert.False(t, bd.Manual)

		_, err := a.GroupInstances(t.Context(), "", a.instanceNamed(t, "Study").id, a.instanceNamed(t, "Bedroom").id)
		require.ErrorIs(t, err, ErrManagedByConfig)
		require.ErrorIs(t, a.SplitInstance(t.Context(), a.instanceNamed(t, "Kitchen").id), ErrManagedByConfig)
		require.ErrorIs(t, a.ResetLibraryGroup(t.Context(), a.instanceNamed(t, "Study").id), ErrManagedByConfig)

		// The source of a configured group may still be chosen, and groups keep their IDs across restarts.
		require.NoError(t, a.SetGroupSource(t.Context(), up.ID, a.instanceNamed(t, "Kitchen").id))
		stop()

		kodis[2].LibraryGroup = "" // the study is handed back to detection, and joins the bedroom
		a, _ = runApp(t, dir, fastTuning(), kodis...)

		again, _ := a.groupOf("Lounge")
		assert.Equal(t, up.ID, again.ID)
		assert.Equal(t, a.instanceNamed(t, "Kitchen").id, again.SourceID)

		settled(t, func() bool { g, _ := a.groupOf("Study"); return len(g.Members) == 2 })

		g, _ := a.groupOf("Study")
		assert.ElementsMatch(t, []string{"Study", "Bedroom"}, memberNames(g))
		assert.Equal(t, store.GroupLikelyShared, g.State)
	})
}

func TestUserGroupChoices(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lounge := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
		bedroom := newKodi(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})

		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
		loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id
		ctx := t.Context()

		settled(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom") })
		require.Len(t, a.Groups(), 2)

		// The user knows better than the fingerprint.
		id, err := a.GroupInstances(ctx, "Whole house", loungeID, bedroomID)
		require.NoError(t, err)

		g, _ := a.groupOf("Lounge")
		assert.Equal(t, id, g.ID)
		assert.Equal(t, "Whole house", g.Name)
		assert.Equal(t, store.GroupManual, g.State)
		assert.Equal(t, "grouped by the user", g.Evidence)
		assert.Equal(t, loungeID, g.SourceID)
		assert.Len(t, g.Members, 2)

		require.NoError(t, a.SetGroupSource(ctx, id, bedroomID))
		settled(t, func() bool { g, _ = a.groupOf("Lounge"); return g.ActiveID == bedroomID })
		assert.Equal(t, bedroomID, g.SourceID)
		require.ErrorIs(t, a.SetGroupSource(ctx, id, 999), store.ErrInvalid)
		require.ErrorIs(t, a.SetGroupSource(ctx, 999, bedroomID), store.ErrNotFound)

		again, err := a.GroupInstances(ctx, "Whole house", bedroomID, loungeID)
		require.NoError(t, err)
		assert.Equal(t, id, again)

		require.NoError(t, a.SplitInstance(ctx, bedroomID))
		require.Len(t, a.Groups(), 2)

		bd, _ := a.groupOf("Bedroom")
		assert.True(t, bd.Manual)
		assert.Equal(t, "kept separate by the user", bd.Evidence)

		require.NoError(t, a.ResetLibraryGroup(ctx, bedroomID))

		bd, _ = a.groupOf("Bedroom")
		assert.False(t, bd.Manual)
		assert.Len(t, a.Groups(), 2, "the libraries differ, so detection keeps them apart")

		_, err = a.GroupInstances(ctx, "")
		require.ErrorIs(t, err, store.ErrInvalid)
		require.ErrorIs(t, a.SplitInstance(ctx, 999), store.ErrNotFound)
	})
}

func TestSnapshotWarmStart(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		srv := newKodi(t, fakekodi.Options{})
		dir := t.TempDir()

		tune := fastTuning()
		a, stop := runApp(t, dir, tune, kodiConfig("Lounge", srv, ""))
		g, _ := a.groupOf("Lounge")
		file := filepath.Join(dir, "blobs", "library", fmt.Sprintf("%d.json.gz", g.ID))

		settled(t, func() bool { _, err := os.Stat(file); return err != nil }, "nothing is saved before the first check")
		after(t, tune.snapshotCheck, func() bool { _, err := os.Stat(file); return err == nil }, "the synced library is saved at the next check")
		stop()

		// With Kodi off, the restarted app still knows the library.
		srv.Close()

		a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
		inst := a.instanceNamed(t, "Lounge")

		settled(t, func() bool { return inst.Index.Len() > 0 })
		assert.True(t, hasTitle(inst.Catalog, "Hot Fuzz"))
		assert.False(t, inst.Catalog.Stats().LastSync.IsZero(), "the snapshot's sync time comes back with it")
	})
}

func TestUnsuitableSnapshotsAreSkipped(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		srv := newKodi(t, fakekodi.Options{})
		dir := t.TempDir()

		a, stop := runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
		g, _ := a.groupOf("Lounge")
		key := snapshotKey(g.ID)
		file := filepath.Join(dir, "blobs", filepath.FromSlash(key))

		after(t, fastTuning().snapshotCheck, func() bool { _, err := os.Stat(file); return err == nil })
		stop()
		srv.Close()

		// A snapshot taken while another Kodi instance was the source describes that instance's library.
		blobs, err := blobstore.NewLocal(filepath.Join(dir, "blobs"))
		require.NoError(t, err)

		var snap library.Snapshot
		require.NoError(t, blobstore.LoadJSON(t.Context(), blobs, key, &snap))
		require.NotEmpty(t, snap.Items)

		snap.Instance = "Study"
		require.NoError(t, blobstore.SaveJSON(t.Context(), blobs, key, snap))

		a, stop = runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Zero(t, a.instanceNamed(t, "Lounge").Index.Len())
		stop()

		// A missing or damaged snapshot just means a cold start.
		require.NoError(t, os.WriteFile(file, []byte("not gzip"), 0o600))

		a, stop = runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Zero(t, a.instanceNamed(t, "Lounge").Index.Len())
		stop()

		require.NoError(t, os.Remove(file))

		a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Zero(t, a.instanceNamed(t, "Lounge").Index.Len())
	})
}

func TestAThirdSharingInstanceJoinsTheGroup(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge, bedroom, study := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})

		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""), kodiConfig("Study", study, ""))

		settled(t, func() bool { return len(a.Groups()) == 1 && len(a.Groups()[0].Members) == 3 })

		// Whichever pair sampled first formed the group; the third joins it without taking it over.
		g := a.Groups()[0]
		assert.Equal(t, a.instanceNamed(t, g.Name).id, g.SourceID, "the group is named after its source")
		assert.Equal(t, store.GroupLikelyShared, g.State)
		assert.Equal(t, "60 of 60 sampled items match", g.Evidence)
		assert.ElementsMatch(t, []string{"Lounge", "Bedroom", "Study"}, memberNames(g))
	})
}

func TestAChangeMostMembersMissedSplitsOffItsInstance(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		// Three databases built identically from the same files fool the fingerprint, but not playback.
		lounge := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
		bedroom := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
		study := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})

		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""), kodiConfig("Study", study, ""))

		settled(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 3 })
		settled(t, func() bool { return hasTitle(a.instanceNamed(t, "Study").Catalog, "Film Number 3") })

		connected(t, study)
		require.True(t, study.SetPlayState("movie", 3, fakekodi.PlayState{Resume: 1200, LastPlayed: time.Now().Truncate(time.Second)}))

		tune := fastTuning()
		// The peers are checked in turn, each getting its grace period.
		after(t, tune.confirmDebounce+2*tune.confirmGrace, func() bool { return len(a.Groups()) == 2 })

		st, _ := a.groupOf("Study")
		assert.Equal(t, []string{"Study"}, memberNames(st), "the one Kodi whose change nobody saw is the odd one out")
		assert.Contains(t, st.Reason, "a change to “Film Number 3” on Study wasn't seen on")

		rest, _ := a.groupOf("Lounge")
		assert.ElementsMatch(t, []string{"Lounge", "Bedroom"}, memberNames(rest), "the others still agree with each other")
	})
}

func TestConfiguredSeparationSurvivesRestarts(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge, study := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})
		dir := t.TempDir()
		kodis := []config.KodiInstance{kodiConfig("Lounge", lounge, ""), kodiConfig("Study", study, "none")}

		a, stop := runApp(t, dir, fastTuning(), kodis...)
		first, _ := a.groupOf("Study")
		stop()

		a, _ = runApp(t, dir, fastTuning(), kodis...)
		settled(t, func() bool { return a.fingerprintedAll("Lounge", "Study") })

		again, _ := a.groupOf("Study")
		assert.Equal(t, first.ID, again.ID, "the config file's group is found again, not made anew")
		assert.Equal(t, []string{"Study"}, memberNames(again), "the same database, but kept apart")
		assert.Equal(t, "kept separate by the config file", again.Evidence)
	})
}

func TestGroupChangesReportDatabaseFailures(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lounge := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
		bedroom := newKodi(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})

		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
		loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id
		ctx := t.Context()

		settled(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom") })

		both, err := a.GroupInstances(ctx, "Whole house", loungeID, bedroomID)
		require.NoError(t, err)

		before := a.Groups()

		require.NoError(t, a.DB().Close())

		_, err = a.GroupInstances(ctx, "", loungeID)
		require.Error(t, err)
		require.Error(t, a.SetGroupSource(ctx, both, bedroomID))
		require.Error(t, a.SplitInstance(ctx, loungeID))
		require.Error(t, a.ResetLibraryGroup(ctx, loungeID))
		require.Error(t, a.ReloadInstance(ctx, loungeID))
		require.Error(t, a.RemoveInstance(ctx, bedroomID))

		assert.Equal(t, memberNames(before[0]), memberNames(a.Groups()[0]), "groups change in memory only once saved")
	})
}

func TestUserGroupEdgeCases(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lounge := newKodi(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
		bedroom := newKodi(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})

		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
		loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id
		ctx := t.Context()

		settled(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom") })

		_, err := a.GroupInstances(ctx, "", loungeID, unknownInstance)
		require.ErrorIs(t, err, store.ErrNotFound)
		require.ErrorIs(t, a.ResetLibraryGroup(ctx, unknownInstance), store.ErrNotFound)
		require.NoError(t, a.ResetLibraryGroup(ctx, loungeID), "a detected group has nothing to reset")

		// Grouping one instance, however often it's named, keeps it on its own by choice.
		solo, err := a.GroupInstances(ctx, "", loungeID, loungeID)
		require.NoError(t, err)

		g, _ := a.groupOf("Lounge")
		assert.Equal(t, solo, g.ID)
		assert.Equal(t, store.GroupSolo, g.State)
		assert.True(t, g.Manual)
		assert.Equal(t, "Lounge", g.Name, "an unnamed group is named after its source")

		require.NoError(t, a.SetGroupSource(ctx, solo, loungeID), "the source already")

		// The first instance named leads the group, and names it.
		both, err := a.GroupInstances(ctx, "", bedroomID, loungeID)
		require.NoError(t, err)

		g, _ = a.groupOf("Lounge")
		assert.Equal(t, both, g.ID)
		assert.Equal(t, "Bedroom", g.Name)
		assert.Equal(t, bedroomID, g.SourceID)

		require.NoError(t, a.SplitInstance(ctx, loungeID))

		split, _ := a.groupOf("Lounge")
		require.NoError(t, a.SplitInstance(ctx, loungeID))

		again, _ := a.groupOf("Lounge")
		assert.Equal(t, split.ID, again.ID, "splitting an instance already on its own keeps its group")
		assert.Len(t, a.Groups(), 2)
	})
}

// plannedApp opens, without running, an App with Kodi instances that are never contacted, enough to plan library groups.
func plannedApp(t *testing.T, names ...string) *App {
	t.Helper()

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()

	for i, name := range names {
		cfg.Kodi = append(cfg.Kodi, config.KodiInstance{Name: name, URL: fmt.Sprintf("http://127.0.0.1:%d/jsonrpc", i+1)})
	}

	a := openApp(t, &cfg, fastTuning())

	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	return a
}

func TestJoiningKeepsEarlierSplitsFromBothSides(t *testing.T) {
	a := plannedApp(t, "Lounge", "Bedroom", "Study", "Kitchen")
	ctx := t.Context()
	lounge, bedroom := a.instanceNamed(t, "Lounge"), a.instanceNamed(t, "Bedroom")
	study, kitchen := a.instanceNamed(t, "Study").id, a.instanceNamed(t, "Kitchen").id

	a.mu.Lock()
	g := lounge.group
	g.ev.Vetoed = []int64{study}
	bedroom.group.ev.Vetoed = []int64{kitchen}
	a.mu.Unlock()

	a.planMu.Lock()
	err := a.join(ctx, lounge, g, bedroom, &fpResult{Matched: 60, Compared: 60})
	a.planMu.Unlock()
	require.NoError(t, err)

	joined, ok := a.groupOf("Bedroom")
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"Lounge", "Bedroom"}, memberNames(joined))
	assert.Equal(t, "Lounge", joined.Name, "the instance created first leads")
	assert.Equal(t, store.GroupLikelyShared, joined.State)
	assert.Equal(t, "60 of 60 sampled items match", joined.Evidence)

	rec, err := a.DB().LibraryGroups().Get(ctx, joined.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{study, kitchen}, decodeEvidence(rec.Evidence).Vetoed, "neither side's earlier splits are forgotten")
}

func TestPlanRestoresGroupInvariants(t *testing.T) {
	fp := &fingerprintEvidence{Matched: 60, Compared: 60}

	cases := []struct {
		name       string
		group      func(lounge, bedroom int64) (store.LibraryGroup, []int64)
		wantState  store.LibraryGroupState
		wantSource string
		wantEv     evidence
	}{
		{
			name: "a detected group left with one member is solo and forgets what showed it shared",
			group: func(lounge, bedroom int64) (store.LibraryGroup, []int64) {
				ev := evidence{Origin: originDetected, Fingerprint: fp, Confirmed: []int64{lounge, bedroom}, Confirmation: "seen", Reason: "split", Vetoed: []int64{99}}
				return store.LibraryGroup{Name: "Lounge", State: store.GroupConfirmedShared, SourceInstanceID: lounge, Evidence: ev.encode()}, []int64{lounge}
			},
			wantState: store.GroupSolo, wantSource: "Lounge",
			wantEv: evidence{Origin: originDetected, Reason: "split", Vetoed: []int64{99}},
		},
		{
			name: "a detected solo group with two members is likely shared",
			group: func(lounge, bedroom int64) (store.LibraryGroup, []int64) {
				ev := evidence{Origin: originDetected, Fingerprint: fp}
				return store.LibraryGroup{Name: "Lounge", State: store.GroupSolo, SourceInstanceID: lounge, Evidence: ev.encode()}, []int64{lounge, bedroom}
			},
			wantState: store.GroupLikelyShared, wantSource: "Lounge",
			wantEv: evidence{Origin: originDetected, Fingerprint: fp},
		},
		{
			name: "a manual group with two members is manual",
			group: func(lounge, bedroom int64) (store.LibraryGroup, []int64) {
				ev := evidence{Origin: originUser}
				return store.LibraryGroup{Name: "Upstairs", State: store.GroupSolo, Manual: true, SourceInstanceID: bedroom, Evidence: ev.encode()}, []int64{lounge, bedroom}
			},
			wantState: store.GroupManual, wantSource: "Bedroom",
			wantEv: evidence{Origin: originUser},
		},
		{
			name: "a manual group with one member is solo",
			group: func(_, bedroom int64) (store.LibraryGroup, []int64) {
				ev := evidence{Origin: originUser}
				return store.LibraryGroup{Name: "Upstairs", State: store.GroupManual, Manual: true, SourceInstanceID: bedroom, Evidence: ev.encode()}, []int64{bedroom}
			},
			wantState: store.GroupSolo, wantSource: "Bedroom",
			wantEv: evidence{Origin: originUser},
		},
		{
			name: "a group whose source isn't a member is led by its first member",
			group: func(lounge, bedroom int64) (store.LibraryGroup, []int64) {
				ev := evidence{Origin: originUser}
				return store.LibraryGroup{Name: "Upstairs", State: store.GroupManual, Manual: true, Evidence: ev.encode()}, []int64{bedroom, lounge}
			},
			wantState: store.GroupManual, wantSource: "Lounge",
			wantEv: evidence{Origin: originUser},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := plannedApp(t, "Lounge", "Bedroom")
			ctx := t.Context()
			lounge, bedroom := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id

			rec, members := c.group(lounge, bedroom)
			require.NoError(t, a.DB().LibraryGroups().Save(ctx, &rec, members))

			a.planMu.Lock()
			err := a.plan(ctx)
			a.planMu.Unlock()
			require.NoError(t, err)

			got, err := a.DB().LibraryGroups().Get(ctx, rec.ID)
			require.NoError(t, err)
			assert.Equal(t, c.wantState, got.State)
			assert.Equal(t, a.instanceNamed(t, c.wantSource).id, got.SourceInstanceID)

			ev := decodeEvidence(got.Evidence)
			if ev.Fingerprint != nil {
				ev.Fingerprint.At = fp.At
			}

			assert.Equal(t, c.wantEv, ev)

			groups := a.Groups()
			for _, g := range groups {
				assert.NotEmpty(t, g.Members, "empty groups are removed")
			}

			assert.Len(t, groups, 3-len(members), "every instance is in exactly one group")
		})
	}
}

func TestResettingADetectedGroupChangesNothing(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge, bedroom := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})
		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

		settled(t, func() bool {
			g, _ := a.groupOf("Lounge")
			return len(g.Members) == 2 && a.fingerprintedAll("Lounge", "Bedroom")
		})

		before, _ := a.groupOf("Lounge")

		require.NoError(t, a.ResetLibraryGroup(t.Context(), a.instanceNamed(t, "Lounge").id))

		after, _ := a.groupOf("Lounge")
		assert.Equal(t, before.ID, after.ID, "the group isn't dissolved and formed again")
		assert.ElementsMatch(t, []string{"Lounge", "Bedroom"}, memberNames(after))
		assert.Equal(t, before.State, after.State)
	})
}
