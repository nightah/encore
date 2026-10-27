package media

import "errors"

// Errors returned by Backend implementations, wrapped with detail.
var (
	// ErrNotFound means the referenced item does not exist.
	ErrNotFound = errors.New("media: not found")
	// ErrUnavailable means the item exists but its content can't be reached, e.g. a NAS is offline.
	ErrUnavailable = errors.New("media: unavailable")
	// ErrUnreachable means the backend itself could not be contacted.
	ErrUnreachable = errors.New("media: backend unreachable")
	// ErrNothingPlaying means a playback command needs an active player and there is none.
	ErrNothingPlaying = errors.New("media: nothing playing")
)
