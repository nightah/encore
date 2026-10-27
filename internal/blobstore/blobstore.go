package blobstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// ValidateKey reports whether key is a clean relative path that cannot escape the store root.
func ValidateKey(key string) error {
	switch {
	case key == "":
		return fmt.Errorf("%w: empty", ErrInvalidKey)
	case len(key) > maxKeyLen:
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidKey, maxKeyLen)
	case !utf8.ValidString(key):
		return fmt.Errorf("%w: not UTF-8", ErrInvalidKey)
	case strings.ContainsAny(key, "\\\x00"):
		return fmt.Errorf("%w: %q contains a backslash or NUL", ErrInvalidKey, key)
	}

	for seg := range strings.SplitSeq(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q must be a relative path without empty, . or .. segments", ErrInvalidKey, key)
		}
	}

	return nil
}

func validatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}

	return ValidateKey(strings.TrimSuffix(prefix, "/"))
}

func joinPrefix(prefix, key string) string {
	if prefix == "" {
		return key
	}

	return prefix + "/" + key
}

func cleanPrefix(p string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}

	if err := ValidateKey(p); err != nil {
		return "", fmt.Errorf("prefix: %w", err)
	}

	return p, nil
}

func probe(ctx context.Context, s Store) error {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])

	key := probeKeyPrefix + hex.EncodeToString(nonce[:])
	want := []byte("encore storage probe " + key)

	if err := s.Put(ctx, key, bytes.NewReader(want), int64(len(want))); err != nil {
		return fmt.Errorf("probe: cannot write: %w", err)
	}

	rc, err := s.Get(ctx, key)
	if err != nil {
		_ = s.Delete(ctx, key)

		return fmt.Errorf("probe: wrote %s but cannot read it back: %w", key, err)
	}

	got, err := io.ReadAll(io.LimitReader(rc, int64(len(want))+1))
	_ = rc.Close()

	if err != nil {
		_ = s.Delete(ctx, key)

		return fmt.Errorf("probe: reading %s: %w", key, err)
	}

	if !bytes.Equal(got, want) {
		_ = s.Delete(ctx, key)

		return fmt.Errorf("probe: %s read back %d bytes that differ from the %d written", key, len(got), len(want))
	}

	if err := s.Delete(ctx, key); err != nil {
		return fmt.Errorf("probe: wrote and read %s but cannot delete it: %w", key, err)
	}

	return nil
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}

	return c.r.Read(p)
}
