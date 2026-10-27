package library

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/media"
)

// Searcher is implemented by backends that can filter by title server side, e.g. kodi.Backend. Lookup falls back to listing the whole kind otherwise.
type Searcher interface {
	Search(ctx context.Context, kind media.Kind, title string) ([]media.Item, error)
}

// ChangeFunc observes catalog changes: items added or changed, and items removed. Episodes are a lazily filled side cache and are not reported.
type ChangeFunc func(instance string, upserted []media.Item, removed []media.Ref)

// Options configure a Catalog.
type Options struct {
	// Kinds to load; DefaultKinds when empty. SetKinds changes them later.
	Kinds []media.Kind
	// RefreshInterval is how often Run applies a safety-net delta; negative disables it.
	RefreshInterval time.Duration
	// Debounce coalesces bursts of events, such as the per-item updates of a library scan.
	Debounce time.Duration
	// MinResync skips a reconnect-triggered full sync when one finished this recently.
	MinResync time.Duration
	// Logger is labelled by the caller, which knows the instance and backend; the catalog adds nothing.
	Logger *slog.Logger
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// Stats summarise a catalog.
type Stats struct {
	Counts map[media.Kind]int
	// Episodes counts cached episodes across all shows.
	Episodes  int
	LastSync  time.Time
	LastDelta time.Time
	LastEvent time.Time
}

// Catalog is the library of one backend instance. It is safe for concurrent use; reads take a read lock and return shared, immutable slices.
type Catalog struct {
	instance string
	backend  media.Source
	opts     Options
	log      *slog.Logger

	// write serialises mutations so change hooks see them in order.
	write sync.Mutex

	mu sync.RWMutex
	// kinds are the kinds loaded now; needFull are those added since the last listing of them, which the next delta lists in full rather than by date added.
	kinds     []media.Kind
	needFull  map[media.Kind]bool
	items     map[media.Ref]media.Item
	byKind    map[media.Kind][]media.Item
	episodes  map[int][]media.Item
	epIndex   map[int]media.Item
	epGen     uint64
	watermark map[media.Kind]time.Time
	lastSync  time.Time
	lastDelta time.Time
	lastEvent time.Time

	hooksMu  sync.Mutex
	hooks    map[int]ChangeFunc
	nextHook int
}

// Snapshot is a catalog's state in a plain, serialisable form, for persisting between restarts.
type Snapshot struct {
	Instance   string                   `json:"instance"`
	Items      []media.Item             `json:"items"`
	Episodes   map[int][]media.Item     `json:"episodes,omitempty"`
	Watermarks map[media.Kind]time.Time `json:"watermarks,omitempty"`
	LastSync   time.Time                `json:"lastSync"`
}

// pending accumulates events between debounced passes.
type pending struct {
	mu      sync.Mutex
	full    bool
	delta   bool
	refresh map[media.Ref]bool
	remove  map[media.Ref]bool
	signal  chan struct{}
}

type errorList []error
