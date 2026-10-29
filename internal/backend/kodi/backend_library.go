package kodi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

var playlistDirs = []struct {
	dir   string
	media kodirpc.FilesMedia
}{
	{"special://profile/playlists/video/", kodirpc.FilesMediaVideo},
	{"special://profile/playlists/music/", kodirpc.FilesMediaMusic},
}

func (q query) rule(field string) (kodirpc.ListFilterRule, string, bool) {
	switch {
	case q.title != "":
		return kodirpc.ListFilterRule{Operator: "contains", Value: kodirpc.NewListFilterRuleValueFromString(q.title)}, field, true
	case q.unwatched:
		return kodirpc.ListFilterRule{Operator: "is", Value: kodirpc.NewListFilterRuleValueFromString("0")}, "playcount", true
	}

	return kodirpc.ListFilterRule{}, "", false
}

var fetchers = map[media.Kind]fetchPage{
	media.KindMovie: func(ctx context.Context, b *Backend, q query, limits *kodirpc.ListLimits) ([]media.Item, int, error) {
		p := kodirpc.VideoLibraryGetMoviesParams{Properties: movieFields, Limits: limits, Sort: q.sort}
		if rule, field, ok := q.rule("title"); ok {
			p.Filter = kodirpc.NewVideoLibraryGetMoviesFilterFromListFilterMovies(
				kodirpc.NewListFilterMoviesFromListFilterRuleMovies(kodirpc.ListFilterRuleMovies{ListFilterRule: rule, Field: kodirpc.ListFilterFieldsMovies(field)}))
		}

		r, err := b.c.VideoLibraryGetMovies(ctx, p)

		return mapAll(r.Movies, b.m.movie), r.Limits.Total, err
	},
	media.KindTVShow: func(ctx context.Context, b *Backend, q query, limits *kodirpc.ListLimits) ([]media.Item, int, error) {
		p := kodirpc.VideoLibraryGetTVShowsParams{Properties: tvShowFields, Limits: limits, Sort: q.sort}
		if rule, field, ok := q.rule("title"); ok {
			p.Filter = kodirpc.NewVideoLibraryGetTVShowsFilterFromListFilterTVShows(
				kodirpc.NewListFilterTVShowsFromListFilterRuleTVShows(kodirpc.ListFilterRuleTVShows{ListFilterRule: rule, Field: kodirpc.ListFilterFieldsTVShows(field)}))
		}

		r, err := b.c.VideoLibraryGetTVShows(ctx, p)

		return mapAll(r.Tvshows, b.m.tvShow), r.Limits.Total, err
	},
	media.KindEpisode: func(ctx context.Context, b *Backend, q query, limits *kodirpc.ListLimits) ([]media.Item, int, error) {
		p := kodirpc.VideoLibraryGetEpisodesParams{TvshowID: kodirpc.LibraryID(q.showID), Properties: episodeFields, Limits: limits, Sort: q.sort}
		if rule, field, ok := q.rule("title"); ok {
			p.Filter = kodirpc.NewVideoLibraryGetEpisodesFilterFromListFilterEpisodes(
				kodirpc.NewListFilterEpisodesFromListFilterRuleEpisodes(kodirpc.ListFilterRuleEpisodes{ListFilterRule: rule, Field: kodirpc.ListFilterFieldsEpisodes(field)}))
		}

		r, err := b.c.VideoLibraryGetEpisodes(ctx, p)

		return mapAll(r.Episodes, b.m.episode), r.Limits.Total, err
	},
	media.KindMusicVideo: func(ctx context.Context, b *Backend, q query, limits *kodirpc.ListLimits) ([]media.Item, int, error) {
		p := kodirpc.VideoLibraryGetMusicVideosParams{Properties: musicVideoFields, Limits: limits, Sort: q.sort}
		if rule, field, ok := q.rule("title"); ok {
			p.Filter = kodirpc.NewVideoLibraryGetMusicVideosFilterFromListFilterMusicVideos(
				kodirpc.NewListFilterMusicVideosFromListFilterRuleMusicVideos(kodirpc.ListFilterRuleMusicVideos{ListFilterRule: rule, Field: kodirpc.ListFilterFieldsMusicVideos(field)}))
		}

		r, err := b.c.VideoLibraryGetMusicVideos(ctx, p)

		return mapAll(r.Musicvideos, b.m.musicVideo), r.Limits.Total, err
	},
	media.KindArtist: func(ctx context.Context, b *Backend, q query, limits *kodirpc.ListLimits) ([]media.Item, int, error) {
		p := kodirpc.AudioLibraryGetArtistsParams{Properties: artistFields, Limits: limits, Sort: q.sort}
		if rule, field, ok := q.rule("artist"); ok {
			p.Filter = kodirpc.NewAudioLibraryGetArtistsFilterFromListFilterArtists(
				kodirpc.NewListFilterArtistsFromListFilterRuleArtists(kodirpc.ListFilterRuleArtists{ListFilterRule: rule, Field: kodirpc.ListFilterFieldsArtists(field)}))
		}

		r, err := b.c.AudioLibraryGetArtists(ctx, p)

		return mapAll(r.Artists, b.m.artist), r.Limits.Total, err
	},
	media.KindAlbum: func(ctx context.Context, b *Backend, q query, limits *kodirpc.ListLimits) ([]media.Item, int, error) {
		p := kodirpc.AudioLibraryGetAlbumsParams{Properties: albumFields, Limits: limits, Sort: q.sort}
		if rule, field, ok := q.rule("album"); ok {
			p.Filter = kodirpc.NewAudioLibraryGetAlbumsFilterFromListFilterAlbums(
				kodirpc.NewListFilterAlbumsFromListFilterRuleAlbums(kodirpc.ListFilterRuleAlbums{ListFilterRule: rule, Field: kodirpc.ListFilterFieldsAlbums(field)}))
		}

		r, err := b.c.AudioLibraryGetAlbums(ctx, p)

		return mapAll(r.Albums, b.m.album), r.Limits.Total, err
	},
	media.KindSong: func(ctx context.Context, b *Backend, q query, limits *kodirpc.ListLimits) ([]media.Item, int, error) {
		p := kodirpc.AudioLibraryGetSongsParams{Properties: songFields, Limits: limits, Sort: q.sort}
		if rule, field, ok := q.rule("title"); ok {
			p.Filter = kodirpc.NewAudioLibraryGetSongsFilterFromListFilterSongs(
				kodirpc.NewListFilterSongsFromListFilterRuleSongs(kodirpc.ListFilterRuleSongs{ListFilterRule: rule, Field: kodirpc.ListFilterFieldsSongs(field)}))
		}

		r, err := b.c.AudioLibraryGetSongs(ctx, p)

		return mapAll(r.Songs, b.m.song), r.Limits.Total, err
	},
}

