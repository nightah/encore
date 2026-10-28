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
	good, err := HashPassword("correct horse battery")
	require.NoError(t, err)

	parts := strings.Split(good, "$")
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v

		return strings.Join(p, "$")
	}

	for name, hash := range map[string]string{
		"empty":            "",
		"bcrypt":           "$2a$10$abcdefghijklmnopqrstuv",
		"too few parts":    strings.Join(parts[:5], "$"),
		"too many parts":   good + "$extra",
		"argon2i":          with(1, "argon2i"),
		"version missing":  with(2, "x=19"),
		"old version":      with(2, "v=16"),
		"params garbled":   with(3, "m=19456;t=2;p=1"),
		"zero passes":      with(3, "m=19456,t=0,p=1"),
		"zero threads":     with(3, "m=19456,t=2,p=0"),
		"salt not base64":  with(4, "!!!"),
		"hash not base64":  with(5, "!!!"),
		"hash empty":       with(5, ""),
		"padded salt":      with(4, parts[4]+"=="),
		"negative threads": with(3, "m=19456,t=2,p=-1"),
	} {
		t.Run(name, func(t *testing.T) {
			ok, err := VerifyPassword(hash, "correct horse battery")
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

	ok, err := VerifyPassword(hash, "hunter2hunter2")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = VerifyPassword(hash, "hunter2hunter3")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestValidatePasswordLimits(t *testing.T) {
	for name, tt := range map[string]struct {
		password string
		ok       bool
	}{
		"empty":                      {"", false},
		"nine characters":            {"123456789", false},
		"ten characters":             {"1234567890", true},
		"ten multi-byte characters":  {strings.Repeat("ü", 10), true},
		"nine multi-byte characters": {strings.Repeat("ü", 9), false},
		"spaces count":               {"          ", true},
		"1024 bytes":                 {strings.Repeat("a", 1024), true},
		"1025 bytes":                 {strings.Repeat("a", 1025), false},
		"600 two-byte characters":    {strings.Repeat("ü", 600), false},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if tt.ok {
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
