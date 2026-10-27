package kodi

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// Input implements media.Backend.
func (b *Backend) Input(ctx context.Context, action media.InputAction) error {
	var err error

	switch action {
	case media.InputUp:
		_, err = b.c.InputUp(ctx)
	case media.InputDown:
		_, err = b.c.InputDown(ctx)
	case media.InputLeft:
		_, err = b.c.InputLeft(ctx)
	case media.InputRight:
		_, err = b.c.InputRight(ctx)
	case media.InputSelect:
		_, err = b.c.InputSelect(ctx)
	case media.InputBack:
		_, err = b.c.InputBack(ctx)
	case media.InputHome:
		_, err = b.c.InputHome(ctx)
	case media.InputInfo:
		_, err = b.c.InputInfo(ctx)
	case media.InputContextMenu:
		_, err = b.c.InputContextMenu(ctx)
	case media.InputOSD:
		_, err = b.c.InputShowOSD(ctx)
	case media.InputFullscreen:
		_, err = b.c.GUISetFullscreen(ctx, kodirpc.GUISetFullscreenParams{Fullscreen: kodirpc.NewGlobalToggleFromString(kodirpc.GlobalToggleStringToggle)})
	case media.InputPageUp:
		err = b.action(ctx, "pageup")
	case media.InputPageDown:
		err = b.action(ctx, "pagedown")
	case media.InputStereoscopic:
		err = b.action(ctx, "togglestereomode")
	case media.InputPassthrough:
		err = b.action(ctx, "audiotoggledigital")
	default:
		return fmt.Errorf("kodi: unknown input action %q", action)
	}

	return err
}

func (b *Backend) action(ctx context.Context, name kodirpc.InputAction) error {
	_, err := b.c.InputExecuteAction(ctx, kodirpc.InputExecuteActionParams{Action: name})

	return err
}

// SendText implements media.Backend. The text is left in the field for the user to confirm, as with a keyboard.
func (b *Backend) SendText(ctx context.Context, text string) error {
	_, err := b.c.InputSendText(ctx, kodirpc.InputSendTextParams{Text: text, Done: ptr(false)})

	return err
}

var sectionPaths = map[media.Section]struct {
	window kodirpc.GUIWindow
	path   string
}{
	media.SectionMovies:            {windowVideos, "videodb://movies/titles/"},
	media.SectionTVShows:           {windowVideos, "videodb://tvshows/titles/"},
	media.SectionMusicVideos:       {windowVideos, "videodb://musicvideos/titles/"},
	media.SectionMusic:             {windowMusic, "musicdb://"},
	media.SectionArtists:           {windowMusic, "musicdb://artists/"},
	media.SectionAlbums:            {windowMusic, "musicdb://albums/"},
	media.SectionRecentMovies:      {windowVideos, "videodb://recentlyaddedmovies/"},
	media.SectionRecentEpisodes:    {windowVideos, "videodb://recentlyaddedepisodes/"},
	media.SectionRecentMusicVideos: {windowVideos, "videodb://recentlyaddedmusicvideos/"},
	media.SectionRecentMusic:       {windowMusic, "musicdb://recentlyaddedalbums/"},
}

var genreRoots = map[media.Section]struct {
	window kodirpc.GUIWindow
	root   string
	video  kodirpc.VideoLibraryGetGenresType
}{
	media.SectionMovies:      {windowVideos, "videodb://movies/genres/", "movie"},
	media.SectionTVShows:     {windowVideos, "videodb://tvshows/genres/", "tvshow"},
	media.SectionMusicVideos: {windowVideos, "videodb://musicvideos/genres/", "musicvideo"},
	media.SectionMusic:       {windowMusic, "musicdb://genres/", ""},
}

// ShowView implements media.Backend.
func (b *Backend) ShowView(ctx context.Context, v media.View) error {
	switch v.Section {
	case media.SectionFavourites:
		return b.activate(ctx, windowFavourites)
	case media.SectionSubtitleSearch:
		return b.activate(ctx, windowSubtitleSearch)
	case media.SectionPlaylist:
		pl, err := b.playlistFile(ctx, v.Playlist.ID)
		if err != nil {
			return err
		}

		window := windowVideos
		if pl.media == kodirpc.FilesMediaMusic {
			window = windowMusic
		}

		return b.activate(ctx, window, pl.path, "return")
	}

	if v.Genre != "" {
		g, ok := genreRoots[v.Section]
		if !ok {
			return fmt.Errorf("kodi: %s has no genres: %w", v.Section, errUnsupportedKind)
		}

		id, err := b.genreID(ctx, g.video, v.Genre)
		if err != nil {
			return err
		}

		return b.activate(ctx, g.window, g.root+strconv.Itoa(id)+"/", "return")
	}

	s, ok := sectionPaths[v.Section]
	if !ok {
		return fmt.Errorf("kodi: unknown section %q", v.Section)
	}

	return b.activate(ctx, s.window, s.path, "return")
}

