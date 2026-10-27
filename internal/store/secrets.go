package store

import (
	"context"
	"database/sql"
	"fmt"
)

// SecretRows returns the encrypted secret repository.
func (db *DB) SecretRows() SecretRows { return SecretRows{db} }

func scanSecret(s scanner) (SecretRow, error) {
	var (
		row              SecretRow
		created, updated int64
	)

	if err := s.Scan(&row.ID, &row.KeyID, &row.Nonce, &row.Ciphertext, &created, &updated); err != nil {
		return SecretRow{}, mapErr(err)
	}

	row.CreatedAt, row.UpdatedAt = fromMillis(created), fromMillis(updated)

	return row, nil
}

// Put inserts or replaces a secret, keeping the original creation time.
func (r SecretRows) Put(ctx context.Context, row SecretRow) error {
	now := toMillis(r.db.stamp())

	_, err := r.db.write.ExecContext(ctx, `INSERT INTO secrets (`+secretColumns+`) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET key_id = excluded.key_id, nonce = excluded.nonce, ciphertext = excluded.ciphertext, updated_at = excluded.updated_at`,
		row.ID, row.KeyID, row.Nonce, row.Ciphertext, now, now)
	if err != nil {
		return fmt.Errorf("storing secret %q: %w", row.ID, mapErr(err))
	}

	return nil
}

// Get returns one secret, or ErrNotFound.
func (r SecretRows) Get(ctx context.Context, id string) (SecretRow, error) {
	row, err := scanSecret(r.db.read.QueryRowContext(ctx, `SELECT `+secretColumns+` FROM secrets WHERE id = ?`, id))
	if err != nil {
		return SecretRow{}, fmt.Errorf("getting secret %q: %w", id, err)
	}

	return row, nil
}

// Delete removes a secret. Deleting a missing secret is not an error.
func (r SecretRows) Delete(ctx context.Context, id string) error {
	if _, err := r.db.write.ExecContext(ctx, `DELETE FROM secrets WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deleting secret %q: %w", id, err)
	}

	return nil
}

// KeyIDs returns the distinct key IDs in use.
func (r SecretRows) KeyIDs(ctx context.Context) ([]string, error) {
	return queryAll(ctx, r.db.read, func(s scanner) (string, error) {
		var id string
		err := s.Scan(&id)

		return id, err
	}, `SELECT DISTINCT key_id FROM secrets ORDER BY key_id`)
}

// Rewrite passes every secret to fn in one write transaction and saves each row back. If fn returns an error nothing is saved. It is how keys are rotated atomically.
func (r SecretRows) Rewrite(ctx context.Context, fn func(row *SecretRow) error) error {
	now := toMillis(r.db.stamp())

	return r.db.tx(ctx, func(tx *sql.Tx) error {
		rows, err := queryAll(ctx, tx, scanSecret, `SELECT `+secretColumns+` FROM secrets ORDER BY id`)
		if err != nil {
			return err
		}

		for i := range rows {
			if err := fn(&rows[i]); err != nil {
				return err
			}

			_, err := tx.ExecContext(ctx, `UPDATE secrets SET key_id = ?, nonce = ?, ciphertext = ?, updated_at = ? WHERE id = ?`,
				rows[i].KeyID, rows[i].Nonce, rows[i].Ciphertext, now, rows[i].ID)
			if err != nil {
				return fmt.Errorf("rewriting secret %q: %w", rows[i].ID, err)
			}
		}

		return nil
	})
}
