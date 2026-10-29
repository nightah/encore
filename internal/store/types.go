package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// Options configure Open.
type Options struct {
	// Path is the database file. Use DefaultPath for the standard location.
	Path   string
	Logger *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// ReadConns caps the read pool; zero means max(4, GOMAXPROCS).
	ReadConns int
	// RequestLog bounds the request log; zero fields take the defaults.
	RequestLog RequestLogRetention
}

// DB is an open database.
type DB struct {
	path  string
	write *sql.DB
	read  *sql.DB
	log   *slog.Logger
	now   func() time.Time

	retention RequestLogRetention
	subs      subscribers
}

type scanner interface {
	Scan(dest ...any) error
}

// trailing scans one column after those its caller asks for.
type trailing struct {
	scanner

	last any
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type subscribers struct {
	mu   sync.RWMutex
	next int
	fns  map[int]func(RequestLogEntry)
}

// TitleAlias is a user-defined spoken name for a library item, e.g. "the bad guy film" for one specific movie.
type TitleAlias struct {
	ID             int64
	KodiInstanceID int64
	Media          media.Ref
	Alias          string
	Normalised     string
	CreatedAt      time.Time
}

// Aliases is the title alias repository used by the matcher.
type Aliases struct{ db *DB }

// EchoDevice is an Alexa device that has talked to the skill.
type EchoDevice struct {
	ID            int64
	AlexaDeviceID string
	DisplayName   string
	// RoomID and KodiInstanceID are 0 when unassigned.
	RoomID         int64
	KodiInstanceID int64
	FirstSeen      time.Time
	LastSeen       time.Time
	RequestCount   int64
}

// EchoDevices is the echo_devices repository.
type EchoDevices struct{ db *DB }

// FileInfo describes a database file that is not open, such as a backup about to be restored.
type FileInfo struct {
	// SchemaVersion is the newest migration applied to the file, or zero if it was never migrated.
	SchemaVersion int64
	// Secrets are its encrypted secret rows, for checking the configured key can open them.
	Secrets []SecretRow
}

// KodiInstance is a configured Kodi host.
type KodiInstance struct {
	ID       int64
	Name     string
	Scheme   string
	Host     string
	Port     int
	Path     string
	Username string
	// PasswordSecretRef is the internal/secrets ID holding the password; empty when there is none.
	PasswordSecretRef string
	// WSPort is the JSON-RPC WebSocket port for notifications; 0 disables them.
	WSPort int
	// MAC and BroadcastAddr enable Wake-on-LAN when MAC is set.
	MAC           string
	BroadcastAddr string
	// IsDefaultForUnlinked makes this instance answer Echo devices that are not linked to one. At most one instance has it.
	IsDefaultForUnlinked bool
	// Language selects title-matching rules ("en", "de", ...); empty means English.
	Language string
	// Timezone is the Kodi host's IANA time zone; empty means learn the offset from Kodi's clock.
	Timezone string
	// ShutdownQuits overrides alexa.shutdown_quits for this instance; nil follows it.
	ShutdownQuits *bool
	// LibraryGroupID is the library group the instance belongs to; 0 until one is assigned. Update leaves it alone; change it with LibraryGroups().Save or Assign.
	LibraryGroupID int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// KodiInstances is the kodi_instances repository.
type KodiInstances struct{ db *DB }

// LibraryGroupState is how Encore came to believe a group's members share one library database.
type LibraryGroupState string

// LibraryGroup is a set of Kodi instances that share one library database. Members point at their group through KodiInstance.LibraryGroupID.
type LibraryGroup struct {
	ID   int64
	Name string
	// SourceInstanceID is the member that runs library syncs; 0 when none is set.
	SourceInstanceID int64
	State            LibraryGroupState
	// Manual marks a user or config choice, which detection never overrides.
	Manual bool
	// Evidence is free-form JSON describing what detection found; nil when there is none.
	Evidence  json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LibraryGroups is the library_groups repository.
type LibraryGroups struct{ db *DB }

// RequestLogRetention bounds the request log. Whichever limit is hit first wins.
type RequestLogRetention struct {
	MaxEntries int
	MaxAge     time.Duration
}

// RequestLogEntry is one handled Alexa request, kept for the diagnostics UI.
type RequestLogEntry struct {
	ID        int64
	Time      time.Time
	RequestID string
	DeviceID  string
	Locale    string
	Intent    string
	// Slots, Match, KodiCalls and Timing are free-form JSON; nil is stored as NULL.
	Slots     json.RawMessage
	Match     json.RawMessage
	KodiCalls json.RawMessage
	Timing    json.RawMessage
	Speech    string
	Outcome   string
	Duration  time.Duration
	// Simulated marks a request from Amazon's simulator during Encore's own deploy smoke test.
	Simulated bool
}

// RequestLog is the request_log ring buffer.
type RequestLog struct{ db *DB }

// RequestLogQuery pages through the log, newest first.
type RequestLogQuery struct {
	// Limit caps the result; zero means 100.
	Limit int
	// BeforeID returns entries older than this ID, for paging; zero starts from the newest.
	BeforeID int64
}

// Room is a named place that Echo devices and spoken targets ("in the lounge") resolve to.
type Room struct {
	ID   int64
	Name string
	// KodiInstanceID is the instance that plays in this room; 0 for none.
	KodiInstanceID int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RoomAlias is an extra spoken name for a room.
type RoomAlias struct {
	ID         int64
	RoomID     int64
	Alias      string
	Normalised string
	CreatedAt  time.Time
	Kind       SpokenKind
}

// SpokenKind says whether a spoken name is another name for a room ("lounge room") or a device in it ("TV").
type SpokenKind string

// Rooms is the rooms and room_aliases repository. Room names and aliases share one namespace under Normalise, so a spoken name resolves to at most one room.
type Rooms struct{ db *DB }

// SecretRow is one encrypted secret. Encryption is internal/secrets' job; this layer only stores bytes.
type SecretRow struct {
	ID         string
	KeyID      string
	Nonce      []byte
	Ciphertext []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SecretRows is the secrets table repository.
type SecretRows struct{ db *DB }

// Session is an admin web session. Only a hash of the session token is stored, so a database leak does not leak live sessions.
type Session struct {
	IDHash     []byte
	Username   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// Sessions is the admin session repository.
type Sessions struct{ db *DB }

// Settings is the key/value settings repository. Values are stored as JSON.
type Settings struct{ db *DB }
