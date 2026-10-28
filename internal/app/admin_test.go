package app

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/library"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

const unknownInstance = 999

// instance returns the harness's only Kodi instance.
func (h *harness) instance(t *testing.T) *Instance {
	t.Helper()

	ids := h.app.InstanceIDs()
	require.Len(t, ids, 1)

	inst, err := h.app.runtime(ids[0])
	require.NoError(t, err)

	return inst
}

// newBareApp opens an App with no Kodi instances, which is all the connection test needs.
func newBareApp(t *testing.T) *App {
	t.Helper()

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()

	a, err := newApp(t.Context(), &cfg, slog.New(slog.DiscardHandler), onFakes(defaultTuning()))
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	return a
}

func TestAccessorsExposeWhatTheAppWasStartedWith(t *testing.T) {
	a := newBareApp(t)

	assert.Same(t, a.cfg, a.Config())
	assert.Empty(t, a.InstanceIDs(), "no Kodi instances are configured")

	require.NoError(t, a.Secrets().Put(t.Context(), "test/secret", []byte("hunter2")))

	got, err := a.Secrets().Get(t.Context(), "test/secret")
	require.NoError(t, err)
	assert.Equal(t, "hunter2", string(got))
}

func TestInstanceIDsAreSorted(t *testing.T) {
	lounge, bedroom := newKodi(t, fakekodi.Options{}), newKodi(t, fakekodi.Options{})
	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

	ids := a.InstanceIDs()
	require.Len(t, ids, 2)
	assert.Less(t, ids[0], ids[1])
	assert.Equal(t, []int64{a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id}, ids)
}

func TestInstanceStatusOfAReachableKodi(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		inst := h.instance(t)

		settled(t, func() bool { return inst.Client.Transport().Connected() })

		st, err := h.app.InstanceStatus(t.Context(), inst.id, true)
		require.NoError(t, err)

		assert.Equal(t, inst.id, st.ID)
		assert.Equal(t, "Lounge", st.Name)
		assert.True(t, st.Connected)
		assert.True(t, st.Reachable)
		assert.False(t, st.Unauthorized)
		assert.NotEmpty(t, st.KodiVersion)
		assert.Equal(t, "13.5.0", st.APIVersion)
		assert.Equal(t, LibraryCounts{Movies: 7, Shows: 4, Episodes: 4, Songs: 3}, st.Counts, "the episode total comes from Kodi")
		require.NotNil(t, st.Newest)
		assert.Equal(t, "Conclave", st.Newest.Title, "the most recently added movie or episode")
		assert.False(t, st.LastSyncAt.IsZero())
		assert.Equal(t, st.LastSyncAt.Add(library.DefaultRefreshInterval), st.NextSyncAt)
		assert.Nil(t, st.NowPlaying, "nothing is playing")

		quick, err := h.app.InstanceStatus(t.Context(), inst.id, false)
		require.NoError(t, err)
		assert.Empty(t, quick.KodiVersion, "without info the version isn't asked for")
		assert.Empty(t, quick.APIVersion)
		assert.Zero(t, quick.Counts.Episodes, "without info only the cached episodes count, and none are cached yet")
		assert.Equal(t, 7, quick.Counts.Movies)

		h.say(t, "WatchMovie", "Movie", "hot fuzz")

		playing, err := h.app.InstanceStatus(t.Context(), inst.id, false)
		require.NoError(t, err)
		require.NotNil(t, playing.NowPlaying)
		assert.Equal(t, "Hot Fuzz", playing.NowPlaying.Item.Title)
	})
}

func TestInstanceStatusOfAnUnreachableKodi(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		inst := h.instance(t)

		h.kodi.Close()

		st, err := h.app.InstanceStatus(t.Context(), inst.id, true)
		require.NoError(t, err, "an unreachable Kodi is a status, not an error")
		assert.False(t, st.Reachable)
		assert.False(t, st.Unauthorized)
		assert.Empty(t, st.KodiVersion)
		assert.Nil(t, st.NowPlaying)
		assert.Equal(t, 7, st.Counts.Movies, "the library is still known from the catalog")
	})
}

