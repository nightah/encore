package auth

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
)

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	good := HashPassword("correct horse battery")

	parts := strings.Split(good, "$")
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v

		return strings.Join(p, "$")
	}

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldRejectEmpty", have: ""},
		{name: "ShouldRejectBcrypt", have: "$2a$10$abcdefghijklmnopqrstuv"},
		{name: "ShouldRejectTooFewParts", have: strings.Join(parts[:5], "$")},
		{name: "ShouldRejectTooManyParts", have: good + "$extra"},
		{name: "ShouldRejectArgon2i", have: with(1, "argon2i")},
		{name: "ShouldRejectAMissingVersion", have: with(2, "x=19")},
		{name: "ShouldRejectAnOldVersion", have: with(2, "v=16")},
		{name: "ShouldRejectGarbledParameters", have: with(3, "m=19456;t=2;p=1")},
		{name: "ShouldRejectZeroPasses", have: with(3, "m=19456,t=0,p=1")},
		{name: "ShouldRejectZeroThreads", have: with(3, "m=19456,t=2,p=0")},
		{name: "ShouldRejectASaltThatIsNotBase64", have: with(4, "!!!")},
		{name: "ShouldRejectAHashThatIsNotBase64", have: with(5, "!!!")},
		{name: "ShouldRejectAnEmptyHash", have: with(5, "")},
		{name: "ShouldRejectAPaddedSalt", have: with(4, parts[4]+"==")},
		{name: "ShouldRejectNegativeThreads", have: with(3, "m=19456,t=2,p=-1")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := VerifyPassword(tc.have, "correct horse battery")
			require.ErrorIs(t, err, errMalformedHash)
			assert.False(t, ok)
		})
	}
}

func TestVerifyPasswordHonoursStoredParameters(t *testing.T) {
	// A hash made with other parameters and key length still verifies, so the defaults can change without locking anyone out.
	salt := []byte("saltsaltsaltsalt")
	key := argon2.IDKey([]byte("hunter2hunter2"), salt, 1, 1024, 2, 16)
	hash := fmt.Sprintf("$argon2id$v=19$m=1024,t=1,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))

	testCases := []struct {
		name     string
		have     string
		expected bool
	}{
		{name: "ShouldVerifyTheRightPassword", have: "hunter2hunter2", expected: true},
		{name: "ShouldRejectAWrongPassword", have: "hunter2hunter3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := VerifyPassword(hash, tc.have)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, ok)
		})
	}
}

func TestValidatePasswordLimits(t *testing.T) {
	testCases := []struct {
		name string
		have string
		ok   bool
	}{
		{name: "ShouldRejectEmpty", have: ""},
		{name: "ShouldRejectNineCharacters", have: "123456789"},
		{name: "ShouldAcceptTenCharacters", have: "1234567890", ok: true},
		{name: "ShouldAcceptTenMultiByteCharacters", have: strings.Repeat("ü", 10), ok: true},
		{name: "ShouldRejectNineMultiByteCharacters", have: strings.Repeat("ü", 9)},
		{name: "ShouldCountSpaces", have: "          ", ok: true},
		{name: "ShouldAccept1024Bytes", have: strings.Repeat("a", 1024), ok: true},
		{name: "ShouldReject1025Bytes", have: strings.Repeat("a", 1025)},
		{name: "ShouldReject600TwoByteCharacters", have: strings.Repeat("ü", 600)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.have)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestDummyHashIsARealHash(t *testing.T) {
	ok, err := VerifyPassword(dummyHash(), "encore-dummy-password")
	require.NoError(t, err)
	assert.True(t, ok, "it is a real hash, so verifying against it costs the same as a real account")
}
