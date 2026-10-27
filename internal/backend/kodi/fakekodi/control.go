package fakekodi

import (
	"encoding/json"
	"slices"
	"strings"
)

// Kodi reports BCP 47 from Kodi 22 (API 13.1000), ISO 639-2 before.
func (s *Server) lang(bcp47, iso6392 string) string {
	if v := s.opts.Version; v[0] > 13 || (v[0] == 13 && v[1] >= 1000) {
		return bcp47
	}

	return iso6392
}

func (s *Server) videoStreams() (audio, subs []Stream) {
	audio = []Stream{
		{Index: 0, Name: "English 5.1", Language: s.lang("en", "eng")},
		{Index: 1, Name: "Commentary", Language: s.lang("en", "eng")},
		{Index: 2, Name: "Français", Language: s.lang("fr", "fre")},
	}
	subs = []Stream{
		{Index: 0, Name: "English", Language: s.lang("en", "eng")},
		{Index: 1, Name: "Deutsch", Language: s.lang("de", "ger")},
		{Index: 2, Name: "Unknown", Language: ""},
	}

	return audio, subs
}

func (s *Server) streamPropertiesLocked(props map[string]any) {
	props["repeat"] = s.state.repeat
	props["subtitleenabled"] = false
	props["audiostreams"] = []Stream{}
	props["subtitles"] = []Stream{}
	props["currentaudiostream"] = map[string]any{}
	props["currentsubtitle"] = map[string]any{}

	if s.state.playing.playerID() != videoPlayerID {
		return
	}

	audio, subs := s.videoStreams()
	props["audiostreams"], props["subtitles"] = audio, subs
	props["subtitleenabled"] = s.state.subtitlesOn
	props["currentaudiostream"] = audio[s.state.audio]
	props["currentsubtitle"] = subs[s.state.subtitle]
}

func (s *Server) registerControl() {
	ok := func(json.RawMessage) (any, *Error) { return "OK", nil }

	for _, m := range []string{
		"Input.Up", "Input.Down", "Input.Left", "Input.Right", "Input.Select", "Input.Back", "Input.Home", "Input.Info",
		"Input.ContextMenu", "Input.ShowOSD", "Input.ExecuteAction", "Input.SendText", "GUI.ActivateWindow",
		"System.Shutdown", "System.Reboot", "System.Hibernate", "System.Suspend", "System.EjectOpticalDrive", "Application.Quit",
		"VideoLibrary.Scan", "VideoLibrary.Clean", "AudioLibrary.Scan", "AudioLibrary.Clean", "Addons.ExecuteAddon",
	} {
		s.handlers[m] = ok
	}

	s.handlers["GUI.SetFullscreen"] = func(json.RawMessage) (any, *Error) { return true, nil }
	s.handlers["Player.SetShuffle"] = s.playerCommand(func(json.RawMessage) *Error { return nil })
	s.handlers["Player.Zoom"] = s.playerCommand(func(json.RawMessage) *Error { return nil })
	s.handlers["Player.Rotate"] = s.playerCommand(func(json.RawMessage) *Error { return nil })
	s.handlers["Player.Move"] = s.playerCommand(func(json.RawMessage) *Error { return nil })
	s.handlers["Player.SetRepeat"] = s.playerCommand(s.setRepeatLocked)
	s.handlers["Player.SetSubtitle"] = s.playerCommand(s.setSubtitleLocked)
	s.handlers["Player.SetAudioStream"] = s.playerCommand(s.setAudioStreamLocked)
	s.handlers["VideoLibrary.GetGenres"] = s.videoGenres
	s.handlers["AudioLibrary.GetGenres"] = s.audioGenres
	s.handlers["Favourites.GetFavourites"] = s.favourites
	s.handlers["Addons.GetAddons"] = s.addons
}

// Like Kodi's Player.* methods, the command fails without an active player.
func (s *Server) playerCommand(apply func(json.RawMessage) *Error) HandlerFunc {
	return func(params json.RawMessage) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.state.playing == nil {
			return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
		}

		if err := apply(params); err != nil {
			return nil, err
		}

		return "OK", nil
	}
}

