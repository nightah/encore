package statestore

import (
	"time"
)

// Object keys in the storage backend.
const (
	KeyDatabase = "state/encore.db.gz"
	KeyPrevious = "state/encore.db.prev.gz"
	KeyOwner    = "state/owner.json"
)

// Defaults for Saver.
const (
	DefaultInterval = time.Minute
	// ownerStale is how old another owner's heartbeat must be before it's considered gone.
	ownerStale = 3 * DefaultInterval
)