func mapAll[T any](in []T, f func(*T) media.Item) []media.Item {
	out := make([]media.Item, len(in))
	for i := range in {
		out[i] = f(&in[i])
	}

	return out
}

// When stop is set, reading ends after the first page containing an item for which it returns true.
func (b *Backend) list(ctx context.Context, kind media.Kind, q query, stop func(media.Item) bool) ([]media.Item, error) {
	fetch, ok := fetchers[kind]
	if !ok {
		return nil, fmt.Errorf("kodi: %q: %w", kind, errUnsupportedKind)
	}

	var out []media.Item

	for start := 0; ; {
		page, total, err := fetch(ctx, b, q, &kodirpc.ListLimits{Start: start, End: kodirpc.ListAmount(start + b.pageSize)})
		if err != nil {
			return nil, fmt.Errorf("listing %ss: %w", kind, err)
		}

		out = append(out, page...)
		start += len(page)

		if len(page) == 0 || start >= total || stop != nil && slices.ContainsFunc(page, stop) {
			return out, nil
		}
	}
}

func sortEpisodes(items []media.Item) {
	slices.SortStableFunc(items, func(a, b media.Item) int {
		return cmp.Or(cmp.Compare(a.Season, b.Season), cmp.Compare(a.Episode, b.Episode))
	})
}

func sortNewestFirst(items []media.Item) {
	slices.SortStableFunc(items, func(a, b media.Item) int { return b.DateAdded.Compare(a.DateAdded) })
}

func filterTitle(items []media.Item, title string) []media.Item {
	title = strings.ToLower(title)

	var out []media.Item

	for i := range items {
		if strings.Contains(strings.ToLower(items[i].Title), title) {
			out = append(out, items[i])
		}
	}

	return out
}

// Kodi doesn't number playlist files, so the ID is derived from the path.
func playlistID(path string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(path))

	return int(h.Sum32() & 0x7fffffff)
}

// Also remembers each playlist's path for Play.
func (b *Backend) playlistItems(ctx context.Context) ([]media.Item, error) {
	items, _, err := b.listPlaylists(ctx)

	return items, err
}

// listPlaylists lists the playlists and remembers their files, which it also returns so a caller needn't read them back.
func (b *Backend) listPlaylists(ctx context.Context) ([]media.Item, map[int]playlistFile, error) {
	var out []media.Item

	paths := map[int]playlistFile{}

	for _, d := range playlistDirs {
		r, err := b.c.FilesGetDirectory(ctx, kodirpc.FilesGetDirectoryParams{Directory: d.dir, Media: d.media})
		if err != nil {
			// A profile without the folder answers with an error; that just means no playlists.
			if errors.Is(err, media.ErrNotFound) || isRPCError(err) {
				continue
			}

			return nil, nil, fmt.Errorf("listing playlists: %w", err)
		}

		for i := range r.Files {
			f := &r.Files[i]
			if f.Filetype == kodirpc.ListItemFileFiletypeDirectory {
				continue
			}

			id := playlistID(f.File)
			paths[id] = playlistFile{path: f.File, media: d.media}
			out = append(out, media.Item{Kind: media.KindPlaylist, ID: id, Title: titleOr(f.Label, f.File)})
		}
	}

	b.mu.Lock()
	b.playlists = paths
	b.mu.Unlock()

	return out, paths, nil
}

func isRPCError(err error) bool {
	var rpcErr *RPCError

	return errors.As(err, &rpcErr)
}

func (b *Backend) playlistItem(ctx context.Context, id int) (media.Item, error) {
	items, err := b.playlistItems(ctx)
	if err != nil {
		return media.Item{}, err
	}

	for i := range items {
		if items[i].ID == id {
			return items[i], nil
		}
	}

	return media.Item{}, fmt.Errorf("kodi: playlist %d: %w", id, media.ErrNotFound)
}

// Re-lists the playlists when the ID isn't known yet.
func (b *Backend) playlistFile(ctx context.Context, id int) (playlistFile, error) {
	b.mu.Lock()
	pl, ok := b.playlists[id]
	b.mu.Unlock()

	if ok {
		return pl, nil
	}

	_, paths, err := b.listPlaylists(ctx)
	if err != nil {
		return playlistFile{}, err
	}

	if pl, ok = paths[id]; !ok {
		return playlistFile{}, fmt.Errorf("kodi: playlist %d: %w", id, media.ErrNotFound)
	}

	return pl, nil
}
