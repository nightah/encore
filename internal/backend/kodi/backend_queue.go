package kodi

import (
	"context"
	"fmt"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// Queue implements media.Backend.
func (b *Backend) Queue(ctx context.Context, refs []media.Ref) error {
	if len(refs) == 0 {
		return fmt.Errorf("kodi: nothing to queue: %w", media.ErrNotFound)
	}

	var playlist kodirpc.PlaylistID

	items := make([]kodirpc.PlaylistItem, len(refs))

	for i, r := range refs {
		p, item, err := queueEntry(r)

		switch {
		case err != nil:
			return err
		case i > 0 && p != playlist:
			return errMixedQueue
		}

		playlist, items[i] = p, item
	}

	return b.queue(ctx, playlist, items)
}

// queueEntry is the playlist r is queued on and its item there.
func queueEntry(r media.Ref) (kodirpc.PlaylistID, kodirpc.PlaylistItem, error) {
	id := kodirpc.LibraryID(r.ID)

	switch r.Kind {
	case media.KindSong:
		return audioPlaylist, kodirpc.NewPlaylistItemFromSongID(kodirpc.PlaylistItemSongID{SongID: id}), nil
	case media.KindMovie:
		return videoPlaylist, kodirpc.NewPlaylistItemFromMovieID(kodirpc.PlaylistItemMovieID{MovieID: id}), nil
	case media.KindEpisode:
		return videoPlaylist, kodirpc.NewPlaylistItemFromEpisodeID(kodirpc.PlaylistItemEpisodeID{EpisodeID: id}), nil
	case media.KindMusicVideo:
		return videoPlaylist, kodirpc.NewPlaylistItemFromMusicvideoID(kodirpc.PlaylistItemMusicvideoID{MusicvideoID: id}), nil
	}

	return 0, kodirpc.PlaylistItem{}, fmt.Errorf("kodi: can't queue %q: %w", r.Kind, errUnsupportedKind)
}

// Replaces playlist with items, which callers make sure there are, and starts it from the top. The order is the caller's: Kodi's own shuffle is not used because it ignores long playlists.
func (b *Backend) queue(ctx context.Context, playlist kodirpc.PlaylistID, items []kodirpc.PlaylistItem) error {
	if _, err := b.c.PlaylistClear(ctx, kodirpc.PlaylistClearParams{PlaylistID: playlist}); err != nil {
		return err
	}

	for start := 0; start < len(items); start += queuePage {
		page := items[start:min(start+queuePage, len(items))]

		if _, err := b.c.PlaylistAdd(ctx, kodirpc.PlaylistAddParams{PlaylistID: playlist, Item: kodirpc.NewPlaylistAddItemFromArray(page)}); err != nil {
			return err
		}
	}

	_, err := b.c.PlayerOpen(ctx, kodirpc.PlayerOpenParams{
		Item: kodirpc.NewPlayerOpenItemFromPlaylistID(kodirpc.PlayerOpenItemPlaylistID{PlaylistID: playlist}),
	})

	return err
}

// A zero limit keeps every item.
func (b *Backend) shuffled(items []kodirpc.PlaylistItem, limit int) []kodirpc.PlaylistItem {
	b.shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })

	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	return items
}

func (b *Backend) playShuffled(ctx context.Context, ref media.Ref, opts media.PlayOptions) error {
	var (
		items    []kodirpc.PlaylistItem
		playlist = audioPlaylist
		err      error
	)

	switch ref.Kind {
	case media.KindArtist:
		items, err = b.songs(ctx, kodirpc.NewAudioLibraryGetSongsFilterFromArtistID(kodirpc.AudioLibraryGetSongsFilterArtistID{ArtistID: kodirpc.LibraryID(ref.ID)}))
	case media.KindAlbum:
		items, err = b.songs(ctx, kodirpc.NewAudioLibraryGetSongsFilterFromAlbumID(kodirpc.AudioLibraryGetSongsFilterAlbumID{AlbumID: kodirpc.LibraryID(ref.ID)}))
	case media.KindTVShow:
		playlist = videoPlaylist

		var episodes []media.Item
		if episodes, err = b.Episodes(ctx, ref.ID); err == nil {
			items = make([]kodirpc.PlaylistItem, len(episodes))
			for i := range episodes {
				items[i] = kodirpc.NewPlaylistItemFromEpisodeID(kodirpc.PlaylistItemEpisodeID{EpisodeID: kodirpc.LibraryID(episodes[i].ID)})
			}
		}
	default: // media.KindPlaylist: Play shuffles no other kinds.
		items, playlist, err = b.playlistEntries(ctx, ref.ID)
	}

	if err != nil {
		return err
	}

	if len(items) == 0 {
		return fmt.Errorf("kodi: %s %d is empty: %w", ref.Kind, ref.ID, media.ErrNotFound)
	}

	return b.queue(ctx, playlist, b.shuffled(items, opts.Limit))
}

func (b *Backend) songs(ctx context.Context, filter kodirpc.AudioLibraryGetSongsFilter) ([]kodirpc.PlaylistItem, error) {
	var out []kodirpc.PlaylistItem

	for start := 0; ; {
		r, err := b.c.AudioLibraryGetSongs(ctx, kodirpc.AudioLibraryGetSongsParams{
			Filter: filter,
			Limits: &kodirpc.ListLimits{Start: start, End: kodirpc.ListAmount(start + b.pageSize)},
		})
		if err != nil {
			return nil, fmt.Errorf("listing songs: %w", err)
		}

		for i := range r.Songs {
			out = append(out, kodirpc.NewPlaylistItemFromSongID(kodirpc.PlaylistItemSongID{SongID: r.Songs[i].SongID}))
		}

		start += len(r.Songs)

		if len(r.Songs) == 0 || start >= r.Limits.Total {
			return out, nil
		}
	}
}

// Smart playlists are expanded too.
func (b *Backend) playlistEntries(ctx context.Context, id int) ([]kodirpc.PlaylistItem, kodirpc.PlaylistID, error) {
	pl, err := b.playlistFile(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	playlist := videoPlaylist
	if pl.media == kodirpc.FilesMediaMusic {
		playlist = audioPlaylist
	}

	r, err := b.c.FilesGetDirectory(ctx, kodirpc.FilesGetDirectoryParams{Directory: pl.path, Media: pl.media})
	if err != nil {
		return nil, 0, fmt.Errorf("reading playlist %s: %w", pl.path, err)
	}

	var items []kodirpc.PlaylistItem

	for i := range r.Files {
		if f := &r.Files[i]; f.Filetype != kodirpc.ListItemFileFiletypeDirectory && f.File != "" {
			items = append(items, kodirpc.NewPlaylistItemFromFile(kodirpc.PlaylistItemFile{File: f.File}))
		}
	}

	return items, playlist, nil
}

// PlayFile implements media.Backend.
func (b *Backend) PlayFile(ctx context.Context, path string) error {
	return b.open(ctx, kodirpc.NewPlaylistItemFromFile(kodirpc.PlaylistItemFile{File: path}), nil)
}

// PartyMode implements media.Backend.
func (b *Backend) PartyMode(ctx context.Context) error {
	_, err := b.c.PlayerOpen(ctx, kodirpc.PlayerOpenParams{
		Item: kodirpc.NewPlayerOpenItemFromPartymode(kodirpc.PlayerOpenItemPartymode{
			Partymode: kodirpc.NewPlayerOpenItemPartymodePartymodeFromString(kodirpc.PlayerOpenItemPartymodePartymodeStringMusic),
		}),
	})

	return err
}
