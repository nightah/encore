package kodi

import (
	"errors"
	"fmt"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

var (
	// ErrUnsupported means the connected Kodi's API version doesn't provide the method.
	ErrUnsupported = errors.New("kodi: method not supported by this Kodi version")
	// ErrUnauthorized means Kodi rejected the configured credentials. It is always wrapped together with media.ErrUnreachable.
	ErrUnauthorized = errors.New("kodi: unauthorized")
	// ErrNotImage means an image reference isn't one of Kodi's image:// URLs.
	ErrNotImage = errors.New("kodi: not an image:// reference")
	// ErrHTTPPort means the Kodi URL names a port outside 1 to 65535.
	ErrHTTPPort = errors.New("the Kodi URL's port must be between 1 and 65535")
	// ErrWebSocketPort means the Kodi WebSocket URL names a port outside 1 to 65535.
	ErrWebSocketPort = errors.New("the Kodi WebSocket URL's port must be between 1 and 65535")
)

var (
	errUnsupportedKind = errors.New("unsupported kind")
	errMixedQueue      = errors.New("kodi: a queue can't mix songs and video")
	errFavouriteType   = errors.New("kodi: this kind of favourite can't be opened remotely")
	errNoHost          = errors.New("no host to connect to")
	// The request never left the process, so retrying it over HTTP is safe.
	errNotSent = errors.New("request not sent")

	errNoMulticastInterface = errors.New("kodi: no multicast interface is up")
	errMDNSJoin             = errors.New("kodi: could not join the mDNS group on any interface")
	errMDNSSend             = errors.New("kodi: could not send an mDNS query on any interface")
)

// RPCError is a JSON-RPC error returned by Kodi.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Method  string `json:"-"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("kodi: %s: error %d: %s", e.Method, e.Code, e.Message)
}

// Is maps Kodi error codes onto the media package's errors.
func (e *RPCError) Is(target error) bool {
	switch target {
	case media.ErrNotFound:
		return e.Code == CodeNotFound || e.Code == CodeInvalidParams && itemLookups[e.Method]
	case media.ErrUnavailable:
		return e.Code == CodeUnavailable
	}

	return false
}

// itemLookups are methods whose only invalid parameter, as Encore calls them, is an item ID. Kodi before API 13.1000 reports a missing item as InvalidParams.
var itemLookups = map[string]bool{
	kodirpc.MethodPlayerOpen:                       true,
	kodirpc.MethodVideoLibraryGetMovieDetails:      true,
	kodirpc.MethodVideoLibraryGetTVShowDetails:     true,
	kodirpc.MethodVideoLibraryGetEpisodeDetails:    true,
	kodirpc.MethodVideoLibraryGetMusicVideoDetails: true,
	kodirpc.MethodAudioLibraryGetArtistDetails:     true,
	kodirpc.MethodAudioLibraryGetAlbumDetails:      true,
	kodirpc.MethodAudioLibraryGetSongDetails:       true,
}

func unreachable(method string, err error) error {
	return fmt.Errorf("kodi: %s: %w: %w", method, media.ErrUnreachable, err)
}
