package auth

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses HMAC-SHA1, which is not affected by SHA-1 collisions.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// newTOTPKey makes a random secret for account and its otpauth:// URL. The secret is unpadded base32, as authenticator apps expect.
func newTOTPKey(account string, r io.Reader) (secret, uri string, err error) {
	key := make([]byte, totpSecretSize)
	defer clear(key)

	if _, err := io.ReadFull(r, key); err != nil {
		return "", "", err
	}

	secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)

	u := url.URL{
		Scheme: "otpauth",
		Host:   "totp",
		Path:   "/" + totpIssuer + ":" + account,
		// Sorted keys, as the otpauth:// URLs issued so far had them.
		RawQuery: "algorithm=SHA1&digits=" + strconv.Itoa(totpDigits) + "&issuer=" + url.PathEscape(totpIssuer) + "&period=" + strconv.Itoa(totpPeriod) + "&secret=" + secret,
	}

	return secret, u.String(), nil
}

// totpCode returns the code for the base32 secret at t.
func totpCode(secret string, t time.Time) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	defer clear(key)

	return hotp(key, uint64(t.Unix()/totpPeriod)), nil //nolint:gosec // the clock is after 1970
}

// totpStep reports whether code is valid for secret at now, allowing totpSkew steps of clock skew, and returns the time step it matched so the caller can reject a replay.
func totpStep(secret, code string, now time.Time) (int64, bool) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return 0, false
	}
	defer clear(key)

	given := []byte(strings.TrimSpace(code))
	current := now.Unix() / totpPeriod

	// The current step first, then outwards, so a code that happens to repeat matches the nearest step.
	for i := range 2*totpSkew + 1 {
		step := current + int64((i+1)/2)
		if i%2 == 1 {
			step = current - int64((i+1)/2)
		}

		if subtle.ConstantTimeCompare([]byte(hotp(key, uint64(step))), given) == 1 { //nolint:gosec // the clock is after 1970
			return step, true
		}
	}

	return 0, false
}

// decodeTOTPSecret decodes a base32 secret, accepting lowercase, surrounding whitespace, and missing or partial padding.
func decodeTOTPSecret(secret string) ([]byte, error) {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	if n := len(secret) % 8; n != 0 {
		secret += strings.Repeat("=", 8-n)
	}

	return base32.StdEncoding.DecodeString(secret)
}

// hotp is the RFC 4226 HOTP value of key at counter, as totpDigits decimal digits.
func hotp(key []byte, counter uint64) string {
	mac := hmac.New(sha1.New, key)

	var msg [8]byte

	binary.BigEndian.PutUint64(msg[:], counter)
	mac.Write(msg[:])

	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:]) & 0x7fffffff

	var code [totpDigits]byte

	for i := len(code) - 1; i >= 0; i-- {
		code[i] = '0' + byte(value%10)
		value /= 10
	}

	return string(code[:])
}
