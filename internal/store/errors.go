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
