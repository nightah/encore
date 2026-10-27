package app

import (
	"errors"
)

// ErrNoRuntime means no Kodi instance with that ID is loaded.
var ErrNoRuntime = errors.New("no runtime for this Kodi instance")

// ErrManagedByConfig means a Kodi instance's library group is set in the config file and can't be changed at runtime.
var ErrManagedByConfig = errors.New("the library group is set in the config file")
