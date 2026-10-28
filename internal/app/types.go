package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/library"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/store"
)

// App is the running service state shared by the HTTP handlers.
type App struct {
	cfg     *config.Config
	log     *slog.Logger
	db      *store.DB
	secrets *secrets.Store
	// blobs holds library snapshots; nil when the store couldn't be opened.
	blobs blobstore.Store
	tune  tuning

	// mu guards instances, groups, each group's record and evidence, each instance's group pointers, and prints.
	mu        sync.RWMutex
	instances map[int64]*Instance
	groups    map[int64]*group
	prints    map[int64]*kodi.LibraryFingerprint

	// planMu serialises changes to library groups and their runtimes; changes also take mu. runCtx is Run's context while it runs.
	planMu sync.Mutex
	runCtx context.Context

	recorder *recorder

	onPlayer func(instanceID int64)

	// effective returns the configuration merged with the web interface's settings; see SetSettings.
	effective func(context.Context) (*config.Config, error)
}

// Instance is the runtime for one Kodi host. Catalog and Index belong to the instance's library group and are shared with the other members; they change when the group does, under App.mu.
type Instance struct {
	Record  store.KodiInstance
	Client  *kodi.Client
	Backend *kodi.Backend
	Catalog *library.Catalog
	Index   *match.Index
	Waker   *kodi.Waker

	// id and name are copies of Record's.
	id   int64
	name string

	group *group
	log   *slog.Logger
	db    *store.DB

	cancel context.CancelFunc
	done   chan struct{}
}

// LibraryCounts are the sizes of an instance's library.
type LibraryCounts struct {
	Movies   int
	Shows    int
	Episodes int
	Songs    int
}

// InstanceStatus is a point-in-time view of one Kodi instance for the admin API.
type InstanceStatus struct {
	ID   int64
	Name string
	// Connected means the WebSocket is up, so live library updates arrive.
	Connected bool
	// Reachable means Kodi answered a JSON-RPC ping, over either transport.
	Reachable bool
	// Unauthorized means Kodi's web server refused the configured credentials. Kodi may still answer over the WebSocket, which it doesn't authenticate, but images and the HTTP fallback fail.
	Unauthorized bool
	// CanWake means a MAC address is known for Wake-on-LAN.
	CanWake     bool
	KodiVersion string
	APIVersion  string
	LastEventAt time.Time
	LastSyncAt  time.Time
	NextSyncAt  time.Time
	Counts      LibraryCounts
	// Newest is the most recently added movie or episode, if any.
	Newest     *media.Item
	NowPlaying *media.NowPlaying
}

// Check is one step of a connection test.
type Check struct {
	Name     string
	OK       bool
	Duration time.Duration
	Detail   string
}

// group is the runtime of one library group: the catalog and title index its members share, and the backend the catalog syncs from.
type group struct {
	// id and shared are fixed for the runtime; rec and ev may change under App.mu.
	id      int64
	shared  bool
	rec     store.LibraryGroup
	ev      evidence
	members []*Instance
	source  *Instance
	catalog *library.Catalog
	index   *match.Index
	backend *groupBackend
	log     *slog.Logger

	unlisten func()
	aliasMu  sync.RWMutex
	aliases  map[media.Ref][]string
	// seeded means the catalog was carried over from the runtime this one replaced, so no snapshot is restored.
	seeded bool

	cancel context.CancelFunc
	done   chan struct{}
}

type groupUpdate struct {
	rec store.LibraryGroup
	ev  evidence
}

// evidence is a group's evidence JSON: why its members are, or aren't, grouped.
type evidence struct {
	Origin      string               `json:"origin,omitempty"`
	Fingerprint *fingerprintEvidence `json:"fingerprint,omitempty"`
	// Confirmed lists members seen mirroring a playback change.
	Confirmed    []int64 `json:"confirmed,omitempty"`
	Confirmation string  `json:"confirmation,omitempty"`
	// Reason explains the last split.
	Reason string `json:"reason,omitempty"`
	// Vetoed lists instances detection must not group with this group's members, because they were seen not to share its library.
	Vetoed []int64 `json:"vetoed,omitempty"`
	// None marks the group of an instance with library_group: none in the config file.
	None bool `json:"none,omitempty"`
}

type fingerprintEvidence struct {
	Matched      int       `json:"matched"`
	Compared     int       `json:"compared"`
	DatabaseName string    `json:"databaseName,omitempty"`
	At           time.Time `json:"at"`
}

// tuning holds the timings of library groups and Kodi connections, which tests shorten or run on a fake clock, and the client tests route Kodi traffic through.
type tuning struct {
	fingerprintSample int
	minSample         int
	minAgreement      float64
	// fingerprintRetry is how often an unreachable instance is sampled again.
	fingerprintRetry time.Duration
	// confirmDebounce lets a burst of playback notifications settle before members are compared.
	confirmDebounce time.Duration
	// confirmGrace is how long a member gets to show a change before it's split off.
	confirmGrace     time.Duration
	failoverInterval time.Duration
	pingTimeout      time.Duration
	// snapshotEvery limits how often a library snapshot is written; snapshotCheck is how often the need is checked.
	snapshotEvery time.Duration
	snapshotCheck time.Duration
	// learnMACDelay gives a new connection time to settle before the MAC address is read for Wake-on-LAN.
	learnMACDelay time.Duration
	catalog       library.Options
	// kodiClient, when set, carries every request to Kodi; tests route it to fakes in memory.
	kodiClient *http.Client
}

