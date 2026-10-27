package fakekodi

import (
	"encoding/json"
)

// SetPlayState changes a library movie's or episode's play state as if playback had just stopped on this server. The library is updated, so servers sharing it see the change, but only this server's clients are told: Player.OnStop, then VideoLibrary.OnUpdate carrying the play count when it changed, as Kodi does. kind is "movie" or "episode"; it reports false when there is no such item.
func (s *Server) SetPlayState(kind string, id int, st PlayState) bool {
	s.mu.Lock()
	lib, unlock := s.lockLibrary()

	var (
		found, countChanged bool
		lastPlayed          string
	)

	if !st.LastPlayed.IsZero() {
		lastPlayed = st.LastPlayed.Local().Format(TimeLayout)
	}

	switch kind {
	case "movie":
		for i := range lib.Movies {
			if m := &lib.Movies[i]; m.MovieID == id {
				found, countChanged = true, m.PlayCount != st.PlayCount
				m.PlayCount, m.Resume.Position = st.PlayCount, st.Resume

				if lastPlayed != "" {
					m.LastPlayed = lastPlayed
				}
			}
		}
	case "episode":
		for i := range lib.Episodes {
			if e := &lib.Episodes[i]; e.EpisodeID == id {
				found, countChanged = true, e.PlayCount != st.PlayCount
				e.PlayCount, e.Resume.Position = st.PlayCount, st.Resume

				if lastPlayed != "" {
					e.LastPlayed = lastPlayed
				}
			}
		}
	}

	unlock()
	s.mu.Unlock()

	if !found {
		return false
	}

	s.Notify("Player.OnStop", map[string]any{"item": map[string]any{"type": kind, "id": id}, "end": st.Resume == 0})

	update := map[string]any{"id": id, "type": kind}
	if countChanged {
		update["playcount"] = st.PlayCount
	}

	s.Notify("VideoLibrary.OnUpdate", update)

	return true
}

func (s *Server) databaseGetDatabaseName(params json.RawMessage) (any, *Error) {
	var p struct {
		Type string `json:"type"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	if p.Type != "videos" {
		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	return s.opts.DatabaseName, nil
}
