package nlu

import "errors"

// ErrFailed means at least one row failed.
var ErrFailed = errors.New("nlu: some utterances didn't profile as expected")
