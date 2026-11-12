package fakekodi

import (
	"encoding/json"
	"slices"
)

func (s *Server) registerPVR() {
	s.handlers["PVR.GetProperties"] = s.pvrGetProperties
	s.handlers["PVR.GetChannels"] = s.pvrGetChannels
}

func (s *Server) pvrGetProperties(json.RawMessage) (any, *Error) {
	return map[string]bool{"available": s.opts.PVR != nil, "recording": false, "scanning": false}, nil
}

func (s *Server) pvrGetChannels(params json.RawMessage) (any, *Error) {
	var p struct {
		ChannelGroupID json.RawMessage `json:"channelgroupid"`
		Properties     []string        `json:"properties"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}

	if s.opts.PVR == nil {
		return nil, &Error{Code: CodeFailedToExecute, Message: "Failed to execute method."}
	}

	var group string
	if json.Unmarshal(p.ChannelGroupID, &group) != nil || group != "alltv" {
		return nil, &Error{Code: CodeInvalidParams, Message: "Invalid params."}
	}

	out := []map[string]any{}

	for _, c := range s.opts.PVR.Channels {
		if c.Hidden {
			continue
		}

		out = append(out, c.fields(p.Properties))
	}

	return map[string]any{"channels": out, "limits": map[string]int{"start": 0, "end": len(out), "total": len(out)}}, nil
}

// fields is the channel as PVR.GetChannels reports it with props requested.
func (c *Channel) fields(props []string) map[string]any {
	all := map[string]any{
		"channel": c.Channel, "channelnumber": c.ChannelNumber, "subchannelnumber": c.SubChannelNumber, "clientid": c.ClientID,
		"hidden": c.Hidden, "locked": c.Locked, "channeltype": "tv",
	}
	if c.Now != nil {
		all["broadcastnow"] = c.Now.fields()
	}

	if c.Next != nil {
		all["broadcastnext"] = c.Next.fields()
	}

	out := map[string]any{"channelid": c.ChannelID, "label": c.Channel}

	for _, p := range props {
		if v, ok := all[p]; ok {
			out[p] = v
		}
	}

	return out
}

func (b *Broadcast) fields() map[string]any {
	return map[string]any{
		"broadcastid": b.BroadcastID, "label": b.Title, "title": b.Title, "episodename": b.EpisodeName, "genre": b.Genre,
		"starttime": b.StartTime, "endtime": b.EndTime,
	}
}

func (s *Server) channel(id int) (Channel, bool) {
	if s.opts.PVR == nil {
		return Channel{}, false
	}

	i := slices.IndexFunc(s.opts.PVR.Channels, func(c Channel) bool { return c.ChannelID == id })
	if i < 0 {
		return Channel{}, false
	}

	return s.opts.PVR.Channels[i], true
}

// channelItem is a channel as Player.GetItem reports it: the channel's name as the label and the programme on now as the title.
func channelItem(c *Channel) Item {
	it := Item{Type: "channel", ID: c.ChannelID, Label: c.Channel}
	if c.Now != nil {
		it.Title = c.Now.Title
	}

	return it
}
