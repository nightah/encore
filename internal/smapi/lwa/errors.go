package lwa

import "errors"

var (
	// ErrReauthorise means the refresh token was rejected (revoked, expired or issued to another client) and the user has to authorise again.
	ErrReauthorise = errors.New("lwa: authorisation is no longer valid, authorise Encore with Amazon again")
	// ErrInvalidClient means LWA rejected the client ID or secret.
	ErrInvalidClient = errors.New("lwa: client ID or secret rejected")
	// ErrNotAuthorised means no token has been stored yet.
	ErrNotAuthorised = errors.New("lwa: Encore has not been authorised with Amazon yet")
)
