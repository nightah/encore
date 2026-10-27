package secrets

import (
	"errors"

	"github.com/nightah/encore/internal/store"
)

// Errors.
var (
	// ErrNotFound means no secret has the requested ID.
	ErrNotFound = store.ErrNotFound
	// ErrUnknownKey means a row was sealed with a key that is not loaded.
	ErrUnknownKey = errors.New("secret was encrypted with a key that is not loaded")
	// ErrDecrypt means the ciphertext failed authentication: the wrong key, tampering, or a row moved to another ID.
	ErrDecrypt = errors.New("secret failed to decrypt")
	// ErrNoKey means no key is configured and no generated key file exists.
	ErrNoKey = errors.New("no secrets key is configured and no generated key file exists")
)
