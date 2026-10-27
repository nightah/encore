package kodi

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

var _ media.Backend = (*Backend)(nil)

// NewBackend wraps c.
func NewBackend(c *Client, opts BackendOptions) *Backend {
	if opts.PageSize <= 0 {
		opts.PageSize = DefaultPageSize
	}

	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}

	return &Backend{
		c:         c,
		name:      opts.Name,
		pageSize:  opts.PageSize,
		m:         mapper{zone: newZone(opts.Location)},
		log:       opts.Logger,
		shuffle:   rand.Shuffle,
		playlists: map[int]playlistFile{},
	}
}

// Client returns the underlying client.
func (b *Backend) Client() *Client { return b.c }

// Info reports Kodi's friendly name, version and API version.
func (b *Backend) Info(ctx context.Context) (media.Info, error) {
	props, err := b.c.ApplicationGetProperties(ctx, kodirpc.ApplicationGetPropertiesParams{
		Properties: []kodirpc.ApplicationPropertyName{"name", "version"},
	})
	if err != nil {
		return media.Info{}, err
	}

	api, err := b.c.APIVersion(ctx)
	if err != nil {
		return media.Info{}, err
	}

	info := media.Info{Name: b.name, APIVersion: api.String()}

	if labels, err := b.c.XBMCGetInfoLabels(ctx, kodirpc.XBMCGetInfoLabelsParams{Labels: []string{"System.FriendlyName"}}); err == nil {
		if name := labels["System.FriendlyName"]; name != "" {
			info.Name = name
		}
	}

	if info.Name == "" {
		info.Name = props.Name
	}

	info.Version = formatVersion(props.Version)

	return info, nil
}

// Search returns items of kind whose title contains title, asking Kodi directly rather than any cache. The library catalog uses it to find items it hasn't heard about yet.
func (b *Backend) Search(ctx context.Context, kind media.Kind, title string) ([]media.Item, error) {
	b.m.zone.refresh(ctx, b.localNow)

	if kind == media.KindPlaylist {
		all, err := b.playlistItems(ctx)
		if err != nil {
			return nil, err
		}

		return filterTitle(all, title), nil
	}

	return b.list(ctx, kind, query{title: title}, nil)
}

// Library implements media.Backend.
func (b *Backend) Library(ctx context.Context, kind media.Kind) ([]media.Item, error) {
	b.m.zone.refresh(ctx, b.localNow)

	switch kind {
	case media.KindEpisode:
		return nil, fmt.Errorf("kodi: episodes are listed per show: %w", errUnsupportedKind)
	case media.KindPlaylist:
		return b.playlistItems(ctx)
	}

	return b.list(ctx, kind, query{}, nil)
}

// Episodes implements media.Backend.
func (b *Backend) Episodes(ctx context.Context, showID int) ([]media.Item, error) {
	b.m.zone.refresh(ctx, b.localNow)

	items, err := b.list(ctx, media.KindEpisode, query{showID: showID}, nil)
	if err != nil {
		return nil, err
	}

	sortEpisodes(items)

	return items, nil
}

// RecentlyAdded implements media.Backend. Lists are read newest first and stop at the first item at or before since.
func (b *Backend) RecentlyAdded(ctx context.Context, kind media.Kind, since time.Time) ([]media.Item, error) {
	b.m.zone.refresh(ctx, b.localNow)

	if kind == media.KindPlaylist {
		return nil, nil
	}

	q := query{sort: &kodirpc.ListSort{Method: kodirpc.ListSortMethodDateadded, Order: kodirpc.ListSortOrderDescending}}
	old := func(it media.Item) bool { return !it.DateAdded.After(since) }

	// Artists can't be sorted by date added on every Kodi version, so read them all.
	stop := old
	if kind == media.KindArtist {
		stop = nil
	}

	items, err := b.list(ctx, kind, q, stop)
	if err != nil {
		return nil, err
	}

	out := slices.DeleteFunc(items, old)

	sortNewestFirst(out)

	return out, nil
}

