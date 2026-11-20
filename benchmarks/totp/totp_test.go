package totp

import (
	"bytes"
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/auth"
)

// opts are Encore's TOTP parameters in pquerna/otp's terms: SHA1, six digits, 30-second steps and one step of skew.
var opts = totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

// rfcSecret is the SHA1 key of RFC 6238 Appendix B, "12345678901234567890", in base32.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

// TestRFC6238 checks both implementations against the SHA1 rows of RFC 6238 Appendix B, truncated to six digits.
func TestRFC6238(t *testing.T) {
	testCases := []struct {
		name     string
		have     int64
		expected string
	}{
		{name: "ShouldMatchAtT59", have: 59, expected: "287082"},
		{name: "ShouldMatchAtT1111111109", have: 1111111109, expected: "081804"},
		{name: "ShouldMatchAtT1111111111", have: 1111111111, expected: "050471"},
		{name: "ShouldMatchAtT1234567890", have: 1234567890, expected: "005924"},
		{name: "ShouldMatchAtT2000000000", have: 2000000000, expected: "279037"},
		{name: "ShouldMatchAtT20000000000", have: 20000000000, expected: "353130"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(tc.have, 0)

			actual, err := auth.TOTPCode(rfcSecret, at)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual, "in-tree")

			lib, err := totp.GenerateCodeCustom(rfcSecret, at, opts)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, lib, "pquerna/otp")
		})
	}
}

func genSecret(t *rapid.T) string {
	key := rapid.SliceOfN(rapid.Byte(), 20, 20).Draw(t, "key")
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)

	// Secrets are stored as enrolled, but the in-tree decoder also accepts lowercase and surrounding space, as pquerna/otp does.
	if rapid.Bool().Draw(t, "lower") {
		s = strings.ToLower(s)
	}

	return s
}

func genTime(t *rapid.T) time.Time {
	return time.Unix(rapid.Int64Range(0, 1<<40).Draw(t, "unix"), 0)
}

// TestParityCodes checks that both generate the same code for random secrets and times.
func TestParityCodes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secret, at := genSecret(t), genTime(t)

		got, err := auth.TOTPCode(secret, at)
		require.NoError(t, err)

		want, err := totp.GenerateCodeCustom(secret, at, opts)
		require.NoError(t, err)

		if got != want {
			t.Fatalf("TOTPCode(%q, %d) = %q, pquerna/otp %q", secret, at.Unix(), got, want)
		}
	})
}

// TestParityValidation checks that both accept and reject the same codes: the code from a few steps either side of now, a random code, and codes with surrounding space.
func TestParityValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secret, now := genSecret(t), genTime(t)

		code, err := totp.GenerateCodeCustom(secret, now.Add(time.Duration(rapid.IntRange(-90, 90).Draw(t, "offset"))*time.Second), opts)
		require.NoError(t, err)

		switch rapid.IntRange(0, 2).Draw(t, "variant") {
		case 1:
			code = rapid.StringMatching(`[0-9]{6}`).Draw(t, "random")
		case 2:
			code = " " + code + "\n"
		}

		_, got := auth.TOTPStep(secret, code, now)
		want, err := totp.ValidateCustom(strings.TrimSpace(code), secret, now, opts)

		if got != (want && err == nil) {
			t.Fatalf("TOTPStep(%q, %q, %d) = %v, pquerna/otp %v (%v)", secret, code, now.Unix(), got, want, err)
		}
	})
}

// TestParityEnrolment checks that the same random bytes give the same secret and otpauth:// URL.
func TestParityEnrolment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		seed := rapid.SliceOfN(rapid.Byte(), 20, 20).Draw(t, "seed")
		account := rapid.StringMatching(`[a-zA-Z0-9 @._-]{1,24}`).Draw(t, "account")

		secret, uri, err := auth.NewTOTPKey(account, bytes.NewReader(seed))
		require.NoError(t, err)

		key, err := totp.Generate(totp.GenerateOpts{Issuer: "Encore", AccountName: account, Rand: bytes.NewReader(seed)})
		require.NoError(t, err)

		if secret != key.Secret() || uri != key.URL() {
			t.Fatalf("NewTOTPKey(%q) = %q, %q; pquerna/otp %q, %q", account, secret, uri, key.Secret(), key.URL())
		}
	})
}

var (
	sinkString string
	sinkBool   bool
)

func BenchmarkCode(b *testing.B) {
	at := time.Unix(1111111111, 0)

	b.Run("impl=pquerna", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			sinkString, _ = totp.GenerateCodeCustom(rfcSecret, at, opts)
		}
	})

	b.Run("impl=intree", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			sinkString, _ = auth.TOTPCode(rfcSecret, at)
		}
	})
}

// BenchmarkValidate checks a code from the previous step, the worst case for both: the in-tree check tries the current step first, then the ones either side.
func BenchmarkValidate(b *testing.B) {
	now := time.Unix(1111111111, 0)

	code, err := totp.GenerateCodeCustom(rfcSecret, now.Add(-30*time.Second), opts)
	require.NoError(b, err)

	b.Run("impl=pquerna", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			sinkBool, _ = totp.ValidateCustom(code, rfcSecret, now, opts)
		}
	})

	b.Run("impl=intree", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, sinkBool = auth.TOTPStep(rfcSecret, code, now)
		}
	})
}
