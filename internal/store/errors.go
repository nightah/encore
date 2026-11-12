package store

import (
	"errors"
)

// Errors returned by repositories. Constraint failures wrap the driver error so the detail is still available.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
	ErrInvalid  = errors.New("invalid")
)

// ErrSchemaVersion is returned by OpenReadOnly for a database at a schema version other than this build's.
var ErrSchemaVersion = errors.New("schema version differs")
