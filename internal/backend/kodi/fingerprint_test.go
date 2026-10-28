package kodi

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
)

func fingerprintLibrary(movies, episodes int) *fakekodi.Library {
	lib := &fakekodi.Library{}

	for i := 1; i <= movies; i++ {
		lib.Movies = append(lib.Movies, fakekodi.Movie{MovieID: i, Title: fmt.Sprintf("Movie %d", i), File: fmt.Sprintf("/media/movies/%d.mkv", i), DateAdded: "2024-01-01 10:00:00"})
	}

	for i := 1; i <= episodes; i++ {
		lib.Episodes = append(lib.Episodes, fakekodi.Episode{EpisodeID: i, TVShowID: 1, Title: fmt.Sprintf("Episode %d", i), File: fmt.Sprintf("/media/tv/%d.mkv", i), DateAdded: "2024-02-01 10:00:00"})
	}

	return lib
}

func TestFingerprint(t *testing.T) {
	b, s := newBackend(t, fakekodi.Options{Library: fingerprintLibrary(250, 3)}, BackendOptions{})

	fp, err := b.Fingerprint(t.Context(), 0)
	require.NoError(t, err)

	assert.Len(t, fp.Movies, DefaultFingerprintSample, "the sample is capped")
	assert.Len(t, fp.Episodes, 3)
	assert.Equal(t, 250, fp.MovieTotal)
	assert.Equal(t, 3, fp.EpisodeTotal)
	assert.Equal(t, FingerprintItem{ID: 1, File: "/media/movies/1.mkv", DateAdded: "2024-01-01 10:00:00"}, fp.Movies[0])
	assert.Empty(t, fp.DatabaseName, "Kodi 21 can't name its database")
	assert.False(t, fp.Empty())

	params := lastParams(t, s, "VideoLibrary.GetMovies")
	assert.Contains(t, params, `"properties":["file","dateadded"]`)
	assert.Empty(t, s.CallsTo("Database.GetDatabaseName"), "the client knows Kodi 21 lacks the method")
}

func TestFingerprintDatabaseName(t *testing.T) {
	b, _ := newBackend(t, fakekodi.Options{Version: [3]int{13, 1000, 0}, DatabaseName: "MyVideos149", Library: &fakekodi.Library{}}, BackendOptions{})

	fp, err := b.Fingerprint(t.Context(), 20)
	require.NoError(t, err)
	assert.Equal(t, "MyVideos149", fp.DatabaseName)
	assert.True(t, fp.Empty())
}

func TestFingerprintFailures(t *testing.T) {
	for _, method := range []string{"VideoLibrary.GetMovies", "VideoLibrary.GetEpisodes"} {
		b, s := newBackend(t, fakekodi.Options{}, BackendOptions{})
		failWith(s, method)

		fp, err := b.Fingerprint(t.Context(), 5)

		require.ErrorContains(t, err, "sampling", method)
		assert.True(t, fp.Empty(), "no partial sample: %s", method)
	}

	b, s := newBackend(t, fakekodi.Options{Version: [3]int{13, 1000, 0}, DatabaseName: "MyVideos149"}, BackendOptions{})
	failWith(s, "Database.GetDatabaseName")

	fp, err := b.Fingerprint(t.Context(), 5)
	require.NoError(t, err, "the database name is a bonus, not required")
	assert.Empty(t, fp.DatabaseName)
	assert.Len(t, fp.Movies, 5)
}