// Item implements media.Backend.
func (b *Backend) Item(ctx context.Context, ref media.Ref) (media.Item, error) {
	b.m.zone.refresh(ctx, b.localNow)

	id := kodirpc.LibraryID(ref.ID)

	var (
		it  media.Item
		err error
	)

	switch ref.Kind {
	case media.KindMovie:
		var r kodirpc.VideoLibraryGetMovieDetailsResult
		if r, err = b.c.VideoLibraryGetMovieDetails(ctx, kodirpc.VideoLibraryGetMovieDetailsParams{MovieID: id, Properties: movieFields}); err == nil && r.Moviedetails != nil {
			it = b.m.movie(r.Moviedetails)
		}
	case media.KindTVShow:
		var r kodirpc.VideoLibraryGetTVShowDetailsResult
		if r, err = b.c.VideoLibraryGetTVShowDetails(ctx, kodirpc.VideoLibraryGetTVShowDetailsParams{TvshowID: id, Properties: tvShowFields}); err == nil && r.Tvshowdetails != nil {
			it = b.m.tvShow(r.Tvshowdetails)
		}
	case media.KindEpisode:
		var r kodirpc.VideoLibraryGetEpisodeDetailsResult
		if r, err = b.c.VideoLibraryGetEpisodeDetails(ctx, kodirpc.VideoLibraryGetEpisodeDetailsParams{EpisodeID: id, Properties: episodeFields}); err == nil && r.Episodedetails != nil {
			it = b.m.episode(r.Episodedetails)
		}
	case media.KindMusicVideo:
		var r kodirpc.VideoLibraryGetMusicVideoDetailsResult
		if r, err = b.c.VideoLibraryGetMusicVideoDetails(ctx, kodirpc.VideoLibraryGetMusicVideoDetailsParams{MusicvideoID: id, Properties: musicVideoFields}); err == nil && r.Musicvideodetails != nil {
			it = b.m.musicVideo(r.Musicvideodetails)
		}
	case media.KindArtist:
		var r kodirpc.AudioLibraryGetArtistDetailsResult
		if r, err = b.c.AudioLibraryGetArtistDetails(ctx, kodirpc.AudioLibraryGetArtistDetailsParams{ArtistID: id, Properties: artistFields}); err == nil && r.Artistdetails != nil {
			it = b.m.artist(r.Artistdetails)
		}
	case media.KindAlbum:
		var r kodirpc.AudioLibraryGetAlbumDetailsResult
		if r, err = b.c.AudioLibraryGetAlbumDetails(ctx, kodirpc.AudioLibraryGetAlbumDetailsParams{AlbumID: id, Properties: albumFields}); err == nil && r.Albumdetails != nil {
			it = b.m.album(r.Albumdetails)
		}
	case media.KindSong:
		var r kodirpc.AudioLibraryGetSongDetailsResult
		if r, err = b.c.AudioLibraryGetSongDetails(ctx, kodirpc.AudioLibraryGetSongDetailsParams{SongID: id, Properties: songFields}); err == nil && r.Songdetails != nil {
			it = b.m.song(r.Songdetails)
		}
	case media.KindPlaylist:
		return b.playlistItem(ctx, ref.ID)
	default:
		return media.Item{}, fmt.Errorf("kodi: %q: %w", ref.Kind, errUnsupportedKind)
	}

	if err != nil {
		return media.Item{}, err
	}

	if it.Kind == "" {
		return media.Item{}, fmt.Errorf("kodi: %s %d: %w", ref.Kind, ref.ID, media.ErrNotFound)
	}

	return it, nil
}

// NextEpisode implements media.Backend. An episode in progress wins; otherwise it's the first unwatched episode, with specials (season 0) only when nothing else is left.
func (b *Backend) NextEpisode(ctx context.Context, showID int) (media.Item, error) {
	b.m.zone.refresh(ctx, b.localNow)

	unwatched, err := b.list(ctx, media.KindEpisode, query{showID: showID, unwatched: true}, nil)
	if err != nil {
		return media.Item{}, err
	}

	sortEpisodes(unwatched)

	var first, firstSpecial *media.Item

	for i := range unwatched {
		ep := &unwatched[i]

		if ep.InProgress() {
			return *ep, nil
		}

		switch {
		case ep.Season > 0 && first == nil:
			first = ep
		case ep.Season == 0 && firstSpecial == nil:
			firstSpecial = ep
		}
	}

	switch {
	case first != nil:
		return *first, nil
	case firstSpecial != nil:
		return *firstSpecial, nil
	}

	return media.Item{}, fmt.Errorf("kodi: show %d has no unwatched episodes: %w", showID, media.ErrNotFound)
}
