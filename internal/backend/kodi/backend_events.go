package kodi

import (
	"context"
	"encoding/json"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// Subscribe implements media.Backend. It keeps the WebSocket up for as long as ctx lives and translates Kodi's notifications into events.
func (b *Backend) Subscribe(ctx context.Context, fn func(media.Event)) error {
	stop := b.c.Listen(Listener{
		Notification: func(n Notification) {
			if ev, ok := translate(n); ok {
				fn(ev)
			}
		},
		State: func(up bool) {
			if up {
				fn(media.Event{Kind: media.EventConnected})
			} else {
				fn(media.Event{Kind: media.EventDisconnected})
			}
		},
	})
	defer stop()

	return b.c.Run(ctx)
}

func (l *libraryItem) ref() (media.Ref, bool) {
	id, typ := l.ID, l.Type
	if l.Item != nil {
		id, typ = l.Item.ID, l.Item.Type
	}

	kind, ok := kindOf(typ)

	return media.Ref{Kind: kind, ID: id}, ok && id > 0
}

// ok is false for notifications Encore doesn't act on.
func translate(n Notification) (media.Event, bool) {
	switch n.Method {
	case kodirpc.NotificationVideoLibraryOnUpdate, kodirpc.NotificationAudioLibraryOnUpdate:
		return libraryEvent(n.Params, media.EventItemUpdated)
	case kodirpc.NotificationVideoLibraryOnRemove, kodirpc.NotificationAudioLibraryOnRemove:
		return libraryEvent(n.Params, media.EventItemRemoved)
	case kodirpc.NotificationVideoLibraryOnScanFinished, kodirpc.NotificationAudioLibraryOnScanFinished,
		kodirpc.NotificationVideoLibraryOnCleanFinished, kodirpc.NotificationAudioLibraryOnCleanFinished:
		return media.Event{Kind: media.EventLibraryScanned}, true
	case kodirpc.NotificationPlayerOnPlay:
		return playerEvent(n.Params, media.EventPlaybackStarted), true
	case kodirpc.NotificationPlayerOnStop:
		return playerEvent(n.Params, media.EventPlaybackStopped), true
	case kodirpc.NotificationSystemOnWake:
		return media.Event{Kind: media.EventConnected}, true
	case kodirpc.NotificationSystemOnQuit, kodirpc.NotificationSystemOnRestart, kodirpc.NotificationSystemOnSleep:
		return media.Event{Kind: media.EventDisconnected}, true
	}

	return media.Event{}, false
}

func libraryEvent(params json.RawMessage, kind media.EventKind) (media.Event, bool) {
	var p struct {
		Data libraryItem `json:"data"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return media.Event{}, false
	}

	ref, ok := p.Data.ref()
	if !ok {
		return media.Event{}, false
	}

	return media.Event{Kind: kind, Ref: ref}, true
}

// Ref is set only for library items.
func playerEvent(params json.RawMessage, kind media.EventKind) media.Event {
	var p struct {
		Data struct {
			Item struct {
				ID   int    `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
		} `json:"data"`
	}

	ev := media.Event{Kind: kind}

	if json.Unmarshal(params, &p) == nil {
		if k, ok := kindOf(p.Data.Item.Type); ok && p.Data.Item.ID > 0 {
			ev.Ref = media.Ref{Kind: k, ID: p.Data.Item.ID}
		}
	}

	return ev
}
