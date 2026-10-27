package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// HashPassword returns an argon2id hash of password in the PHC string format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches hash, in constant time for a given hash.
func VerifyPassword(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errMalformedHash
	}

	var (
		memory, passes uint32
		threads        uint8
	)

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil || passes == 0 || threads == 0 {
		return false, errMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errMalformedHash
	}

	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errMalformedHash
	}

	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want))) //nolint:gosec // len(want) is a decoded hash, far below 2^32

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// ValidatePassword checks a new password against the length limits.
func ValidatePassword(password string) error {
	n := utf8.RuneCountInString(password)

	switch {
	case n < MinPasswordLength:
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	case len(password) > MaxPasswordLength:
		return fmt.Errorf("password must be at most %d bytes", MaxPasswordLength)
	}

	return nil
}

// dummyHash is verified against when the username is unknown, so a failed sign-in takes as long either way.
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword("encore-dummy-password")
	if err != nil {
		panic(err) // unreachable: crypto/rand does not fail
	}

	return h
})
