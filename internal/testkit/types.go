package testkit

import (
	"sync"
	"testing"
	"time"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// Backend is an in-memory media.Backend that records calls. Set Err to make every call fail.
type Backend struct {
	mu      sync.Mutex
	items   []media.Item
	playing *media.NowPlaying
	volume  media.VolumeState
	calls   []string

	streams    media.Streams
	repeat     media.Repeat
	favourites []media.Favourite
	addons     []media.Addon
	clock      time.Time

	Err error
}

// Matcher is a predictable stand-in for the real matcher: a case-insensitive exact title match plays, several exact matches ask, a substring match asks for confirmation, and anything else is not found.
type Matcher struct {
	Docs []match.Doc
}

// Library is an actions.Library fake that reveals extra items on lookup, simulating titles added to Kodi after the index was built.
type Library struct {
	Matcher *Matcher
	Hidden  []media.Item
	// Cached is what Items serves.
	Cached []media.Item
}

// Op is the kind of statement a Fault fails.
type Op string

// Fault makes statements on one table of a database fail.
type Fault struct {
	Op    Op
	Table string
	// Nth is the first statement (for Read) or row (for writes) to fail, counting from 1; every one after it fails too. Zero means the first.
	Nth int
	// When narrows a write fault to the rows that meet this SQL condition on NEW or OLD, e.g. "NEW.name = 'Bedroom'". It cannot be set for Read.
	When string
}

// Faults injects failures into the SQLite database at one path, for testing what an operation leaves behind when the database fails part way through. Writes fail through triggers added to the schema, so they fail as SQLite would, undoing their statement and failing their transaction; reads fail through an authorizer on every connection to the path.
type Faults struct {
	t    testing.TB
	path string

	mu       sync.Mutex
	reads    []*readFault
	triggers []string
}

type readFault struct {
	table string
	nth   int
	seen  int
}
