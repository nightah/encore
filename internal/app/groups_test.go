package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

const eventually = 10 * time.Second

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

	return t
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

// connected waits until the app listens to a fake's notifications.
func connected(t *testing.T, srv *fakekodi.Server) {
	t.Helper()

	require.Eventually(t, func() bool { return srv.WebSocketClients() > 0 }, eventually, 10*time.Millisecond)
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

// scanned reports a finished library scan until done is true; the app may not be listening to the fake yet.
func scanned(t *testing.T, srv *fakekodi.Server, done func() bool) {
	t.Helper()

	require.Eventually(t, func() bool {
		srv.Notify("VideoLibrary.OnScanFinished", nil)

		return done()
	}, eventually, 50*time.Millisecond)
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

	nearly.Movies = nearly.Movies[:180]
	assert.False(t, compareFingerprints(sample(200, 0, "2024-01-01 10:00:00"), nearly).match(&tune))

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
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	lounge, bedroom := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})

	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

	var g GroupStatus

	require.Eventually(t, func() bool {
		var ok bool
		g, ok = a.groupOf("Lounge")

		return ok && len(g.Members) == 2
	}, eventually, 10*time.Millisecond)

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

	require.Eventually(t, func() bool { return a.instanceNamed(t, "Bedroom").Index.Len() == 31 }, eventually, 10*time.Millisecond)

	// Watching on the bedroom Kodi changes the shared row, which the lounge Kodi reads too.
	connected(t, bedroom)

	played := time.Now().Truncate(time.Second)
	require.True(t, bedroom.SetPlayState("episode", 7, fakekodi.PlayState{PlayCount: 1, LastPlayed: played}))

	require.Eventually(t, func() bool {
		g, _ = a.groupOf("Lounge")

		return g.State == store.GroupConfirmedShared
	}, eventually, 10*time.Millisecond)

	assert.Equal(t, `a change to “Part 7” on Bedroom was seen on Lounge`, g.Evidence)

	for _, m := range g.Members {
		assert.True(t, m.Confirmed, m.Name)
	}

	stored, err := a.DB().LibraryGroups().Get(t.Context(), g.ID)
	require.NoError(t, err)
	assert.Equal(t, store.GroupConfirmedShared, stored.State)
}

func TestSeparateLibrariesOfTheSameFilesStayApart(t *testing.T) {
	lounge := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	bedroom := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})
	empty1, empty2 := fakekodi.New(t, fakekodi.Options{Library: &fakekodi.Library{}}), fakekodi.New(t, fakekodi.Options{Library: &fakekodi.Library{}})

	a, _ := runApp(t, t.TempDir(), fastTuning(),
		kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""), kodiConfig("Garage", empty1, ""), kodiConfig("Shed", empty2, ""))

	require.Eventually(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom", "Garage", "Shed") }, eventually, 10*time.Millisecond)

	// Give a mistaken merge time to show.
	time.Sleep(200 * time.Millisecond)

	groups := a.Groups()
	require.Len(t, groups, 4)

	for _, g := range groups {
		assert.Len(t, g.Members, 1)
		assert.Equal(t, store.GroupSolo, g.State)
	}
}

func TestUnmirroredChangeSplitsTheGroup(t *testing.T) {
	// Two databases built identically from the same files fool the fingerprint, but not playback.
	lounge := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	bedroom := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	dir := t.TempDir()

	a, stop := runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

	require.Eventually(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 2 }, eventually, 10*time.Millisecond)
	require.Eventually(t, func() bool { return hasTitle(a.instanceNamed(t, "Lounge").Catalog, "Film Number 3") }, eventually, 10*time.Millisecond)

	connected(t, lounge)
	require.True(t, lounge.SetPlayState("movie", 3, fakekodi.PlayState{Resume: 1200, LastPlayed: time.Now().Truncate(time.Second)}))

	require.Eventually(t, func() bool { return len(a.Groups()) == 2 }, eventually, 10*time.Millisecond)

	for _, name := range []string{"Lounge", "Bedroom"} {
		g, ok := a.groupOf(name)
		require.True(t, ok)
		assert.Equal(t, store.GroupSolo, g.State, name)
		assert.Equal(t, `a change to “Film Number 3” on Lounge wasn't seen on Bedroom`, g.Reason, name)
	}

	a.mu.RLock()
	assert.NotSame(t, a.instanceNamed(t, "Lounge").Index, a.instanceNamed(t, "Bedroom").Index)
	a.mu.RUnlock()

	// The split survives a restart, when the fingerprints match again.
	stop()

	a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

	require.Eventually(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom") }, eventually, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	assert.Len(t, a.Groups(), 2)
}

func TestDivergedFingerprintSplitsTheGroup(t *testing.T) {
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	lounge, bedroom := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})
	dir := t.TempDir()

	a, stop := runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
	require.Eventually(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 2 }, eventually, 10*time.Millisecond)
	stop()

	// The bedroom Kodi was pointed at a database of its own while Encore was down.
	rebuilt := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2025-06-01 09:00:00")})

	a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", rebuilt, ""))
	require.Eventually(t, func() bool { return len(a.Groups()) == 2 }, eventually, 10*time.Millisecond)

	g, _ := a.groupOf("Bedroom")
	assert.Equal(t, store.GroupSolo, g.State)
	assert.Contains(t, g.Reason, "no longer agree: 0 of 60 sampled items match")
}

