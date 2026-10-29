package auth

import (
	"io"
	"time"
)

// NewTOTPKey makes a random secret for account and its otpauth:// URL, as enrolment does. It wraps newTOTPKey for the benchmarks module, which checks it against pquerna/otp; Encore calls newTOTPKey itself.
func NewTOTPKey(account string, r io.Reader) (secret, uri string, err error) {
	return newTOTPKey(account, r)
}

// TOTPCode returns the code for the base32 secret at t. It wraps totpCode for the benchmarks module.
func TOTPCode(secret string, t time.Time) (string, error) { return totpCode(secret, t) }

// TOTPStep reports whether code is valid for secret at now, allowing one step of clock skew, and returns the time step it matched. It wraps totpStep, which sign-in uses, for the benchmarks module.
func TOTPStep(secret, code string, now time.Time) (int64, bool) { return totpStep(secret, code, now) }
