package fakekodi

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

func (it *Item) length() float64 { return float64(max(it.Runtime, it.Duration)) }

func (it *Item) playerID() int {
	if it.Type == "song" {
		return audioPlayerID
	}

	return videoPlayerID
}

func movieItem(m *Movie) Item {
	return Item{
		Type: "movie", ID: m.MovieID, Label: m.Label, Title: m.Title, Year: m.Year, Genre: m.Genre, Runtime: m.Runtime, resume: m.Resume.Position,
		Art: map[string]string{"poster": fmt.Sprintf("image://fake/movie-%d-poster.png/", m.MovieID)},
	}
}

func episodeItem(e *Episode) Item {
	return Item{
		Type: "episode", ID: e.EpisodeID, Label: e.Label, Title: e.Title, ShowTitle: e.ShowTitle, TVShowID: e.TVShowID,
		Season: e.Season, Episode: e.Episode, Runtime: e.Runtime, resume: e.Resume.Position,
		Art: map[string]string{"thumb": fmt.Sprintf("image://fake/episode-%d-thumb.png/", e.EpisodeID)},
	}
}

func musicVideoItem(v *MusicVideo) Item {
	return Item{Type: "musicvideo", ID: v.MusicVideoID, Label: v.Label, Title: v.Title, Artist: v.Artist, Album: v.Album, Year: v.Year, Runtime: v.Runtime, resume: v.Resume.Position}
}

func songItem(s *Song) Item {
	return Item{Type: "song", ID: s.SongID, Label: s.Label, Title: s.Title, Artist: s.Artist, Album: s.Album, Year: s.Year, Genre: s.Genre, Duration: s.Duration}
}

func (s *Server) registerPlayer() {
	s.handlers["Player.GetActivePlayers"] = s.playerGetActivePlayers
	s.handlers["Player.Open"] = s.playerOpen
	s.handlers["Player.PlayPause"] = s.playerPlayPause
	s.handlers["Player.Stop"] = s.playerStop
	s.handlers["Player.GetItem"] = s.playerGetItem
	s.handlers["Player.GetProperties"] = s.playerGetProperties
	s.handlers["Player.Seek"] = s.playerSeek
	s.handlers["Player.GoTo"] = s.playerGoTo
	s.handlers["Playlist.Clear"] = s.playlistClear
	s.handlers["Playlist.Add"] = s.playlistAdd
}

func (s *Server) playerGetActivePlayers(json.RawMessage) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.playing == nil {
		return []any{}, nil
	}

	kind := "video"
	if s.state.playing.playerID() == audioPlayerID {
		kind = "audio"
	}

	return []map[string]any{{"playerid": s.state.playing.playerID(), "playertype": "internal", "type": kind}}, nil
}

// Callers hold s.mu.
func (s *Server) resolve(ref *itemRef) ([]Item, bool) {
	lib, unlock := s.lockLibrary()
	defer unlock()

	switch {
	case ref.MovieID != 0:
		if i := slices.IndexFunc(lib.Movies, func(m Movie) bool { return m.MovieID == ref.MovieID }); i >= 0 {
			return []Item{movieItem(&lib.Movies[i])}, true
		}
	case ref.EpisodeID != 0:
		if i := slices.IndexFunc(lib.Episodes, func(e Episode) bool { return e.EpisodeID == ref.EpisodeID }); i >= 0 {
			return []Item{episodeItem(&lib.Episodes[i])}, true
		}
	case ref.MusicVideoID != 0:
		if i := slices.IndexFunc(lib.MusicVideos, func(v MusicVideo) bool { return v.MusicVideoID == ref.MusicVideoID }); i >= 0 {
			return []Item{musicVideoItem(&lib.MusicVideos[i])}, true
		}
	case ref.SongID != 0:
		if i := slices.IndexFunc(lib.Songs, func(song Song) bool { return song.SongID == ref.SongID }); i >= 0 {
			return []Item{songItem(&lib.Songs[i])}, true
		}
	case ref.AlbumID != 0:
		songs := slices.Clone(lib.Songs)
		slices.SortStableFunc(songs, func(a, b Song) int { return a.Track - b.Track })

		return songsWhere(songs, func(song *Song) bool { return song.AlbumID == ref.AlbumID })
	case ref.ArtistID != 0:
		i := slices.IndexFunc(lib.Artists, func(a Artist) bool { return a.ArtistID == ref.ArtistID })
		if i < 0 {
			return nil, false
		}

		name := lib.Artists[i].Artist

		return songsWhere(lib.Songs, func(song *Song) bool { return slices.Contains(song.Artist, name) })
	case ref.PartyMode == "music":
		return songsWhere(lib.Songs, func(*Song) bool { return true })
	case ref.File != "":
		label := ref.File[strings.LastIndex(ref.File, "/")+1:]

		return []Item{{Type: "unknown", Label: label, File: ref.File}}, true
	case ref.PlaylistID != nil:
		items := s.state.playlists[*ref.PlaylistID]

		return slices.Clone(items), len(items) > 0
	}

	return nil, false
}

