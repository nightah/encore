package secrets

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenerateKey returns a new random key.
func GenerateKey() []byte {
	k := make([]byte, KeySize)
	_, _ = rand.Read(k) // crypto/rand.Read never returns an error.

	return k
}

// ParseKey decodes a standard base64 key and checks its length. The error never contains the input.
func ParseKey(s string) ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(k) != KeySize {
		clear(k)

		return nil, fmt.Errorf("key must be %d bytes encoded as standard base64", KeySize)
	}

	return k, nil
}

// EncodeKey encodes a key as standard base64, the form ParseKey and secrets.key accept.
func EncodeKey(k []byte) string {
	return base64.StdEncoding.EncodeToString(k)
}

// KeyID derives a stable, non-secret identifier for a key. It is an HMAC of a fixed label under the key, so it reveals nothing about the key but lets rows record which key sealed them.
func KeyID(k []byte) string {
	m := hmac.New(sha256.New, k)
	m.Write([]byte("Encore secrets key id"))

	return keyIDVersion + "-" + hex.EncodeToString(m.Sum(nil)[:8])
}

func readKeyFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the operator chooses the key file
	if err != nil {
		return nil, fmt.Errorf("reading key file: %w", err)
	}
	defer clear(b)

	k, err := ParseKey(string(b))
	if err != nil {
		return nil, fmt.Errorf("key file %s: %w", path, err)
	}

	return k, nil
}

// writeKeyFile is atomic, with mode 0600.
func writeKeyFile(path string, k []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating key directory: %w", err)
	}

	f, err := os.CreateTemp(dir, ".secrets-key-")
	if err != nil {
		return fmt.Errorf("creating key file: %w", err)
	}

	tmp := f.Name()

	buf := []byte(EncodeKey(k) + "\n")
	defer clear(buf)

	_, err = f.Write(buf)
	err = errors.Join(err, f.Chmod(0o600), f.Sync(), f.Close())

	if err == nil {
		err = os.Rename(tmp, path)
	}

	if err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("writing key file %s: %w", path, err)
	}

	return nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}