func TestSourceFailoverAndFailback(t *testing.T) {
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	lounge, bedroom := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})

	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, "upstairs"), kodiConfig("Bedroom", bedroom, "upstairs"))
	loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id
	catalog := a.instanceNamed(t, "Bedroom").Catalog

	require.Eventually(t, func() bool { return catalog.Stats().Counts[media.KindMovie] == 30 }, eventually, 10*time.Millisecond)

	g, _ := a.groupOf("Lounge")
	assert.Equal(t, loungeID, g.SourceID)
	assert.Equal(t, loungeID, g.ActiveID)

	// With a shared database only the Kodi that scanned reports it, and the source syncs the change.
	connected(t, bedroom)
	bedroom.Update(func(l *fakekodi.Library) {
		l.Movies = append(l.Movies, fakekodi.Movie{MovieID: 400, Title: "Bedroom Scan", Label: "Bedroom Scan", File: "nfs://nas/movies/scan.mkv", DateAdded: time.Now().Format(fakekodi.TimeLayout)})
	})
	scanned(t, bedroom, func() bool { return hasTitle(catalog, "Bedroom Scan") })
	assert.Zero(t, librarySyncs(bedroom), "the bedroom Kodi isn't asked while the source is up")

	// The source goes away; the bedroom Kodi takes over syncing without anything being woken.
	lounge.Close()

	require.Eventually(t, func() bool { g, _ = a.groupOf("Lounge"); return g.ActiveID == bedroomID }, eventually, 10*time.Millisecond)
	assert.Equal(t, loungeID, g.SourceID, "the chosen source doesn't change")

	// A scan reported by any member is synced from the active one.
	bedroom.Update(func(l *fakekodi.Library) {
		l.Movies = append(l.Movies, fakekodi.Movie{MovieID: 500, Title: "Brand New Film", Label: "Brand New Film", File: "nfs://nas/movies/new.mkv", DateAdded: time.Now().Format(fakekodi.TimeLayout)})
	})
	scanned(t, bedroom, func() bool { return hasTitle(catalog, "Brand New Film") })
	assert.Empty(t, a.Groups()[0].Reason)

	// The source returns and takes over again.
	lounge.Restart(t)

	require.Eventually(t, func() bool { g, _ = a.groupOf("Lounge"); return g.ActiveID == loungeID }, eventually, 10*time.Millisecond)
}

func TestConfiguredGroups(t *testing.T) {
	shared := testLibrary(30, 0, "2024-01-01 10:00:00")
	lounge := fakekodi.New(t, fakekodi.Options{Library: shared})
	bedroom := fakekodi.New(t, fakekodi.Options{Library: shared})
	// The study reads the same database but is told to keep its own library; the kitchen's library is different but configured into the group.
	study := fakekodi.New(t, fakekodi.Options{Library: shared})
	kitchen := fakekodi.New(t, fakekodi.Options{})
	dir := t.TempDir()

	kodis := []config.KodiInstance{
		kodiConfig("Lounge", lounge, "upstairs"), kodiConfig("Kitchen", kitchen, "upstairs"),
		kodiConfig("Study", study, "none"), kodiConfig("Bedroom", bedroom, ""),
	}

	a, stop := runApp(t, dir, fastTuning(), kodis...)

	require.Eventually(t, func() bool { return a.fingerprintedAll("Lounge", "Kitchen", "Study", "Bedroom") }, eventually, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)

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

	require.Eventually(t, func() bool { g, _ := a.groupOf("Study"); return len(g.Members) == 2 }, eventually, 10*time.Millisecond)

	g, _ := a.groupOf("Study")
	assert.ElementsMatch(t, []string{"Study", "Bedroom"}, memberNames(g))
	assert.Equal(t, store.GroupLikelyShared, g.State)
}

func TestUserGroupChoices(t *testing.T) {
	lounge := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	bedroom := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})

	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
	loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id
	ctx := t.Context()

	require.Eventually(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom") }, eventually, 10*time.Millisecond)
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
	require.Eventually(t, func() bool { g, _ = a.groupOf("Lounge"); return g.ActiveID == bedroomID }, eventually, 10*time.Millisecond)
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
}

func TestSnapshotWarmStart(t *testing.T) {
	srv := fakekodi.New(t, fakekodi.Options{})
	dir := t.TempDir()

	a, stop := runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
	g, _ := a.groupOf("Lounge")
	file := filepath.Join(dir, "blobs", "library", fmt.Sprintf("%d.json.gz", g.ID))

	require.Eventually(t, func() bool { _, err := os.Stat(file); return err == nil }, eventually, 10*time.Millisecond)
	stop()

	// With Kodi off, the restarted app still knows the library.
	srv.Close()

	a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
	inst := a.instanceNamed(t, "Lounge")

	require.Eventually(t, func() bool { return inst.Index.Len() > 0 }, eventually, 10*time.Millisecond)
	assert.True(t, hasTitle(inst.Catalog, "Hot Fuzz"))
	assert.False(t, inst.Catalog.Stats().LastSync.IsZero(), "the snapshot's sync time comes back with it")
}

