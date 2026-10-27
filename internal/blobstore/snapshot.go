package blobstore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
)

// SaveJSON stores v as gzipped JSON under key. By convention key ends in ".json.gz".
func SaveJSON(ctx context.Context, s Store, key string, v any) error {
	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)

	if err := json.NewEncoder(gz).Encode(v); err != nil {
		return fmt.Errorf("encoding %s: %w", key, err)
	}

	if err := gz.Close(); err != nil {
		return fmt.Errorf("compressing %s: %w", key, err)
	}

	return s.Put(ctx, key, &buf, int64(buf.Len()))
}

// LoadJSON decodes gzipped JSON stored under key into v. It returns ErrNotFound if key is missing.
func LoadJSON(ctx context.Context, s Store, key string, v any) error {
	rc, err := s.Get(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()

	gz, err := gzip.NewReader(rc)
	if err != nil {
		return fmt.Errorf("decompressing %s: %w", key, err)
	}
	defer gz.Close()

	if err := json.NewDecoder(gz).Decode(v); err != nil {
		return fmt.Errorf("decoding %s: %w", key, err)
	}

	return nil
}
