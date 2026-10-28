package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

// fakeLibraries answers the library endpoints with fixed results.
type fakeLibraries struct {
	lib    Library
	list   []Library
	recent []RecentTitle
	err    error
}

func (f *fakeLibraries) List(context.Context) ([]Library, error) { return f.list, f.err }

func (f *fakeLibraries) Get(_ context.Context, id int64) (Library, error) {
	if id != f.lib.ID {
		return Library{}, store.ErrNotFound
	}

	return f.lib, nil
}

func (f *fakeLibraries) LibraryOf(context.Context, int64) (Library, error) { return f.lib, nil }

func (f *fakeLibraries) SetSource(context.Context, int64, int64) (Library, error) {
	return f.lib, f.err
}

func (f *fakeLibraries) Group(context.Context, []int64) (Library, error) { return f.lib, f.err }

func (f *fakeLibraries) Split(context.Context, int64, int64) error { return f.err }

func (f *fakeLibraries) Recent(context.Context, int64, int) ([]RecentTitle, error) {
	return f.recent, f.err
}

func TestLibraryListsAreNeverNull(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.Libraries = &fakeLibraries{lib: Library{ID: 1}} }})
	c := h.admin()

	for _, path := range []string{"/api/v1/libraries", "/api/v1/libraries/1/recent"} {
		resp := c.do(http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, resp.status, path)
		assert.JSONEq(t, `[]`, string(resp.body), path)
	}
}

func TestLibraryGroupsManagedByConfig(t *testing.T) {
	managed := fmt.Errorf("kodi instance 2: %w", app.ErrManagedByConfig)
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) { o.Libraries = &fakeLibraries{lib: Library{ID: 1}, err: managed} }})
	c := h.admin()

	for _, req := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/api/v1/libraries/1", map[string]int64{"source_instance_id": 2}},
		{http.MethodPost, "/api/v1/libraries/group", map[string][]int64{"instance_ids": {1, 2}}},
		{http.MethodPost, "/api/v1/libraries/1/split", map[string]int64{"instance_id": 2}},
	} {
		p := c.do(req.method, req.path, req.body).problem(t)
		assert.Equal(t, http.StatusConflict, p.Status, req.path)
		assert.Contains(t, p.Detail, "kodi[].library_group", "the admin is told where to change it")
	}
}

func TestLibraryChangesAreStreamed(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.Libraries = &fakeLibraries{lib: Library{ID: 7, Name: "Lounge", Members: []LibraryMember{{InstanceID: 1, Name: "Lounge"}, {InstanceID: 2, Name: "Den"}}}}
	}})
	c := h.admin()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, resp := c.stream(ctx, "")
	defer resp.Body.Close()

	next(t, ch, "comment")

	for _, req := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/api/v1/libraries/7", map[string]int64{"source_instance_id": 2}},
		{http.MethodPost, "/api/v1/libraries/group", map[string][]int64{"instance_ids": {1, 2}}},
	} {
		c.json(req.method, req.path, req.body, http.StatusOK, nil)

		var lib Library

		require.NoError(t, json.Unmarshal([]byte(next(t, ch, "library").data), &lib), req.path)
		assert.Equal(t, int64(7), lib.ID, req.path)
		assert.Len(t, lib.Members, 2, req.path)
	}
}

func TestLibraryRequestValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lib := h.libraryID(t)
	lounge := h.loungeID()

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/libraries/group", map[string][]int64{"instance_ids": {lounge}}).status, "a group needs two instances")
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, fmt.Sprintf("/api/v1/libraries/%d/split", lib), map[string]int64{"instance_id": 999}).status, "only a member can be split off")
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPost, "/api/v1/libraries/999/split", map[string]int64{"instance_id": lounge}).status)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPatch, "/api/v1/libraries/999", map[string]int64{"source_instance_id": lounge}).status)

	for _, limit := range []string{"0", "201", "x"} {
		assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, fmt.Sprintf("/api/v1/libraries/%d/recent?limit=%s", lib, limit), nil).status, limit)
	}
}

func TestRecentTitlesNewestFirst(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lib := h.libraryID(t)

	var recent []RecentTitle

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/libraries/%d/recent?limit=200", lib), nil, http.StatusOK, &recent)
	require.Greater(t, len(recent), 3)

	assert.True(t, slices.IsSortedFunc(recent, func(a, b RecentTitle) int { return b.AddedAt.Compare(*a.AddedAt) }), "newest first")
	assert.True(t, slices.ContainsFunc(recent, func(r RecentTitle) bool { return r.Kind == "tvshow" }), "shows as well as movies")

	for _, r := range recent {
		assert.Equal(t, "Lounge", r.Instance)
		assert.True(t, r.Findable, r.Title)
	}

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/libraries/%d/recent?limit=1", lib), nil, http.StatusOK, &recent)
	require.Len(t, recent, 1)
	assert.Equal(t, "Conclave", recent[0].Title)
}

func TestListedTitle(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		item media.Item
		want string
	}{
		"a movie":                     {media.Item{Kind: media.KindMovie, Title: "Conclave"}, "Conclave"},
		"a show":                      {media.Item{Kind: media.KindTVShow, Title: "Severance"}, "Severance"},
		"an episode, after its show":  {media.Item{Kind: media.KindEpisode, Title: "Who Is Alive?", ShowTitle: "Severance"}, "Severance: Who Is Alive?"},
		"an episode without its show": {media.Item{Kind: media.KindEpisode, Title: "Who Is Alive?"}, "Who Is Alive?"},
		"a song keeps its own title":  {media.Item{Kind: media.KindSong, Title: "Clair de Lune", ShowTitle: "ignored"}, "Clair de Lune"},
	} {
		assert.Equal(t, tc.want, listedTitle(tc.item), name)
	}
}

func TestToLibraryUsesTheReasonWithoutEvidence(t *testing.T) {
	t.Parallel()

	lib := toLibrary(&app.GroupStatus{ID: 3, Name: "Lounge", State: "solo", Reason: "only one instance", Members: []app.GroupMember{{ID: 1, Name: "Lounge"}}})
	assert.Equal(t, "only one instance", lib.Evidence)
	assert.Nil(t, lib.LastSyncAt, "never synced")
	assert.Equal(t, []LibraryMember{{InstanceID: 1, Name: "Lounge"}}, lib.Members)

	assert.Equal(t, LibraryCounts{}, lib.Counts, "nothing counted yet is zero, not missing")

	lib = toLibrary(&app.GroupStatus{ID: 3, Counts: map[media.Kind]int{media.KindMovie: 12, media.KindTVShow: 3, media.KindSong: 450, media.KindEpisode: 80}})
	assert.Equal(t, LibraryCounts{Movies: 12, Shows: 3, Songs: 450}, lib.Counts, "each kind is counted under its own name")

	lib = toLibrary(&app.GroupStatus{ID: 3, Evidence: "same database path", Reason: "only one instance"})
	assert.Equal(t, "same database path", lib.Evidence)
	assert.Equal(t, []LibraryMember{}, lib.Members, "no members is an empty list")
}