func TestInstanceStatusReportsRejectedCredentials(t *testing.T) {
	srv := newKodi(t, fakekodi.Options{Username: "kodi", Password: "secret"})
	// Kodi doesn't authenticate its WebSocket, so only HTTP, which the ping takes while the WebSocket is down, sees the credentials.
	srv.SetWebSocket(false)

	h := startHarness(t, onFakes(defaultTuning()), config.KodiInstance{Name: "Lounge", URL: srv.URL(), Username: "kodi", Password: "wrong"})

	st, err := h.app.InstanceStatus(t.Context(), h.instance(t).id, true)
	require.NoError(t, err)
	assert.False(t, st.Reachable)
	assert.True(t, st.Unauthorized)
}

func TestConfiguredCredentialsReachKodi(t *testing.T) {
	srv := newKodi(t, fakekodi.Options{Username: "kodi", Password: "s3cret"})

	for name, kc := range map[string]config.KodiInstance{
		"fields":     {Name: "Lounge", URL: srv.URL(), Username: "kodi", Password: "s3cret"},
		"url":        {Name: "Lounge", URL: "http://kodi:s3cret@" + strings.TrimPrefix(srv.URL(), "http://")},
		"field wins": {Name: "Lounge", URL: "http://kodi:wrong@" + strings.TrimPrefix(srv.URL(), "http://"), Password: "s3cret"},
	} {
		t.Run(name, func(t *testing.T) {
			h := startHarness(t, onFakes(defaultTuning()), kc)
			inst := h.instance(t)

			assert.Equal(t, passwordRef("Lounge"), inst.Record.PasswordSecretRef, "the password is kept in the secret store, not the row")

			st, err := h.app.InstanceStatus(t.Context(), inst.id, false)
			require.NoError(t, err)
			assert.True(t, st.Reachable)
			assert.False(t, st.Unauthorized)
		})
	}
}

func TestUnknownInstancesHaveNoRuntime(t *testing.T) {
	h := newHarness(t, true)
	ctx := t.Context()

	_, err := h.app.InstanceStatus(ctx, unknownInstance, true)
	require.ErrorIs(t, err, ErrNoRuntime)

	_, _, err = h.app.InstanceImage(ctx, unknownInstance, "image://x")
	require.ErrorIs(t, err, ErrNoRuntime)

	_, err = h.app.RecentlyAdded(unknownInstance, 5)
	require.ErrorIs(t, err, ErrNoRuntime)

	_, err = h.app.Search(ctx, unknownInstance, match.Query{Text: "dune"})
	require.ErrorIs(t, err, ErrNoRuntime)

	require.ErrorIs(t, h.app.SyncInstance(ctx, unknownInstance), ErrNoRuntime)
	require.ErrorIs(t, h.app.WakeInstance(ctx, unknownInstance, time.Second), ErrNoRuntime)

	assert.False(t, h.app.Findable(unknownInstance, media.Ref{Kind: media.KindMovie, ID: 6}))

	_, ok := h.app.Item(unknownInstance, media.Ref{Kind: media.KindMovie, ID: 6})
	assert.False(t, ok)

	st := InstanceStatus{Counts: LibraryCounts{Movies: 1}}
	h.app.RefreshLibrary(unknownInstance, &st)
	assert.Equal(t, InstanceStatus{Counts: LibraryCounts{Movies: 1}}, st, "nothing to refresh from")
}

