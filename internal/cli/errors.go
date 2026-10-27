package cli

import (
	"errors"
)

var errLocked = errors.New("locked by another process")

// errSilent signals a failure that has already been reported to the user.
var errSilent = errors.New("silent failure")
