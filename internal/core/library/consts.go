package library

import "time"

// Defaults for Options fields left zero.
const (
	DefaultRefreshInterval = 15 * time.Minute
	DefaultDebounce        = 500 * time.Millisecond
	DefaultMinResync       = 30 * time.Second
)

// resubscribeDelay is how long Run waits before subscribing again when a backend's Subscribe returns early.
const resubscribeDelay = 5 * time.Second

// Delta tuning.
const (
	// deltaSlack re-reads a little before the watermark, since dateadded has one-second resolution and a scan may still be adding items in that second.
	deltaSlack = time.Minute
	// episodeLookback seeds the episode watermark after a full sync, which doesn't list episodes.
	episodeLookback = 24 * time.Hour
)
