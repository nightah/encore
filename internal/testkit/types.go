package testkit

import (
	"sync"
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
