package fakekodi

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
)

func (s *Server) registerDefaults() {
	s.handlers["JSONRPC.Ping"] = func(json.RawMessage) (any, *Error) { return "pong", nil }
	s.handlers["JSONRPC.Version"] = s.version
	s.handlers["JSONRPC.Introspect"] = func(json.RawMessage) (any, *Error) {
		return map[string]any{"id": "http://xbmc.org/jsonrpc/ServiceDescription.json", "version": s.versionString(), "methods": map[string]any{}}, nil
	}
	s.handlers["Application.GetProperties"] = s.applicationGetProperties
	s.handlers["Application.SetVolume"] = s.applicationSetVolume
	s.handlers["Application.SetMute"] = s.applicationSetMute
	s.handlers["System.GetProperties"] = func(json.RawMessage) (any, *Error) {
		return map[string]bool{"canshutdown": true, "cansuspend": true, "canhibernate": false, "canreboot": true}, nil
	}
	s.handlers["XBMC.GetInfoLabels"] = s.getInfoLabels
	s.handlers["Files.GetDirectory"] = s.filesGetDirectory

	if s.opts.DatabaseName != "" {
		s.handlers["Database.GetDatabaseName"] = s.databaseGetDatabaseName
	}

	s.registerPlayer()
	s.registerControl()

	s.handlers["VideoLibrary.GetMovies"] = listOf(s, "movies", func(l *Library) []Movie { return l.Movies }, nil)
	s.handlers["VideoLibrary.GetTVShows"] = listOf(s, "tvshows", func(l *Library) []TVShow { return l.TVShows }, nil)
	s.handlers["VideoLibrary.GetEpisodes"] = listOf(s, "episodes", func(l *Library) []Episode { return l.Episodes },
		func(e Episode, p *listParams) bool {
			return (p.TVShowID == 0 || e.TVShowID == p.TVShowID) && (p.Season == nil || e.Season == *p.Season)
		})
	s.handlers["VideoLibrary.GetMusicVideos"] = listOf(s, "musicvideos", func(l *Library) []MusicVideo { return l.MusicVideos }, nil)
	s.handlers["AudioLibrary.GetArtists"] = listOf(s, "artists", func(l *Library) []Artist { return l.Artists }, nil)
	s.handlers["AudioLibrary.GetAlbums"] = listOf(s, "albums", func(l *Library) []Album { return l.Albums }, nil)
	s.handlers["AudioLibrary.GetSongs"] = listOf(s, "songs", func(l *Library) []Song { return l.Songs }, s.keepSong)

	s.handlers["VideoLibrary.GetMovieDetails"] = detailsOf(s, "movieid", "moviedetails", func(l *Library) []Movie { return l.Movies })
	s.handlers["VideoLibrary.GetTVShowDetails"] = detailsOf(s, "tvshowid", "tvshowdetails", func(l *Library) []TVShow { return l.TVShows })
	s.handlers["VideoLibrary.GetEpisodeDetails"] = detailsOf(s, "episodeid", "episodedetails", func(l *Library) []Episode { return l.Episodes })
	s.handlers["VideoLibrary.GetMusicVideoDetails"] = detailsOf(s, "musicvideoid", "musicvideodetails", func(l *Library) []MusicVideo { return l.MusicVideos })
	s.handlers["AudioLibrary.GetArtistDetails"] = detailsOf(s, "artistid", "artistdetails", func(l *Library) []Artist { return l.Artists })
	s.handlers["AudioLibrary.GetAlbumDetails"] = detailsOf(s, "albumid", "albumdetails", func(l *Library) []Album { return l.Albums })
	s.handlers["AudioLibrary.GetSongDetails"] = detailsOf(s, "songid", "songdetails", func(l *Library) []Song { return l.Songs })
}