func (b *Backend) activate(ctx context.Context, window kodirpc.GUIWindow, params ...string) error {
	_, err := b.c.GUIActivateWindow(ctx, kodirpc.GUIActivateWindowParams{Window: window, Parameters: params})

	return err
}

// Ignores case. An empty video type means the music library.
func (b *Backend) genreID(ctx context.Context, video kodirpc.VideoLibraryGetGenresType, name string) (int, error) {
	var genres []kodirpc.LibraryDetailsGenre

	if video == "" {
		r, err := b.c.AudioLibraryGetGenres(ctx, kodirpc.AudioLibraryGetGenresParams{})
		if err != nil {
			return 0, err
		}

		genres = r.Genres
	} else {
		r, err := b.c.VideoLibraryGetGenres(ctx, kodirpc.VideoLibraryGetGenresParams{Type: video})
		if err != nil {
			return 0, err
		}

		genres = r.Genres
	}

	for _, g := range genres {
		if strings.EqualFold(titleOr(g.Title, g.Label), name) {
			return int(g.GenreID), nil
		}
	}

	return 0, fmt.Errorf("kodi: genre %q: %w", name, media.ErrNotFound)
}

// Favourites implements media.Backend.
func (b *Backend) Favourites(ctx context.Context) ([]media.Favourite, error) {
	r, err := b.c.FavouritesGetFavourites(ctx, kodirpc.FavouritesGetFavouritesParams{
		Properties: kodirpc.FavouriteFieldsFavourite{
			kodirpc.FavouriteFieldsFavouriteItemWindow, kodirpc.FavouriteFieldsFavouriteItemWindowparameter, kodirpc.FavouriteFieldsFavouriteItemPath,
		},
	})
	if err != nil {
		return nil, err
	}

	out := make([]media.Favourite, 0, len(r.Favourites))

	for _, f := range r.Favourites {
		out = append(out, media.Favourite{Title: f.Title, Type: string(f.Type), Path: f.Path, Window: f.Window, WindowParameter: f.Windowparameter})
	}

	return out, nil
}

// OpenFavourite implements media.Backend. Media favourites play, window favourites open their window, and script favourites naming an add-on run it.
func (b *Backend) OpenFavourite(ctx context.Context, f media.Favourite) error {
	switch f.Type {
	case "media":
		return b.PlayFile(ctx, f.Path)
	case "window":
		var params []string
		if f.WindowParameter != "" {
			params = []string{f.WindowParameter}
		}

		return b.activate(ctx, kodirpc.GUIWindow(f.Window), params...)
	case "script":
		if f.Path != "" && !strings.ContainsAny(f.Path, "/\\") {
			return b.ExecuteAddon(ctx, f.Path, nil)
		}
	}

	return fmt.Errorf("%w: “%s” (%s)", errFavouriteType, f.Title, f.Type)
}

var launchable = map[kodirpc.AddonTypes]bool{
	"xbmc.python.pluginsource": true,
	"xbmc.python.script":       true,
}

// Addons implements media.Backend.
func (b *Backend) Addons(ctx context.Context) ([]media.Addon, error) {
	r, err := b.c.AddonsGetAddons(ctx, kodirpc.AddonsGetAddonsParams{
		Enabled:    kodirpc.NewAddonsGetAddonsEnabledFromBool(true),
		Properties: kodirpc.AddonFields{"name"},
	})
	if err != nil {
		return nil, err
	}

	var out []media.Addon

	for _, a := range r.Addons {
		if launchable[a.Type] {
			out = append(out, media.Addon{ID: a.AddonID, Name: titleOr(a.Name, a.AddonID), Type: string(a.Type)})
		}
	}

	return out, nil
}

// ExecuteAddon implements media.Backend.
func (b *Backend) ExecuteAddon(ctx context.Context, id string, params map[string]string) error {
	p := kodirpc.AddonsExecuteAddonParams{AddonID: id}
	if len(params) > 0 {
		p.Params = kodirpc.NewAddonsExecuteAddonParams2FromObject(params)
	}

	_, err := b.c.AddonsExecuteAddon(ctx, p)

	return err
}
