package livetv

import "errors"

// ErrNoChannels means the media centre has live TV but lists no channels.
var ErrNoChannels = errors.New("livetv: no channels")
