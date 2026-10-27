package alexa

import "errors"

// ErrUnlinked means the Echo isn't linked to a Kodi instance and there's no default.
var ErrUnlinked = errors.New("alexa: device is not linked to a Kodi instance")

// ErrUnknownRoom means a request named a room Encore doesn't know.
var ErrUnknownRoom = errors.New("alexa: unknown room")