func TestUnsuitableSnapshotsAreSkipped(t *testing.T) {
	srv := fakekodi.New(t, fakekodi.Options{})
	dir := t.TempDir()

	a, stop := runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
	g, _ := a.groupOf("Lounge")
	key := snapshotKey(g.ID)
	file := filepath.Join(dir, "blobs", filepath.FromSlash(key))

	require.Eventually(t, func() bool { _, err := os.Stat(file); return err == nil }, eventually, 10*time.Millisecond)
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
	time.Sleep(200 * time.Millisecond)
	assert.Zero(t, a.instanceNamed(t, "Lounge").Index.Len())
	stop()

	// A missing or damaged snapshot just means a cold start.
	require.NoError(t, os.WriteFile(file, []byte("not gzip"), 0o600))

	a, stop = runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
	time.Sleep(100 * time.Millisecond)
	assert.Zero(t, a.instanceNamed(t, "Lounge").Index.Len())
	stop()

	require.NoError(t, os.Remove(file))

	a, _ = runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
	time.Sleep(100 * time.Millisecond)
	assert.Zero(t, a.instanceNamed(t, "Lounge").Index.Len())
}

func TestAThirdSharingInstanceJoinsTheGroup(t *testing.T) {
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	lounge, bedroom, study := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})

	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""), kodiConfig("Study", study, ""))

	require.Eventually(t, func() bool { return len(a.Groups()) == 1 && len(a.Groups()[0].Members) == 3 }, eventually, 10*time.Millisecond)

	// Whichever pair sampled first formed the group; the third joins it without taking it over.
	g := a.Groups()[0]
	assert.Equal(t, a.instanceNamed(t, g.Name).id, g.SourceID, "the group is named after its source")
	assert.Equal(t, store.GroupLikelyShared, g.State)
	assert.Equal(t, "60 of 60 sampled items match", g.Evidence)
	assert.ElementsMatch(t, []string{"Lounge", "Bedroom", "Study"}, memberNames(g))
}

func TestAChangeMostMembersMissedSplitsOffItsInstance(t *testing.T) {
	// Three databases built identically from the same files fool the fingerprint, but not playback.
	lounge := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	bedroom := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	study := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})

	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""), kodiConfig("Study", study, ""))

	require.Eventually(t, func() bool { g, _ := a.groupOf("Lounge"); return len(g.Members) == 3 }, eventually, 10*time.Millisecond)
	require.Eventually(t, func() bool { return hasTitle(a.instanceNamed(t, "Study").Catalog, "Film Number 3") }, eventually, 10*time.Millisecond)

	connected(t, study)
	require.True(t, study.SetPlayState("movie", 3, fakekodi.PlayState{Resume: 1200, LastPlayed: time.Now().Truncate(time.Second)}))

	require.Eventually(t, func() bool { return len(a.Groups()) == 2 }, eventually, 10*time.Millisecond)

	st, _ := a.groupOf("Study")
	assert.Equal(t, []string{"Study"}, memberNames(st), "the one Kodi whose change nobody saw is the odd one out")
	assert.Contains(t, st.Reason, "a change to “Film Number 3” on Study wasn't seen on")

	rest, _ := a.groupOf("Lounge")
	assert.ElementsMatch(t, []string{"Lounge", "Bedroom"}, memberNames(rest), "the others still agree with each other")
}

func TestConfiguredSeparationSurvivesRestarts(t *testing.T) {
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	lounge, study := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})
	dir := t.TempDir()
	kodis := []config.KodiInstance{kodiConfig("Lounge", lounge, ""), kodiConfig("Study", study, "none")}

	a, stop := runApp(t, dir, fastTuning(), kodis...)
	first, _ := a.groupOf("Study")
	stop()

	a, _ = runApp(t, dir, fastTuning(), kodis...)
	require.Eventually(t, func() bool { return a.fingerprintedAll("Lounge", "Study") }, eventually, 10*time.Millisecond)

	again, _ := a.groupOf("Study")
	assert.Equal(t, first.ID, again.ID, "the config file's group is found again, not made anew")
	assert.Equal(t, []string{"Study"}, memberNames(again), "the same database, but kept apart")
	assert.Equal(t, "kept separate by the config file", again.Evidence)
}

func TestGroupChangesReportDatabaseFailures(t *testing.T) {
	lounge := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	bedroom := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})

	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
	loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id
	ctx := t.Context()

	require.Eventually(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom") }, eventually, 10*time.Millisecond)

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
}

func TestUserGroupEdgeCases(t *testing.T) {
	lounge := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 0, "2024-01-01 10:00:00")})
	bedroom := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})

	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))
	loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id
	ctx := t.Context()

	require.Eventually(t, func() bool { return a.fingerprintedAll("Lounge", "Bedroom") }, eventually, 10*time.Millisecond)

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
}
