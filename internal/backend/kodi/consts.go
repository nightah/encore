package kodi

import (
	"net"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

// Defaults for Config fields left zero.
const (
	DefaultHTTPPort          = 8080
	DefaultWebSocketPort     = 9090
	DefaultPath              = "/jsonrpc"
	DefaultTimeout           = 10 * time.Second
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultReconnectMin      = 500 * time.Millisecond
	DefaultReconnectMax      = 30 * time.Second
)

// JSON-RPC error codes Kodi returns. NotFound and Unavailable are new in Kodi 22 (API 13.1000); older versions report both as InvalidParams.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeNotFound       = -32098
	CodeUnavailable    = -32097
)

// DefaultPageSize is how many items one library request asks for.
const DefaultPageSize = 1000

// DefaultFingerprintSample is how many movies and how many episodes Fingerprint reads by default.
const DefaultFingerprintSample = 200

// Services Kodi announces over mDNS: the raw TCP JSON-RPC port, which also accepts WebSocket connections, and the HTTP JSON-RPC port.
const (
	ServiceJSONRPC     = "_xbmc-jsonrpc._tcp"
	ServiceJSONRPCHTTP = "_xbmc-jsonrpc-h._tcp"
)

// DefaultDiscoveryTimeout is how long Discover listens for answers.
const DefaultDiscoveryTimeout = 3 * time.Second

// mDNS's port and groups (RFC 6762 §3).
const mdnsPort = 5353

var (
	mdnsGroupV4 = net.IPv4(224, 0, 0, 251)
	mdnsGroupV6 = net.ParseIP("ff02::fb")
)

// mdnsDomain is the domain every mDNS name ends in.
const mdnsDomain = "local."

// Wake-on-LAN defaults.
const (
	DefaultBroadcast      = "255.255.255.255:9"
	DefaultPollInterval   = time.Second
	DefaultResendInterval = 5 * time.Second
)

// Library pages of a thousand items with a dozen fields stay well below this per-message cap.
const wsReadLimit = 64 << 20

// The "xx" meridiem makes the hour unambiguous whatever 12 or 24 hour clock the user chose in Kodi.
const clockLabel = "System.Time(hh:mm:ss xx)"

const dateLabel = "System.Date(yyyy-mm-dd)"

// Short enough to follow daylight saving changes.
const zoneRelearn = 6 * time.Hour

// Kodi writes dateadded and lastplayed in the host's local time.
const kodiTimeLayout = "2006-01-02 15:04:05"

// Kodi's image references are an encoded path or URL.
const maxImageRef = 2048

const (
	audioPlaylist kodirpc.PlaylistID = 0
	videoPlaylist kodirpc.PlaylistID = 1
)

// Per Playlist.Add call, so queueing a long list never sends one huge request.
const queuePage = 100

const (
	windowVideos         kodirpc.GUIWindow = "videos"
	windowMusic          kodirpc.GUIWindow = "music"
	windowFavourites     kodirpc.GUIWindow = "favouritesbrowser"
	windowSubtitleSearch kodirpc.GUIWindow = "subtitlesearch"
)
