package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Settings returns the settings repository.
func (db *DB) Settings() Settings { return Settings{db} }

// Get decodes the value of key into dst, or returns ErrNotFound.
func (r Settings) Get(ctx context.Context, key string, dst any) error {
	raw, err := r.Raw(ctx, key)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decoding setting %q: %w", key, err)
	}

	return nil
}

// Raw returns the JSON value of key, or ErrNotFound.
func (r Settings) Raw(ctx context.Context, key string) (json.RawMessage, error) {
	var raw string

	if err := r.db.read.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw); err != nil {
		return nil, fmt.Errorf("getting setting %q: %w", key, mapErr(err))
	}

	return json.RawMessage(raw), nil
}

// Set stores v, encoded as JSON, under key.
func (r Settings) Set(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding setting %q: %w", key, err)
	}

	_, err = r.db.write.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, string(b), toMillis(r.db.stamp()))
	if err != nil {
		return fmt.Errorf("setting %q: %w", key, mapErr(err))
	}

	return nil
}

// Delete removes key. Deleting a missing key is not an error.
func (r Settings) Delete(ctx context.Context, key string) error {
	if _, err := r.db.write.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key); err != nil {
		return fmt.Errorf("deleting setting %q: %w", key, err)
	}

	return nil
}

// All returns every setting.
func (r Settings) All(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := r.db.read.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("listing settings: %w", err)
	}
	defer rows.Close()

	out := map[string]json.RawMessage{}

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("reading setting: %w", err)
		}

		out[k] = json.RawMessage(v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing settings: %w", err)
	}

	return out, nil
}

// Setting returns the value of key decoded as T, or def when the key is unset.
func Setting[T any](ctx context.Context, s Settings, key string, def T) (T, error) {
	var v T

	err := s.Get(ctx, key, &v)

	switch {
	case errors.Is(err, ErrNotFound):
		return def, nil
	case err != nil:
		return def, err
	default:
		return v, nil
	}
}