func songsWhere(songs []Song, keep func(*Song) bool) ([]Item, bool) {
	var out []Item

	for i := range songs {
		if keep(&songs[i]) {
			out = append(out, songItem(&songs[i]))
		}
	}

	return out, len(out) > 0
}

func (s *Server) playerOpen(params json.RawMessage) (any, *Error) {
	var p struct {
		Item    *itemRef `json:"item"`
		Options struct {
			Resume   json.RawMessage `json:"resume"`
			Shuffled *bool           `json:"shuffled"`
		} `json:"options"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	if p.Item == nil {
		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	s.mu.Lock()

	queue, ok := s.resolve(p.Item)
	if !ok {
		s.mu.Unlock()

		return nil, s.notFound()
	}

	index := 0
	if p.Item.PlaylistID != nil {
		index = min(p.Item.Position, len(queue)-1)
	}

	var resume bool
	_ = json.Unmarshal(p.Options.Resume, &resume)

	s.state.queue, s.state.index = queue, index
	s.state.shuffled = p.Options.Shuffled != nil && *p.Options.Shuffled
	s.startLocked(resume)
	item := *s.state.playing
	s.mu.Unlock()

	s.notifyPlay(&item)

	return "OK", nil
}

func (s *Server) startLocked(resume bool) {
	item := s.state.queue[s.state.index]
	s.state.playing, s.state.paused, s.state.position = &item, false, 0

	if resume {
		s.state.position = item.resume
	}
}

func (s *Server) notifyPlay(item *Item) {
	s.Notify("Player.OnPlay", map[string]any{
		"item":   notificationItem(item),
		"player": map[string]any{"playerid": item.playerID(), "speed": 1},
	})
}

func notificationItem(item *Item) map[string]any {
	if item.ID == 0 {
		return map[string]any{"type": item.Type, "title": item.Label}
	}

	return map[string]any{"type": item.Type, "id": item.ID}
}

func (s *Server) playerPlayPause(params json.RawMessage) (any, *Error) {
	var p struct {
		Play json.RawMessage `json:"play"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.playing == nil {
		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	var play bool
	if err := json.Unmarshal(p.Play, &play); err == nil {
		s.state.paused = !play
	} else {
		s.state.paused = !s.state.paused
	}

	return map[string]int{"speed": s.speedLocked()}, nil
}

func (s *Server) speedLocked() int {
	if s.state.playing == nil || s.state.paused {
		return 0
	}

	return 1
}

func (s *Server) playerStop(json.RawMessage) (any, *Error) {
	s.mu.Lock()
	item := s.state.playing
	s.state.playing = nil
	s.mu.Unlock()

	data := map[string]any{"end": false}
	if item != nil {
		data["item"] = notificationItem(item)
	}

	s.Notify("Player.OnStop", data)

	return "OK", nil
}

func (s *Server) playerGetItem(json.RawMessage) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.playing == nil {
		return map[string]any{"item": map[string]any{"type": "unknown", "label": ""}}, nil
	}

	return map[string]any{"item": s.state.playing}, nil
}

func globalTime(seconds float64) map[string]int {
	ms := int(seconds * 1000)

	return map[string]int{"hours": ms / 3600000, "minutes": ms / 60000 % 60, "seconds": ms / 1000 % 60, "milliseconds": ms % 1000}
}

func (s *Server) playerGetProperties(json.RawMessage) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.playing == nil {
		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	return s.propertiesLocked(), nil
}

func (s *Server) propertiesLocked() map[string]any {
	item := s.state.playing
	total := item.length()

	pct := 0.0
	if total > 0 {
		pct = s.state.position / total * 100
	}

	kind := "video"
	if item.playerID() == audioPlayerID {
		kind = "audio"
	}

	props := map[string]any{
		"type":       kind,
		"speed":      s.speedLocked(),
		"time":       globalTime(s.state.position),
		"totaltime":  globalTime(total),
		"percentage": pct,
		"playlistid": item.playerID(),
		"position":   s.state.index,
		"shuffled":   s.state.shuffled,
		"canseek":    true,
	}

	s.streamPropertiesLocked(props)

	return props
}

func (s *Server) playerSeek(params json.RawMessage) (any, *Error) {
	var p struct {
		Value struct {
			Percentage *float64 `json:"percentage"`
			Seconds    *int     `json:"seconds"`
			Step       string   `json:"step"`
			Time       *struct {
				Hours, Minutes, Seconds, Milliseconds int
			} `json:"time"`
		} `json:"value"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.playing == nil {
		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	v, total := p.Value, s.state.playing.length()
	pos := s.state.position

	switch {
	case v.Percentage != nil:
		pos = total * *v.Percentage / 100
	case v.Seconds != nil:
		pos += float64(*v.Seconds)
	case v.Time != nil:
		pos = float64(v.Time.Hours*3600+v.Time.Minutes*60+v.Time.Seconds) + float64(v.Time.Milliseconds)/1000
	case v.Step != "":
		steps := map[string]float64{"smallforward": smallStep, "smallbackward": -smallStep, "bigforward": bigStep, "bigbackward": -bigStep}

		step, ok := steps[v.Step]
		if !ok {
			return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
		}

		pos += step
	default:
		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	s.state.position = max(0, min(total, pos))
	props := s.propertiesLocked()

	return map[string]any{"percentage": props["percentage"], "time": props["time"], "totaltime": props["totaltime"]}, nil
}

func (s *Server) playerGoTo(params json.RawMessage) (any, *Error) {
	var p struct {
		To json.RawMessage `json:"to"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	s.mu.Lock()

	if s.state.playing == nil {
		s.mu.Unlock()

		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	var (
		to    string
		index int
	)

	switch {
	case json.Unmarshal(p.To, &to) == nil && to == "next":
		index = s.state.index + 1
	case to == "previous":
		index = s.state.index - 1
	case json.Unmarshal(p.To, &index) == nil:
	default:
		s.mu.Unlock()

		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	if index < 0 || index >= len(s.state.queue) {
		s.mu.Unlock()

		return "OK", nil
	}

	s.state.index = index
	s.startLocked(false)
	item := *s.state.playing
	s.mu.Unlock()

	s.notifyPlay(&item)

	return "OK", nil
}

func (s *Server) playlistClear(params json.RawMessage) (any, *Error) {
	var p struct {
		PlaylistID int `json:"playlistid"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	s.mu.Lock()
	delete(s.state.playlists, p.PlaylistID)
	s.mu.Unlock()

	return "OK", nil
}

func (s *Server) playlistAdd(params json.RawMessage) (any, *Error) {
	var p struct {
		PlaylistID int             `json:"playlistid"`
		Item       json.RawMessage `json:"item"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	var refs []itemRef
	if err := json.Unmarshal(p.Item, &refs); err != nil {
		var one itemRef
		if err := json.Unmarshal(p.Item, &one); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
		}

		refs = []itemRef{one}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range refs {
		items, ok := s.resolve(&refs[i])
		if !ok {
			return nil, s.notFound()
		}

		s.state.playlists[p.PlaylistID] = append(s.state.playlists[p.PlaylistID], items...)
	}

	return "OK", nil
}
