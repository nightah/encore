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
	testCases := []struct {
		name     string
		have     int64
		expected string
	}{
		{name: "ShouldMatchT59", have: 59, expected: "287082"},
		{name: "ShouldMatchT1111111109", have: 1111111109, expected: "081804"},
		{name: "ShouldMatchT1111111111", have: 1111111111, expected: "050471"},
		{name: "ShouldMatchT1234567890", have: 1234567890, expected: "005924"},
		{name: "ShouldMatchT2000000000", have: 2000000000, expected: "279037"},
		{name: "ShouldMatchT20000000000", have: 20000000000, expected: "353130"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := totpCode(rfcSecret, time.Unix(tc.have, 0))
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestTOTPCodeRFC4226(t *testing.T) {
	// RFC 4226 Appendix D: HOTP values for counters 0 to 9, reached through TOTP at counter*30 seconds.
	testCases := []struct {
		name     string
		have     int64
		expected string
	}{
		{name: "ShouldMatchCounter0", have: 0, expected: "755224"},
		{name: "ShouldMatchCounter1", have: 1, expected: "287082"},
		{name: "ShouldMatchCounter2", have: 2, expected: "359152"},
		{name: "ShouldMatchCounter3", have: 3, expected: "969429"},
		{name: "ShouldMatchCounter4", have: 4, expected: "338314"},
		{name: "ShouldMatchCounter5", have: 5, expected: "254676"},
		{name: "ShouldMatchCounter6", have: 6, expected: "287922"},
		{name: "ShouldMatchCounter7", have: 7, expected: "162583"},
		{name: "ShouldMatchCounter8", have: 8, expected: "399871"},
		{name: "ShouldMatchCounter9", have: 9, expected: "520489"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := totpCode(rfcSecret, time.Unix(tc.have*30, 0))
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestTOTPSecretEncodings(t *testing.T) {
	at := time.Unix(1111111111, 0)

	// An 11-byte key encodes to 18 base32 characters, so its padded form differs.
	const odd, oddPadded, oddCode = "MFRGGZDFMZTWQ2LKNM", "MFRGGZDFMZTWQ2LKNM======", "714188"

	testCases := []struct {
		name     string
		have     string
		expected string
		err      bool
	}{
		{name: "ShouldDecodeTheRFCSecret", have: rfcSecret, expected: "050471"},
		{name: "ShouldDecodeLowercase", have: strings.ToLower(rfcSecret), expected: "050471"},
		{name: "ShouldTrimSurroundingWhitespace", have: " " + rfcSecret + "\n", expected: "050471"},
		{name: "ShouldDecodeAnOddLengthKeyUnpadded", have: odd, expected: oddCode},
		{name: "ShouldDecodeAnOddLengthKeyPadded", have: oddPadded, expected: oddCode},
		{name: "ShouldDecodeAnOddLengthKeyLowercase", have: strings.ToLower(odd), expected: oddCode},
		{name: "ShouldDecodeAnOddLengthKeyPaddedLowercase", have: strings.ToLower(oddPadded), expected: oddCode},
		{name: "ShouldCompletePartialPaddingNotRejectIt", have: "MFRGGZDFMZTWQ2LKNM=", expected: oddCode},
		{name: "ShouldRejectANonBase32Secret", have: "not base32!", err: true},
		{name: "ShouldRejectAnInvalidBase32Digit", have: "GEZDGNBVGY3TQOJ1", err: true},
		{name: "ShouldRejectAnImpossibleLength", have: "MFRGGZDFMZTWQ2LKN", err: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err {
				_, err := totpCode(tc.have, at)
				require.Error(t, err)

				_, ok := totpStep(tc.have, "000000", at)
				assert.False(t, ok)

				return
			}

			actual, err := totpCode(tc.have, at)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)

			step, ok := totpStep(tc.have, tc.expected, at)
			assert.True(t, ok)
			assert.Equal(t, int64(1111111111/30), step)
		})
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
	testCases := []struct {
		name string
		have time.Time
	}{
		{name: "ShouldUseTheWindowAtTheFirstSecondOfTheStep", have: time.Unix(step*30, 0)},
		{name: "ShouldUseTheWindowAtTheLastSecondOfTheStep", have: time.Unix(step*30+29, 0)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			offsets := []struct {
				name   string
				offset int64
				ok     bool
			}{
				{name: "ShouldRejectTwoStepsBehind", offset: -2},
				{name: "ShouldAcceptOneStepBehind", offset: -1, ok: true},
				{name: "ShouldAcceptTheCurrentStep", offset: 0, ok: true},
				{name: "ShouldAcceptOneStepAhead", offset: 1, ok: true},
				{name: "ShouldRejectTwoStepsAhead", offset: 2},
			}

			for _, o := range offsets {
				t.Run(o.name, func(t *testing.T) {
					actual, ok := totpStep(rfcSecret, codeAt(step+o.offset), tc.have)
					assert.Equal(t, o.ok, ok)

					if o.ok {
						assert.Equal(t, step+o.offset, actual)
					}
				})
			}
		})
	}
}

func TestTOTPStepCodeInput(t *testing.T) {
	now := time.Unix(1111111111, 0)

	testCases := []struct {
		name string
		have string
		ok   bool
	}{
		{name: "ShouldAcceptTheCode", have: "050471", ok: true},
		{name: "ShouldTrimSurroundingWhitespace", have: " 050471\n", ok: true},
		{name: "ShouldRejectFiveDigits", have: "50471"},
		{name: "ShouldRejectSevenDigits", have: "0050471"},
		{name: "ShouldRejectAWrongCode", have: "050472"},
		{name: "ShouldRejectEmpty", have: ""},
		{name: "ShouldRejectALetter", have: "05047a"},
		{name: "ShouldRejectAnInnerSpace", have: "050 471"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := totpStep(rfcSecret, tc.have, now)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

func TestNewTOTPKey(t *testing.T) {
	seed := []byte("0123456789abcdefghij")

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldBuildAURLForAPlainAccount", have: "admin", expected: "otpauth://totp/Encore:admin?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK"},
		{name: "ShouldEscapeASpace", have: "kodi admin", expected: "otpauth://totp/Encore:kodi%20admin?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK"},
		{name: "ShouldEscapeNonASCII", have: "ädmin@example.com", expected: "otpauth://totp/Encore:%C3%A4dmin@example.com?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK"},
		{name: "ShouldEscapeURLDelimiters", have: "a/b?c#d&e=f:g", expected: "otpauth://totp/Encore:a/b%3Fc%23d&e=f:g?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret, url, err := newTOTPKey(tc.have, bytes.NewReader(seed))
			require.NoError(t, err)
			assert.Equal(t, "GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK", secret)
			assert.Equal(t, tc.expected, url)
		})
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
