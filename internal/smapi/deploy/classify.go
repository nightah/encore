package deploy

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/lwa"
)

// Transient reports whether a deploy that failed with err may succeed when tried again later without changes: Amazon was unavailable or throttling after the client's own retries, another build was still running (409), the network failed, an import or build took too long, the deploy was interrupted, or locales failed to build without Amazon saying why. Anything else needs a change first: authorising Encore with Amazon again, or a package, model or slot value Amazon rejected for a reason it named.
func Transient(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, lwa.ErrReauthorise), errors.Is(err, lwa.ErrInvalidClient), errors.Is(err, lwa.ErrNotAuthorised):
		return false
	case errors.Is(err, smapi.ErrPollTimeout), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return true
	}

	if de, ok := errors.AsType[*Error](err); ok {
		return de.unexplained()
	}

	switch status := smapi.StatusCode(err); {
	case status == http.StatusConflict, status == http.StatusTooManyRequests, status >= http.StatusInternalServerError:
		return true
	case status != 0:
		return false
	}

	_, network := errors.AsType[net.Error](err)

	return network
}

// unexplained reports whether every failure in e is a locale build Amazon gave no reason for.
func (e *Error) unexplained() bool {
	if len(e.Problems) > 0 || len(e.Locales) == 0 {
		return false
	}

	for _, problems := range e.Locales {
		for _, p := range problems {
			if p.Code != "" || (p.Message != unexplainedBuild && p.Message != unreportedBuild) {
				return false
			}
		}
	}

	return true
}
