package livetv

import (
	"sync"
	"time"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// Guide finds one media centre instance's channels and what's on them. It is safe for concurrent use.
type Guide struct {
	src  media.LiveTV
	lang string
	now  func() time.Time

	// load serialises fetches, so requests arriving together share one.
	load sync.Mutex

	mu       sync.Mutex
	channels []media.Channel
	index    *match.Index
	loaded   time.Time

	guide      []media.Channel
	guideUntil time.Time

	available   bool
	availableAt time.Time
	availableOK bool
}

// Options configure a Guide.
type Options struct {
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Found is what a channel request found.
type Found struct {
	// Decision is DecisionPlay when Channel is the channel meant, DecisionAsk when Choices should be offered, else DecisionNotFound.
	Decision match.Decision
	Channel  media.Channel
	// Choices are the channels to ask about, best first, one per name.
	Choices []media.Channel
	// Result is the name search behind the decision, for the request log; nil when a number decided it.
	Result *match.Result
}

// Programme is a programme found in the guide and the channel it is on.
type Programme struct {
	Channel   media.Channel
	Broadcast media.Broadcast
	// OnNow is false for a programme on next.
	OnNow bool
}
