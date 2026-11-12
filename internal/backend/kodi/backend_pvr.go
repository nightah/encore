package kodi

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// LiveTVAvailable implements media.Backend.
func (b *Backend) LiveTVAvailable(ctx context.Context) (bool, error) {
	r, err := b.c.PVRGetProperties(ctx, kodirpc.PVRGetPropertiesParams{Properties: []kodirpc.PVRPropertyName{kodirpc.PVRPropertyNameAvailable}})
	if err != nil {
		return false, err
	}

	return r.Available, nil
}

// Channels implements media.Backend.
func (b *Backend) Channels(ctx context.Context, guide bool) ([]media.Channel, error) {
	props := slices.Clone(channelFields)
	if guide {
		props = append(props, kodirpc.PVRFieldsChannelItemBroadcastnow, kodirpc.PVRFieldsChannelItemBroadcastnext)
		b.m.zone.refresh(ctx, b.localNow)
	}

	// Decoded by hand: Kodi sends a broadcast's genre as a list, though its schema says a string.
	r, err := kodirpc.Call[kodirpc.PVRGetChannelsParams, pvrChannels](ctx, b.c.Caller, kodirpc.MethodPVRGetChannels, kodirpc.PVRGetChannelsParams{
		ChannelgroupID: kodirpc.NewPVRChannelGroupIDFromString(kodirpc.PVRChannelGroupIDStringAlltv),
		Properties:     props,
	})
	if err != nil {
		return nil, err
	}

	loc := b.m.zone.location()
	out := make([]media.Channel, 0, len(r.Channels))

	for _, c := range r.Channels {
		if c.Hidden {
			continue
		}

		out = append(out, media.Channel{
			ID: c.ChannelID, Name: strings.TrimSpace(c.Channel), Major: c.ChannelNumber, SubNumber: c.SubChannelNumber,
			Client: c.ClientID, Locked: c.Locked, Now: c.Now.broadcast(loc), Next: c.Next.broadcast(loc),
		})
	}

	slices.SortStableFunc(out, func(x, y media.Channel) int {
		return cmp.Or(cmp.Compare(x.Major, y.Major), cmp.Compare(x.SubNumber, y.SubNumber), cmp.Compare(x.ID, y.ID))
	})

	return out, nil
}

// PlayChannel implements media.Backend.
func (b *Backend) PlayChannel(ctx context.Context, id int) error {
	_, err := b.c.PlayerOpen(ctx, kodirpc.PlayerOpenParams{
		Item: kodirpc.NewPlayerOpenItemFromChannelID(kodirpc.PlayerOpenItemChannelID{ChannelID: kodirpc.LibraryID(id)}),
	})

	return err
}

func (g *flexGenre) UnmarshalJSON(data []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}

		*g = flexGenre(strings.Join(list, " / "))

		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	*g = flexGenre(s)

	return nil
}

// Kodi writes broadcast times in UTC. A broadcast without both times, as for a channel the guide has nothing for, is nil.
func (p *pvrBroadcast) broadcast(loc *time.Location) *media.Broadcast {
	if p == nil || p.Title == "" {
		return nil
	}

	start, err1 := time.ParseInLocation(kodiTimeLayout, p.StartTime, time.UTC)
	end, err2 := time.ParseInLocation(kodiTimeLayout, p.EndTime, time.UTC)

	if err1 != nil || err2 != nil {
		return nil
	}

	return &media.Broadcast{Title: p.Title, Episode: p.EpisodeName, Genre: string(p.Genre), Start: start.In(loc), End: end.In(loc)}
}
