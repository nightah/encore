package auth

import (
	"io"
	"time"
)

// This file is a seam for the benchmarks module (benchmarks/ at the repository root), which checks the in-tree TOTP against pquerna/otp and times both. Encore itself never calls anything here; each function wraps the one sign-in uses.

// NewTOTPKey makes a random secret for account and its otpauth:// URL, as enrolment does.
func NewTOTPKey(account string, r io.Reader) (secret, uri string, err error) {
	return newTOTPKey(account, r)
}

// TOTPCode returns the code for the base32 secret at t.
func TOTPCode(secret string, t time.Time) (string, error) { return totpCode(secret, t) }

// TOTPStep reports whether code is valid for secret at now, allowing one step of clock skew, and returns the time step it matched.
func TOTPStep(secret, code string, now time.Time) (int64, bool) { return totpStep(secret, code, now) }