func (s *Server) setRepeatLocked(params json.RawMessage) *Error {
	var p struct {
		Repeat string `json:"repeat"`
	}
	if err := decode(params, &p); err != nil {
		return err
	}

	switch p.Repeat {
	case "cycle":
		s.state.repeat = map[string]string{"off": "one", "one": "all", "all": "off"}[s.state.repeat]
	case "off", "one", "all":
		s.state.repeat = p.Repeat
	default:
		return &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	return nil
}

// Kodi accepts "next", "previous", "on", "off" or an index.
func streamParam(raw json.RawMessage) (string, int, bool) {
	var (
		word  string
		index int
	)

	if json.Unmarshal(raw, &word) == nil {
		return word, 0, true
	}

	if json.Unmarshal(raw, &index) == nil {
		return "", index, true
	}

	return "", 0, false
}

func stepIndex(cur, n int, word string, index int) (int, bool) {
	switch word {
	case "next":
		return (cur + 1) % n, true
	case "previous":
		return (cur - 1 + n) % n, true
	case "":
		return index, index >= 0 && index < n
	}

	return 0, false
}

func (s *Server) setSubtitleLocked(params json.RawMessage) *Error {
	var p struct {
		Subtitle json.RawMessage `json:"subtitle"`
		Enable   bool            `json:"enable"`
	}
	if err := decode(params, &p); err != nil {
		return err
	}

	word, index, ok := streamParam(p.Subtitle)
	if !ok {
		return &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	switch word {
	case "on", "off":
		s.state.subtitlesOn = word == "on"

		return nil
	}

	_, subs := s.videoStreams()

	i, ok := stepIndex(s.state.subtitle, len(subs), word, index)
	if !ok {
		return &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	s.state.subtitle = i
	s.state.subtitlesOn = s.state.subtitlesOn || p.Enable

	return nil
}

func (s *Server) setAudioStreamLocked(params json.RawMessage) *Error {
	var p struct {
		Stream json.RawMessage `json:"stream"`
	}
	if err := decode(params, &p); err != nil {
		return err
	}

	word, index, ok := streamParam(p.Stream)
	audio, _ := s.videoStreams()

	if ok {
		s.state.audio, ok = stepIndex(s.state.audio, len(audio), word, index)
	}

	if !ok {
		s.state.audio = 0

		return &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	return nil
}

func genreList(names ...[]string) any {
	var all []string
	for _, n := range names {
		all = append(all, n...)
	}

	slices.Sort(all)
	all = slices.Compact(all)

	genres := make([]map[string]any, len(all))
	for i, g := range all {
		genres[i] = map[string]any{"genreid": i + 1, "label": g, "title": g}
	}

	return map[string]any{"genres": genres, "limits": map[string]int{"start": 0, "end": len(genres), "total": len(genres)}}
}

func (s *Server) videoGenres(params json.RawMessage) (any, *Error) {
	var p struct {
		Type string `json:"type"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var names [][]string

	lib := s.opts.Library

	switch p.Type {
	case "movie":
		for _, m := range lib.Movies {
			names = append(names, m.Genre)
		}
	case "tvshow":
		for _, t := range lib.TVShows {
			names = append(names, t.Genre)
		}
	case "musicvideo":
		for _, v := range lib.MusicVideos {
			names = append(names, v.Genre)
		}
	default:
		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	return genreList(names...), nil
}

func (s *Server) audioGenres(json.RawMessage) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([][]string, 0, len(s.opts.Library.Songs))
	for _, song := range s.opts.Library.Songs {
		names = append(names, song.Genre)
	}

	return genreList(names...), nil
}

func (s *Server) favourites(json.RawMessage) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	favs := slices.Clone(s.opts.Library.Favourites)
	if favs == nil {
		// Kodi 21 reports no favourites as null.
		return map[string]any{"favourites": nil, "limits": map[string]int{"start": 0, "end": 0, "total": 0}}, nil
	}

	return map[string]any{"favourites": favs, "limits": map[string]int{"start": 0, "end": len(favs), "total": len(favs)}}, nil
}

func (s *Server) addons(params json.RawMessage) (any, *Error) {
	var p struct {
		Enabled json.RawMessage `json:"enabled"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	onlyEnabled := strings.TrimSpace(string(p.Enabled)) == "true"

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []map[string]any{}

	for _, a := range s.opts.Library.Addons {
		if onlyEnabled && !a.Enabled {
			continue
		}

		out = append(out, map[string]any{"addonid": a.AddonID, "type": a.Type, "name": a.Name})
	}

	return map[string]any{"addons": out, "limits": map[string]int{"start": 0, "end": len(out), "total": len(out)}}, nil
}

// Serves Files.GetDirectory on a playlist file. Callers hold s.mu.
func (s *Server) playlistEntriesLocked(lib *Library, path string) (any, bool) {
	for _, pl := range lib.Playlists {
		if pl.File != path {
			continue
		}

		files := make([]map[string]any, len(pl.Entries))
		for i, f := range pl.Entries {
			files[i] = map[string]any{"file": f, "filetype": "file", "label": f[strings.LastIndex(f, "/")+1:], "type": "unknown"}
		}

		return map[string]any{"files": files, "limits": map[string]int{"start": 0, "end": len(files), "total": len(files)}}, true
	}

	return nil, false
}
