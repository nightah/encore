package blobstore

import "errors"

// ErrNotFound means no object has the requested key.
var ErrNotFound = errors.New("object not found")

// ErrInvalidKey means a key is empty, absolute or escapes the store root.
var ErrInvalidKey = errors.New("invalid object key")
