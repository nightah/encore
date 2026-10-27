package statestore

import (
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"time"

	"github.com/nightah/encore/internal/blobstore"
)

// Database is the part of *store.DB the saver needs.
type Database interface {
	Backup(ctx context.Context, w io.Writer) error
}

// Saver writes the database to the storage backend when it changes.
type Saver struct {
	DB       Database
	Store    blobstore.Store
	Log      *slog.Logger
	Interval time.Duration
	// Owner identifies this process in the owner marker, e.g. the hostname.
	Owner string
	Now   func() time.Time

	last [sha256.Size]byte
}

type owner struct {
	Owner     string    `json:"owner"`
	Heartbeat time.Time `json:"heartbeat"`
}