func TestRefreshLibraryKeepsKodisEpisodeTotal(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		inst := h.instance(t)

		stale := &media.Item{Title: "Stale"}
		st := InstanceStatus{Counts: LibraryCounts{Movies: 1, Episodes: 40}, Newest: stale}

		h.app.RefreshLibrary(inst.id, &st)

		assert.Equal(t, LibraryCounts{Movies: 7, Shows: 4, Episodes: 40, Songs: 3}, st.Counts, "Kodi's episode total beats the cached count")
		require.NotNil(t, st.Newest)
		assert.Equal(t, "Conclave", st.Newest.Title)
		assert.False(t, st.LastSyncAt.IsZero())

		st = InstanceStatus{}
		h.app.RefreshLibrary(inst.id, &st)
		assert.Zero(t, st.Counts.Episodes, "without a total from Kodi only the cached episodes count")

		eps, err := inst.Catalog.Episodes(t.Context(), 1)
		require.NoError(t, err)
		require.NotEmpty(t, eps)

		st = InstanceStatus{Counts: LibraryCounts{Episodes: 1}}
		h.app.RefreshLibrary(inst.id, &st)
		assert.Equal(t, len(eps), st.Counts.Episodes, "a stale total lower than the episodes cached since doesn't win")
	})
}

func TestLibraryReads(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		id := h.instance(t).id

		recent, err := h.app.RecentlyAdded(id, 2)
		require.NoError(t, err)
		require.Len(t, recent, 2)
		assert.Equal(t, "Conclave", recent[0].Title)
		assert.Equal(t, "Dune: Part Two", recent[1].Title)

		it, ok := h.app.Item(id, media.Ref{Kind: media.KindMovie, ID: 6})
		require.True(t, ok)
		assert.Equal(t, "Hot Fuzz", it.Title)

		_, ok = h.app.Item(id, media.Ref{Kind: media.KindMovie, ID: 404})
		assert.False(t, ok)

		res, err := h.app.Search(t.Context(), id, match.Query{Text: "hot fuzz"})
		require.NoError(t, err)
		require.NotEmpty(t, res.Candidates)
		assert.Equal(t, "Hot Fuzz", res.Candidates[0].Doc.Title)
	})
}

func TestFindable(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		id := h.instance(t).id

		assert.True(t, h.app.Findable(id, media.Ref{Kind: media.KindMovie, ID: 6}))
		assert.False(t, h.app.Findable(id, media.Ref{Kind: media.KindMovie, ID: 404}))

		episode := media.Ref{Kind: media.KindEpisode, ID: 102}
		assert.False(t, h.app.Findable(id, episode), "an episode not fetched yet isn't known")

		_, err := h.instance(t).Catalog.Episodes(t.Context(), 1)
		require.NoError(t, err)
		assert.True(t, h.app.Findable(id, episode), "an episode is found through its show")

		h.instance(t).Catalog.SetKinds([]media.Kind{media.KindMovie, media.KindPlaylist})

		_, cached := h.app.Item(id, episode)
		require.True(t, cached, "turning TV off leaves fetched episodes cached")
		assert.False(t, h.app.Findable(id, episode), "an episode whose show left the library can't be found")
	})
}

func TestInstanceImage(t *testing.T) {
	h := newHarness(t, true)
	id := h.instance(t).id

	body, contentType, err := h.app.InstanceImage(t.Context(), id, "image://fake/poster.jpg/")
	require.NoError(t, err)

	defer body.Close()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "image/png", contentType)
	assert.Equal(t, []byte("\x89PNG"), data[:4])

	_, _, err = h.app.InstanceImage(t.Context(), id, "/etc/passwd")
	require.ErrorIs(t, err, kodi.ErrNotImage, "only image:// references are fetched")
}

func TestLatest(t *testing.T) {
	early := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	later := early.Add(time.Hour)

	assert.Equal(t, later, latest(early, later))
	assert.Equal(t, later, latest(later, early))
	assert.Equal(t, early, latest(early, early))
	assert.Equal(t, early, latest(time.Time{}, early))
	assert.True(t, latest(time.Time{}, time.Time{}).IsZero())
}
