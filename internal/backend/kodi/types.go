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

	// HTTPClient overrides the client used for HTTP calls.
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

// Transport is a kodirpc.Caller that prefers one long-lived WebSocket and falls back to HTTP POST while the socket is down. Notifications only arrive over the WebSocket, which is kept up while at least one Run is active.
type Transport struct {
	cfg  Config
	log  *slog.Logger
	http *http.Client
	ids  atomic.Int64

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

// Browser browses one mDNS service type, sending answers to entries until timeout or ctx ends. It must not close entries.
type Browser interface {
	Browse(ctx context.Context, service string, timeout time.Duration, entries chan<- ServiceEntry) error
}

// ServiceEntry is one mDNS answer.
type ServiceEntry struct {
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
	// Interface restricts browsing to one interface; nil means the system default.
	Interface *net.Interface
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
