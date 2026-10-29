package match

import "errors"

// ErrClosed is returned by an Index after Close.
var ErrClosed = errors.New("match: index closed")

// errIndexFull is returned when an inverted index has no slot left for a Doc and no tombstone to reclaim.
var errIndexFull = errors.New("match: index full")
