package api

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"
)

var errNotOverridable = errors.New("this setting cannot be changed in the web interface")

type managedKeyError struct{ key string }

type invalidSettingsError struct{ err error }

// errNoLWACredentials is returned until the Login with Amazon client ID and secret are saved.
var errNoLWACredentials = huma.Error409Conflict("save the Login with Amazon client ID and secret first")
