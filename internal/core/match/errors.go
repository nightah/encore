package match

import "errors"

// ErrClosed is returned by an Index after Close.
var ErrClosed = errors.New("match: index closed")
