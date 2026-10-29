package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rfcSecret is the SHA1 key from RFC 4226 Appendix D and RFC 6238 Appendix B, "12345678901234567890", in base32.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPCodeRFC6238(t *testing.T) {
	// RFC 6238 Appendix B, SHA1 rows, truncated to the six digits Encore uses.
	for _, tc := range []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		code, err := totpCode(rfcSecret, time.Unix(tc.unix, 0))
		require.NoError(t, err)
		assert.Equal(t, tc.code, code, "T=%d", tc.unix)
	}
}

func TestTOTPCodeRFC4226(t *testing.T) {
	// RFC 4226 Appendix D: HOTP values for counters 0 to 9, reached through TOTP at counter*30 seconds.
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}

	for counter, code := range want {
		got, err := totpCode(rfcSecret, time.Unix(int64(counter)*30, 0))
		require.NoError(t, err)
		assert.Equal(t, code, got, "counter %d", counter)
	}
}

func TestTOTPSecretEncodings(t *testing.T) {
	at := time.Unix(1111111111, 0)

	// An 11-byte key encodes to 18 base32 characters, so its padded form differs.
	const odd, oddPadded, oddCode = "MFRGGZDFMZTWQ2LKNM", "MFRGGZDFMZTWQ2LKNM======", "714188"

	for _, tc := range []struct {
		secret, code string
	}{
		{rfcSecret, "050471"},
		{strings.ToLower(rfcSecret), "050471"},
		{" " + rfcSecret + "\n", "050471"},
		{odd, oddCode},
		{oddPadded, oddCode},
		{strings.ToLower(odd), oddCode},
		{strings.ToLower(oddPadded), oddCode},
		{"MFRGGZDFMZTWQ2LKNM=", oddCode}, // partial padding is completed, not rejected
	} {
		code, err := totpCode(tc.secret, at)
		require.NoError(t, err, tc.secret)
		assert.Equal(t, tc.code, code, tc.secret)

		step, ok := totpStep(tc.secret, tc.code, at)
		assert.True(t, ok, tc.secret)
		assert.Equal(t, int64(1111111111/30), step, tc.secret)
	}

	for _, secret := range []string{"not base32!", "GEZDGNBVGY3TQOJ1", "MFRGGZDFMZTWQ2LKN"} {
		_, err := totpCode(secret, at)
		require.Error(t, err, secret)

		_, ok := totpStep(secret, "000000", at)
		assert.False(t, ok, secret)
	}
}

func TestTOTPStepSkewWindow(t *testing.T) {
	codeAt := func(step int64) string {
		code, err := totpCode(rfcSecret, time.Unix(step*30, 0))
		require.NoError(t, err)

		return code
	}

	const step = int64(1111111111 / 30)

	// Both edges of the current step: its first and last second.
	for _, now := range []time.Time{time.Unix(step*30, 0), time.Unix(step*30+29, 0)} {
		for _, tc := range []struct {
			offset int64
			ok     bool
		}{{-2, false}, {-1, true}, {0, true}, {1, true}, {2, false}} {
			got, ok := totpStep(rfcSecret, codeAt(step+tc.offset), now)
			assert.Equal(t, tc.ok, ok, "now %v offset %d", now.Unix(), tc.offset)

			if tc.ok {
				assert.Equal(t, step+tc.offset, got, "now %v offset %d", now.Unix(), tc.offset)
			}
		}
	}
}

func TestTOTPStepCodeInput(t *testing.T) {
	now := time.Unix(1111111111, 0)

	for _, tc := range []struct {
		code string
		ok   bool
	}{
		{"050471", true},
		{" 050471\n", true},
		{"50471", false},
		{"0050471", false},
		{"050472", false},
		{"", false},
		{"05047a", false},
		{"050 471", false},
	} {
		_, ok := totpStep(rfcSecret, tc.code, now)
		assert.Equal(t, tc.ok, ok, "%q", tc.code)
	}
}

func TestNewTOTPKey(t *testing.T) {
	seed := []byte("0123456789abcdefghij")

	for _, tc := range []struct {
		account, url string
	}{
		{"admin", "otpauth://totp/Encore:admin?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK"},
		{"kodi admin", "otpauth://totp/Encore:kodi%20admin?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK"},
		{"ädmin@example.com", "otpauth://totp/Encore:%C3%A4dmin@example.com?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK"},
		{"a/b?c#d&e=f:g", "otpauth://totp/Encore:a/b%3Fc%23d&e=f:g?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK"},
	} {
		secret, url, err := newTOTPKey(tc.account, bytes.NewReader(seed))
		require.NoError(t, err)
		assert.Equal(t, "GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK", secret)
		assert.Equal(t, tc.url, url, tc.account)
	}

	// Each key draws 20 fresh bytes from the reader.
	_, _, err := newTOTPKey("admin", bytes.NewReader(seed[:19]))
	require.Error(t, err)
}

func BenchmarkTOTPStep(b *testing.B) {
	now := time.Unix(1111111111, 0)

	for b.Loop() {
		if _, ok := totpStep(rfcSecret, "050471", now); !ok {
			b.Fatal("code rejected")
		}
	}
}
