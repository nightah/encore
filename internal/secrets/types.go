package secrets

import (
	"context"
	"crypto/cipher"
	"log/slog"
	"sync"

	"github.com/nightah/encore/internal/store"
)

// Repo stores sealed rows; store.SecretRows implements it.
type Repo interface {
	Put(ctx context.Context, row store.SecretRow) error
	Get(ctx context.Context, id string) (store.SecretRow, error)
	Delete(ctx context.Context, id string) error
	KeyIDs(ctx context.Context) ([]string, error)
	Rewrite(ctx context.Context, fn func(row *store.SecretRow) error) error
}

// Options configure Open. Key and KeyFile mirror config.Secrets.
type Options struct {
	// Key is a base64-encoded key.
	Key string
	// KeyFile names a file holding a base64-encoded key.
	KeyFile string
	// DataDir holds the generated key file when neither Key nor KeyFile is set.
	DataDir string
	Logger  *slog.Logger
}

// Store encrypts and decrypts secrets.
type Store struct {
	repo Repo
	log  *slog.Logger
	// managedFile is the generated key file Rotate rewrites; empty when the operator supplies the key.
	managedFile string

	// writeMu is held shared by Put and exclusively by Rotate, so no row is sealed under the outgoing key after its rotation starts.
	writeMu sync.RWMutex

	mu      sync.RWMutex
	primary string
	keys    map[string]*keyEntry
}

type keyEntry struct {
	raw  []byte
	aead cipher.AEAD
}
