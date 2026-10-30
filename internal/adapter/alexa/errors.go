package alexa

import "errors"

// ErrUnlinked means the Echo isn't linked to a Kodi instance and there's no default.
var ErrUnlinked = errors.New("alexa: device is not linked to a Kodi instance")

// ErrUnknownRoom means a request named a room Encore doesn't know.
var ErrUnknownRoom = errors.New("alexa: unknown room")

// ErrTooSlow means a request was answered at RequestDeadline because handling it took too long.
var ErrTooSlow = errors.New("alexa: request not handled within the deadline")

// ErrPanicked means handling a request panicked; the request is answered with an error instead.
var ErrPanicked = errors.New("alexa: handling the request panicked")
