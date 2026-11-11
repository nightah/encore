package kodi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/miekg/dns"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// Config locates a Kodi instance and tunes the connection to it.
type Config struct {
	// Scheme is "http" (default) or "https"; the WebSocket uses "ws" or "wss" to match.
	Scheme string
	Host   string
	// Port is the HTTP JSON-RPC port, WebSocketPort the raw JSON-RPC WebSocket port.
	Port          int
	WebSocketPort int
	// Path is the JSON-RPC path on both ports, "/jsonrpc" by default.
	Path string

	Username string
	Password string

	// Timeout bounds each call that has no earlier context deadline.
	Timeout time.Duration
	// HeartbeatInterval is how often an idle WebSocket is probed with JSONRPC.Ping.
	HeartbeatInterval time.Duration
	// ReconnectMin and ReconnectMax bound the WebSocket reconnect backoff.
	ReconnectMin time.Duration
	ReconnectMax time.Duration
	// DisableWebSocket sends every call over HTTP and disables notifications.
	DisableWebSocket bool

	// HTTPClient overrides the client used for HTTP calls and the WebSocket handshake.
	HTTPClient *http.Client
}

// Client is a typed Kodi JSON-RPC client that knows the server's API version and refuses methods the server doesn't have.
type Client struct {
	kodirpc.Client

	t   *Transport
	log *slog.Logger

	mu      sync.Mutex
	version kodirpc.APIVersion
	known   bool
}

// Refuses methods the server's API version lacks before they reach the transport.
type gate struct{ c *Client }

// HTTPAuth is what Kodi's web server last made of the configured credentials. Only HTTP requests are authenticated; the WebSocket never is.
type HTTPAuth int32

// Transport is a kodirpc.Caller that prefers one long-lived WebSocket and falls back to HTTP POST while the socket is down. Notifications only arrive over the WebSocket, which is kept up while at least one Run is active.
type Transport struct {
	cfg  Config
	log  *slog.Logger
	http *http.Client
	ids  atomic.Int64
	auth atomic.Int32

	mu        sync.Mutex
	ws        *wsConn
	listeners map[int]Listener
	nextLis   int
	runs      int
	stop      context.CancelFunc
	done      chan struct{}
}

// Listener receives transport events. Either func may be nil. They run on the connection's reader goroutine and must not block.
type Listener struct {
	Notification func(Notification)
	// State reports the WebSocket connecting (true) or dropping (false).
	State func(connected bool)
}

// Notification is a JSON-RPC notification pushed by Kodi over the WebSocket.
type Notification struct {
	Method kodirpc.NotificationName
	// Params is the raw params member, {"sender": ..., "data": ...}; decode it with kodirpc.NewNotificationPayload.
	Params json.RawMessage
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// A response or a notification.
type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

type wsConn struct {
	t *Transport
	c *websocket.Conn

	mu      sync.Mutex
	pending map[int64]chan *message
	err     error
	done    chan struct{}
}

type wsCaller struct{ w *wsConn }

// BackendOptions tune a Backend.
type BackendOptions struct {
	// Name is the configured instance name, reported by Info when Kodi has no friendly name.
	Name string
	// PageSize limits each library request; zero means DefaultPageSize.
	PageSize int
	// Location is the Kodi host's time zone, used to read dateadded and lastplayed. When nil, the UTC offset is learned from Kodi's clock.
	Location *time.Location
	Logger   *slog.Logger
}

// Backend implements media.Backend for one Kodi instance.
type Backend struct {
	c        *Client
	name     string
	pageSize int
	m        mapper
	log      *slog.Logger

	// Tests replace it for a repeatable order.
	shuffle func(n int, swap func(i, j int))

	mu        sync.Mutex
	playlists map[int]playlistFile
}

type playlistFile struct {
	path  string
	media kodirpc.FilesMedia
}

type query struct {
	// Substring match.
	title string
	// showID and unwatched apply to episodes.
	showID    int
	unwatched bool
	sort      *kodirpc.ListSort
}

// Returns one page's mapped items and the total Kodi reports.
type fetchPage func(ctx context.Context, b *Backend, q query, limits *kodirpc.ListLimits) ([]media.Item, int, error)

// The data of VideoLibrary and AudioLibrary OnUpdate/OnRemove. Kodi before API 6 nested it under "item"; both shapes are accepted.
type libraryItem struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Item *struct {
		ID   int    `json:"id"`
		Type string `json:"type"`
	} `json:"item"`
}

type mapper struct {
	zone *zone
}

// The Kodi host's time zone, which dateadded and lastplayed are written in. Kodi doesn't report it, so unless one is configured it is learned from the host's clock: Kodi's local time of day against the current UTC time.
type zone struct {
	mu      sync.Mutex
	loc     *time.Location
	fixed   bool
	learned time.Time
	now     func() time.Time
}

// Discoverer finds Kodi instances.
type Discoverer struct {
	// Browser defaults to multicast DNS on every interface.
	Browser Browser
	Timeout time.Duration
}

// Browser browses for instances of the given mDNS service types until timeout or ctx ends.
type Browser interface {
	Browse(ctx context.Context, services []string, timeout time.Duration) ([]ServiceEntry, error)
}

// ServiceEntry is one service instance found by browsing.
type ServiceEntry struct {
	// Service is the service type, e.g. ServiceJSONRPC.
	Service string
	// Instance is the service instance name, e.g. "Kodi (lounge)".
	Instance string
	Host     string
	Addrs    []net.IP
	Port     int
}

