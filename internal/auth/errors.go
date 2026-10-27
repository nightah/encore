package auth

import (
	"errors"
	"time"
)

// Errors returned by the sign-in flows. Messages are safe to show to the user.
var (
	ErrBadCredentials = errors.New("the username or password is incorrect")
	ErrTOTPRequired   = errors.New("a one-time code from your authenticator app is required")
	ErrBadTOTP        = errors.New("the one-time code is incorrect")
	ErrAdminExists    = errors.New("an admin account already exists")
	ErrBadSetupToken  = errors.New("the setup token is incorrect; it is printed in the server log at startup")
	ErrLocalDisabled  = errors.New("password sign-in is disabled while another sign-in mode is active; set ENCORE_BREAK_GLASS=true to allow it")
	ErrNotAdmin       = errors.New("you are signed in but not a member of the admin group")
	ErrOIDCDisabled   = errors.New("OpenID Connect sign-in is not configured")
)

// InputError is an invalid value in a sign-in or setup request.
type InputError struct {
	Field string
	Err   error
}

// LockedError means too many failed sign-ins; try again after RetryAfter.
type LockedError struct {
	RetryAfter time.Duration
}

var errMalformedHash = errors.New("auth: malformed password hash")