// groupBackend is the backend a library group's catalog syncs from. Library reads go to the active member: the group's source while it's reachable, otherwise another reachable member, so a sleeping source isn't woken just to sync. Library events from every member reach the catalog, since with a shared database only the Kodi instance that made a change reports it.
type groupBackend struct {
	// Backend is the source's, answering any method the catalog doesn't use.
	media.Backend

	members []*Instance
	source  *Instance
	active  atomic.Pointer[Instance]
	tune    tuning
	log     *slog.Logger
	nudge   chan struct{}

	mu   sync.Mutex
	emit func(media.Event)
}

type observation struct {
	ref media.Ref
	// playCount is set when Kodi announced a new play count, which proves the item changed.
	playCount *int
	// prior is the item as the catalog knew it before the change.
	prior     media.Item
	havePrior bool
	due       time.Time
}

type verdict int

type fpResult struct {
	Matched, Compared int
	// DatabaseNames are set when both instances name their database (Kodi 22+); differing names rule sharing out.
	DatabaseNames [2]string
}

// GroupStatus describes a library group for the web interface.
type GroupStatus struct {
	ID    int64                   `json:"id"`
	Name  string                  `json:"name"`
	State store.LibraryGroupState `json:"state"`
	// Manual means the user or the config file decided the group; ConfigManaged means the config file did, so it can't be changed at runtime.
	Manual        bool          `json:"manual"`
	ConfigManaged bool          `json:"configManaged"`
	Members       []GroupMember `json:"members"`
	// SourceID is the member chosen to run library syncs; ActiveID is the one running them now, which differs while the source is unreachable.
	SourceID int64 `json:"sourceId"`
	ActiveID int64 `json:"activeId"`
	// Evidence summarises why the members are grouped, e.g. "200 of 200 sampled items match".
	Evidence string `json:"evidence,omitempty"`
	// Reason explains the last split.
	Reason    string             `json:"reason,omitempty"`
	LastSync  time.Time          `json:"lastSync,omitzero"`
	LastDelta time.Time          `json:"lastDelta,omitzero"`
	Counts    map[media.Kind]int `json:"counts"`
	// Episodes counts episodes cached so far, not the library's total.
	Episodes  int       `json:"episodes"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GroupMember is one Kodi instance in a library group.
type GroupMember struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Fingerprinted means the instance's library has been sampled since start-up.
	Fingerprinted bool `json:"fingerprinted"`
	// Confirmed means the instance was seen mirroring a playback change made on another member.
	Confirmed bool `json:"confirmed"`
}

// Library is one distinct library: a library group's catalog and the Kodi instances that share it.
type Library struct {
	GroupID int64
	Name    string
	// Members are the instances' names, ordered by ID.
	Members []string
	Catalog *library.Catalog
}

type recorder struct {
	repo    store.RequestLog
	log     *slog.Logger
	entries chan alexa.LogEntry
}

type loggedCall struct {
	Method string `json:"method"`
	MS     int64  `json:"ms"`
	OK     bool   `json:"ok"`
}

type loggedTiming struct {
	VerifyMS int64 `json:"verify_ms"`
	MatchMS  int64 `json:"match_ms"`
	KodiMS   int64 `json:"kodi_ms"`
}

type loggedMatch struct {
	Target     string            `json:"target,omitempty"`
	Normalized string            `json:"normalized,omitempty"`
	Decision   string            `json:"decision,omitempty"`
	Margin     float64           `json:"margin,omitempty"`
	Candidates []loggedCandidate `json:"candidates,omitempty"`
	Error      string            `json:"error,omitempty"`
}

type loggedCandidate struct {
	Title   string          `json:"title"`
	Year    int             `json:"year,omitempty"`
	Kind    string          `json:"kind,omitempty"`
	MediaID int             `json:"media_id,omitempty"`
	Score   float64         `json:"score"`
	Why     match.Breakdown `json:"breakdown"`
}

// Call is a backend command a dry run would have sent.
type Call struct {
	Method string
	Params map[string]any
}

// DryRun records the commands a replayed request would have sent to Kodi.
type DryRun struct {
	mu    sync.Mutex
	calls []Call
}

// DryRunTargets resolve devices like Target, without recording the device as seen, and hand out targets whose commands are recorded instead of sent. Reads (library, now playing) still reach Kodi.
type DryRunTargets struct {
	App *App
	Run *DryRun
}

// dryBackend passes reads through and records writes.
type dryBackend struct {
	media.Backend

	run *DryRun
}

// slotBuilder collects one slot type's values, merging titles that read the same across libraries.
type slotBuilder struct {
	byTitle map[string]*slotCandidate
	ids     map[string]string
}

type slotCandidate struct {
	value  skillmodel.SlotValue
	recent time.Time
}