func (s *Server) versionString() string {
	v := s.opts.Version

	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

func (s *Server) version(json.RawMessage) (any, *Error) {
	v := s.opts.Version

	return map[string]any{"version": map[string]int{"major": v[0], "minor": v[1], "patch": v[2]}}, nil
}

func (s *Server) getInfoLabels(params json.RawMessage) (any, *Error) {
	var p struct {
		Labels []string `json:"labels"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	s.mu.Lock()
	known := map[string]string{"Network.MacAddress": s.mac, "System.FriendlyName": "Kodi (fake)", "System.Time(hh:mm:ss xx)": s.state.clock}
	s.mu.Unlock()

	out := map[string]string{}
	for _, l := range p.Labels {
		out[l] = known[l]
	}

	return out, nil
}

func (s *Server) filesGetDirectory(params json.RawMessage) (any, *Error) {
	var p struct {
		Directory string `json:"directory"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	media := path.Base(strings.TrimSuffix(p.Directory, "/"))

	s.mu.Lock()
	defer s.mu.Unlock()

	lib, unlock := s.lockLibrary()
	defer unlock()

	if entries, ok := s.playlistEntriesLocked(lib, p.Directory); ok {
		return entries, nil
	}

	files := []map[string]any{}

	for _, pl := range lib.Playlists {
		if pl.Media == media && strings.HasPrefix(pl.File, strings.TrimSuffix(p.Directory, "/")+"/") {
			files = append(files, map[string]any{"file": pl.File, "filetype": "file", "label": pl.Label, "type": "unknown"})
		}
	}

	return map[string]any{"files": files, "limits": map[string]int{"start": 0, "end": len(files), "total": len(files)}}, nil
}

func (s *Server) applicationGetProperties(json.RawMessage) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return map[string]any{
		"volume":  s.state.volume,
		"muted":   s.state.muted,
		"name":    "Kodi",
		"version": map[string]any{"major": 21, "minor": 3, "revision": "20250316-ec8f3f5ba3", "tag": "stable"},
	}, nil
}

func (s *Server) applicationSetVolume(params json.RawMessage) (any, *Error) {
	var p struct {
		Volume json.RawMessage `json:"volume"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	s.mu.Lock()

	var (
		level int
		step  string
	)

	switch {
	case json.Unmarshal(p.Volume, &level) == nil:
		s.state.volume = max(0, min(100, level))
	case json.Unmarshal(p.Volume, &step) == nil && step == "increment":
		s.state.volume = min(100, s.state.volume+10)
	case step == "decrement":
		s.state.volume = max(0, s.state.volume-10)
	default:
		s.mu.Unlock()

		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	volume, muted := s.state.volume, s.state.muted
	s.mu.Unlock()

	s.Notify("Application.OnVolumeChanged", map[string]any{"volume": volume, "muted": muted})

	return volume, nil
}

func (s *Server) applicationSetMute(params json.RawMessage) (any, *Error) {
	var p struct {
		Mute json.RawMessage `json:"mute"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	s.mu.Lock()

	var toggle string
	if json.Unmarshal(p.Mute, &toggle) == nil && toggle == "toggle" {
		s.state.muted = !s.state.muted
	} else if err := json.Unmarshal(p.Mute, &s.state.muted); err != nil {
		s.mu.Unlock()

		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	volume, muted := s.state.volume, s.state.muted
	s.mu.Unlock()

	s.Notify("Application.OnVolumeChanged", map[string]any{"volume": volume, "muted": muted})

	return muted, nil
}

// The error for a missing library item depends on the API version.
func (s *Server) notFound() *Error {
	if v := s.opts.Version; v[0] > 13 || (v[0] == 13 && v[1] >= 1000) {
		return &Error{Code: CodeNotFound, Message: "Not found."}
	}

	return &Error{Code: CodeInvalidParams, Message: "Invalid params."}
}

// Applies AudioLibrary.GetSongs' albumid parameter and its albumid and artistid filters.
func (s *Server) keepSong(song Song, p *listParams) bool {
	if p.AlbumID != 0 && song.AlbumID != p.AlbumID {
		return false
	}

	f := p.Filter
	if f == nil {
		return true
	}

	if f.AlbumID != nil && song.AlbumID != *f.AlbumID {
		return false
	}

	if f.ArtistID != nil {
		s.mu.Lock()
		defer s.mu.Unlock()

		i := slices.IndexFunc(s.opts.Library.Artists, func(a Artist) bool { return a.ArtistID == *f.ArtistID })

		return i >= 0 && slices.Contains(song.Artist, s.opts.Library.Artists[i].Artist)
	}

	return true
}

// Only the title, playcount and dateadded rules are supported.
func (f *filter) matches(r record) bool {
	switch {
	case f == nil:
		return true
	case f.And != nil:
		for _, sub := range f.And {
			if !sub.matches(r) {
				return false
			}
		}

		return true
	case f.Or != nil:
		for _, sub := range f.Or {
			if sub.matches(r) {
				return true
			}
		}

		return false
	}

	value, _ := f.Value.(string)
	if values, ok := f.Value.([]any); ok && len(values) > 0 {
		value, _ = values[0].(string)
	}

	switch f.Field {
	case "title", "artist", "album":
		t, v := strings.ToLower(r.title()), strings.ToLower(value)

		switch f.Operator {
		case "is":
			return t == v
		case "contains":
			return strings.Contains(t, v)
		case "startswith":
			return strings.HasPrefix(t, v)
		}
	case "playcount":
		if f.Operator == "is" {
			return value == "0" && r.playcount() == 0 || value != "0" && r.playcount() > 0
		}
	case "dateadded":
		switch f.Operator {
		case "after", "greaterthan":
			return r.dateadded() > value
		case "before", "lessthan":
			return r.dateadded() < value
		}
	}

	return true
}

func page[T any](items []T, limits *listLimits) ([]T, map[string]int) {
	total := len(items)
	start, end := 0, total

	if limits != nil {
		start = min(limits.Start, total)
		if limits.End > 0 {
			end = min(limits.End, total)
		}
	}

	if end < start {
		end = start
	}

	return items[start:end], map[string]int{"start": start, "end": end, "total": total}
}

// keep applies method-specific parameters such as tvshowid.
func listOf[T record](s *Server, key string, items func(*Library) []T, keep func(T, *listParams) bool) HandlerFunc {
	return func(params json.RawMessage) (any, *Error) {
		var p listParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}

		s.mu.Lock()
		lib, unlock := s.lockLibrary()
		all := slices.Clone(items(lib))
		unlock()
		s.mu.Unlock()

		out := make([]T, 0, len(all))

		for _, it := range all {
			if (keep == nil || keep(it, &p)) && p.Filter.matches(it) {
				out = append(out, it)
			}
		}

		sortRecords(out, p.Sort.method(), p.Sort.descending())

		out, limits := page(out, p.Limits)

		// Kodi 21 answers most empty lists as [] (AudioLibrary.GetArtists omits the member); clients must accept both.
		return map[string]any{key: out, "limits": limits}, nil
	}
}

func (s *listSort) method() string {
	if s == nil {
		return ""
	}

	return s.Method
}

func (s *listSort) descending() bool {
	return s != nil && s.Order == "descending"
}

func sortRecords[T record](items []T, method string, desc bool) {
	cmp := func(a, b T) int {
		switch method {
		case "dateadded":
			return strings.Compare(a.dateadded(), b.dateadded())
		case "title", "label", "sorttitle":
			return strings.Compare(strings.ToLower(a.title()), strings.ToLower(b.title()))
		}

		if ea, ok := any(a).(Episode); ok {
			eb, _ := any(b).(Episode)
			if ea.Season != eb.Season {
				return ea.Season - eb.Season
			}

			return ea.Episode - eb.Episode
		}

		return 0
	}

	slices.SortStableFunc(items, func(a, b T) int {
		if desc {
			return cmp(b, a)
		}

		return cmp(a, b)
	})
}

func detailsOf[T record](s *Server, idParam, key string, items func(*Library) []T) HandlerFunc {
	return func(params json.RawMessage) (any, *Error) {
		var p map[string]json.RawMessage
		if err := decode(params, &p); err != nil {
			return nil, err
		}

		var id int
		if err := json.Unmarshal(p[idParam], &id); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		lib, unlock := s.lockLibrary()
		defer unlock()

		for _, it := range items(lib) {
			if it.id() == id {
				return map[string]any{key: it}, nil
			}
		}

		return nil, s.notFound()
	}
}
