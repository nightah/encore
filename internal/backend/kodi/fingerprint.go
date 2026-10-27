package kodi

import (
	"context"
	"fmt"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

// Empty reports whether the sample holds no items.
func (f *LibraryFingerprint) Empty() bool { return len(f.Movies) == 0 && len(f.Episodes) == 0 }

// Fingerprint samples up to sample movies and sample episodes in Kodi's database order, reading only IDs, file paths and dates added. It never writes to Kodi.
func (b *Backend) Fingerprint(ctx context.Context, sample int) (LibraryFingerprint, error) {
	if sample <= 0 {
		sample = DefaultFingerprintSample
	}

	var fp LibraryFingerprint

	limits := &kodirpc.ListLimits{Start: 0, End: kodirpc.ListAmount(sample)}

	movies, err := b.c.VideoLibraryGetMovies(ctx, kodirpc.VideoLibraryGetMoviesParams{Properties: kodirpc.VideoFieldsMovie{"file", "dateadded"}, Limits: limits})
	if err != nil {
		return LibraryFingerprint{}, fmt.Errorf("sampling movies: %w", err)
	}

	episodes, err := b.c.VideoLibraryGetEpisodes(ctx, kodirpc.VideoLibraryGetEpisodesParams{Properties: kodirpc.VideoFieldsEpisode{"file", "dateadded"}, Limits: limits})
	if err != nil {
		return LibraryFingerprint{}, fmt.Errorf("sampling episodes: %w", err)
	}

	fp.MovieTotal, fp.EpisodeTotal = movies.Limits.Total, episodes.Limits.Total

	for i := range movies.Movies {
		m := &movies.Movies[i]
		fp.Movies = append(fp.Movies, FingerprintItem{ID: int(m.MovieID), File: m.File, DateAdded: m.Dateadded})
	}

	for i := range episodes.Episodes {
		e := &episodes.Episodes[i]
		fp.Episodes = append(fp.Episodes, FingerprintItem{ID: int(e.EpisodeID), File: e.File, DateAdded: e.Dateadded})
	}

	// Kodi 22 names the database in use; older versions don't, and the sample has to speak for itself.
	if b.c.Supports(ctx, kodirpc.MethodDatabaseGetDatabaseName) {
		if name, err := b.c.DatabaseGetDatabaseName(ctx, kodirpc.DatabaseGetDatabaseNameParams{Type: kodirpc.DatabaseTypeVideos}); err == nil {
			fp.DatabaseName = string(name)
		} else {
			b.log.Debug("reading the video database name", "error", err)
		}
	}

	return fp, nil
}