// Instance is a Kodi found on the network.
type Instance struct {
	Name  string
	Host  string
	Addrs []net.IP
	// Port is the HTTP JSON-RPC port and WebSocketPort the TCP/WebSocket one; either is zero when not announced.
	Port          int
	WebSocketPort int
}

// MDNSBrowser browses with multicast DNS.
type MDNSBrowser struct {
	// Interface restricts browsing to one interface; nil means every multicast interface that is up.
	Interface *net.Interface
}

// mdnsConn is one address family's mDNS socket.
type mdnsConn struct {
	conn  net.PacketConn
	group *net.UDPAddr
	// setInterface picks the interface the next multicast write leaves on.
	setInterface func(*net.Interface) error
}

// mdnsAnswers collects records from every answer received during a browse, so services resolve whatever order the records arrive in or however they are split across packets.
type mdnsAnswers struct {
	// instances maps a lower-cased service type to the instance names announced for it.
	instances map[string]map[string]bool
	// srv and addrs are keyed by lower-cased instance and host names.
	srv   map[string]*dns.SRV
	addrs map[string][]net.IP
}

// LibraryFingerprint is a cheap sample of a Kodi instance's video library. Kodi instances reading one shared database (MySQL/MariaDB) report the same IDs, file paths and dates added; separate databases built from the same files almost never do.
type LibraryFingerprint struct {
	// DatabaseName is the video database's name from Database.GetDatabaseName, e.g. "MyVideos131". Empty when Kodi doesn't support the method (before Kodi 22).
	DatabaseName string            `json:"databaseName,omitempty"`
	Movies       []FingerprintItem `json:"movies,omitempty"`
	Episodes     []FingerprintItem `json:"episodes,omitempty"`
	// MovieTotal and EpisodeTotal are the library's sizes, which may exceed the sample.
	MovieTotal   int `json:"movieTotal"`
	EpisodeTotal int `json:"episodeTotal"`
}

// FingerprintItem is one sampled library entry: what a shared database makes identical across Kodi instances.
type FingerprintItem struct {
	ID        int    `json:"id"`
	File      string `json:"file"`
	DateAdded string `json:"dateadded"`
}

// Waker wakes a sleeping Kodi host with Wake-on-LAN.
type Waker struct {
	Client *Client
	// Broadcast is where packets go, "host" or "host:port"; DefaultBroadcast when empty.
	Broadcast string
	// Send delivers packets; SendUDP when nil.
	Send SendFunc
	// PollInterval is how often WakeAndWait pings; ResendInterval how often it repeats the packet.
	PollInterval   time.Duration
	ResendInterval time.Duration

	mu  sync.Mutex
	mac net.HardwareAddr
}

// SendFunc delivers one UDP datagram to addr ("host:port").
type SendFunc func(ctx context.Context, addr string, payload []byte) error

// skinProfile is how to open one skin's own search with a query filled in.
type skinProfile struct {
	// ID is the skin's add-on ID, as lookandfeel.skin reports it, and Name its display name.
	ID, Name string
	Kind     skinSearchKind
	// Requires lists the add-ons that must be installed and enabled for run to work.
	Requires []string
	// Window is the window run opens, as Window.IsVisible takes it; empty for skinSearchNative.
	Window string
	// run opens the search; nil for skinSearchNative.
	run func(ctx context.Context, b *Backend, q string) error
}

// skinSearchKind is how a skin profile fills in the query.
type skinSearchKind string

// SkinProfile describes a skin whose search Encore knows, for the web interface and the docs.
type SkinProfile struct {
	// ID is the skin's add-on ID and Name its display name.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Native is true for a skin that draws Global Search's window itself, where a search runs Global Search.
	Native bool `json:"native"`
	// Requires lists the add-ons the skin's search needs, Window the window it opens and How how it fills in the query.
	Requires []string `json:"requires"`
	Window   string   `json:"window,omitempty"`
	How      string   `json:"how"`
}

// titleFilter is how one library section is narrowed to the titles containing some text: the window that shows it, its root path, and the smart playlist type and field that filter it.
type titleFilter struct {
	window      kodirpc.GUIWindow
	root        string
	kind, field string
}

// smartPlaylist is the subset of a Kodi smart playlist (xsp) that a filtered library path carries as JSON.
type smartPlaylist struct {
	Rules struct {
		And []smartPlaylistRule `json:"and"`
	} `json:"rules"`
	Type string `json:"type"`
}

type smartPlaylistRule struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Value    []string `json:"value"`
}

type pvrChannels struct {
	Channels []pvrChannel `json:"channels"`
}

type pvrChannel struct {
	ChannelID        int           `json:"channelid"`
	Channel          string        `json:"channel"`
	ChannelNumber    int           `json:"channelnumber"`
	SubChannelNumber int           `json:"subchannelnumber"`
	ClientID         int           `json:"clientid"`
	Hidden           bool          `json:"hidden"`
	Locked           bool          `json:"locked"`
	Now              *pvrBroadcast `json:"broadcastnow"`
	Next             *pvrBroadcast `json:"broadcastnext"`
}

type pvrBroadcast struct {
	Title       string    `json:"title"`
	EpisodeName string    `json:"episodename"`
	Genre       flexGenre `json:"genre"`
	StartTime   string    `json:"starttime"`
	EndTime     string    `json:"endtime"`
}

// A genre Kodi sends as a list or a string; a list is joined with " / ", as Kodi shows it.
type flexGenre string
