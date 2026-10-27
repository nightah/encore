package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// KodiInstances returns the Kodi instance repository.
func (db *DB) KodiInstances() KodiInstances { return KodiInstances{db} }

func scanKodi(s scanner) (KodiInstance, error) {
	var (
		k                KodiInstance
		group            sql.NullInt64
		quits            sql.NullBool
		created, updated int64
	)

	err := s.Scan(&k.ID, &k.Name, &k.Scheme, &k.Host, &k.Port, &k.Path, &k.Username, &k.PasswordSecretRef,
		&k.WSPort, &k.MAC, &k.BroadcastAddr, &k.IsDefaultForUnlinked, &k.Language, &k.Timezone, &quits, &group, &created, &updated)
	if err != nil {
		return KodiInstance{}, mapErr(err)
	}

	k.LibraryGroupID = group.Int64

	if quits.Valid {
		k.ShutdownQuits = &quits.Bool
	}

	k.CreatedAt, k.UpdatedAt = fromMillis(created), fromMillis(updated)

	return k, nil
}

// Create inserts k and sets its ID and timestamps. If k is the default for unlinked devices, any previous default is cleared.
func (r KodiInstances) Create(ctx context.Context, k *KodiInstance) error {
	now := r.db.stamp()

	return r.db.tx(ctx, func(tx *sql.Tx) error {
		if k.IsDefaultForUnlinked {
			if err := clearKodiDefault(ctx, tx, now); err != nil {
				return err
			}
		}

		err := tx.QueryRowContext(ctx, `INSERT INTO kodi_instances
			(name, scheme, host, port, path, username, password_secret_ref, ws_port, mac, broadcast_addr, is_default_for_unlinked,
			 language, timezone, shutdown_quits, library_group_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			k.Name, k.Scheme, k.Host, k.Port, k.Path, k.Username, k.PasswordSecretRef, k.WSPort, k.MAC, k.BroadcastAddr,
			k.IsDefaultForUnlinked, k.Language, k.Timezone, k.ShutdownQuits, nullID(k.LibraryGroupID), toMillis(now), toMillis(now)).Scan(&k.ID)
		if err != nil {
			return fmt.Errorf("creating kodi instance: %w", mapErr(err))
		}

		k.CreatedAt, k.UpdatedAt = now, now

		return nil
	})
}

// Update saves every field of k except CreatedAt and LibraryGroupID. If k is the default for unlinked devices, any previous default is cleared.
func (r KodiInstances) Update(ctx context.Context, k *KodiInstance) error {
	now := r.db.stamp()

	return r.db.tx(ctx, func(tx *sql.Tx) error {
		if k.IsDefaultForUnlinked {
			if err := clearKodiDefault(ctx, tx, now); err != nil {
				return err
			}
		}

		err := affected(tx.ExecContext(ctx, `UPDATE kodi_instances SET
			name = ?, scheme = ?, host = ?, port = ?, path = ?, username = ?, password_secret_ref = ?, ws_port = ?,
			mac = ?, broadcast_addr = ?, is_default_for_unlinked = ?, language = ?, timezone = ?, shutdown_quits = ?, updated_at = ? WHERE id = ?`,
			k.Name, k.Scheme, k.Host, k.Port, k.Path, k.Username, k.PasswordSecretRef, k.WSPort,
			k.MAC, k.BroadcastAddr, k.IsDefaultForUnlinked, k.Language, k.Timezone, k.ShutdownQuits, toMillis(now), k.ID))
		if err != nil {
			return fmt.Errorf("updating kodi instance %d: %w", k.ID, err)
		}

		k.UpdatedAt = now

		return nil
	})
}

// SetDefault makes id the default for unlinked devices, or clears the default when id is 0.
func (r KodiInstances) SetDefault(ctx context.Context, id int64) error {
	now := r.db.stamp()

	return r.db.tx(ctx, func(tx *sql.Tx) error {
		if err := clearKodiDefault(ctx, tx, now); err != nil {
			return err
		}

		if id == 0 {
			return nil
		}

		err := affected(tx.ExecContext(ctx, `UPDATE kodi_instances SET is_default_for_unlinked = 1, updated_at = ? WHERE id = ?`, toMillis(now), id))
		if err != nil {
			return fmt.Errorf("setting default kodi instance %d: %w", id, err)
		}

		return nil
	})
}

func clearKodiDefault(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE kodi_instances SET is_default_for_unlinked = 0, updated_at = ? WHERE is_default_for_unlinked = 1`, toMillis(now))
	if err != nil {
		return fmt.Errorf("clearing default kodi instance: %w", err)
	}

	return nil
}

// Get returns the instance with the given ID.
func (r KodiInstances) Get(ctx context.Context, id int64) (KodiInstance, error) {
	k, err := scanKodi(r.db.read.QueryRowContext(ctx, `SELECT `+kodiColumns+` FROM kodi_instances WHERE id = ?`, id))
	if err != nil {
		return KodiInstance{}, fmt.Errorf("getting kodi instance %d: %w", id, err)
	}

	return k, nil
}

// Default returns the default instance for unlinked devices, or ErrNotFound.
func (r KodiInstances) Default(ctx context.Context) (KodiInstance, error) {
	k, err := scanKodi(r.db.read.QueryRowContext(ctx, `SELECT `+kodiColumns+` FROM kodi_instances WHERE is_default_for_unlinked = 1`))
	if err != nil {
		return KodiInstance{}, fmt.Errorf("getting default kodi instance: %w", err)
	}

	return k, nil
}

// List returns all instances ordered by name.
func (r KodiInstances) List(ctx context.Context) ([]KodiInstance, error) {
	return queryAll(ctx, r.db.read, scanKodi, `SELECT `+kodiColumns+` FROM kodi_instances ORDER BY name COLLATE NOCASE`)
}

// Delete removes an instance. Devices and rooms linked to it become unlinked; its title aliases are deleted.
func (r KodiInstances) Delete(ctx context.Context, id int64) error {
	if err := affected(r.db.write.ExecContext(ctx, `DELETE FROM kodi_instances WHERE id = ?`, id)); err != nil {
		return fmt.Errorf("deleting kodi instance %d: %w", id, err)
	}

	return nil
}

func queryAll[T any](ctx context.Context, q querier, scan func(scanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying: %w", err)
	}
	defer rows.Close()

	var out []T

	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading rows: %w", err)
	}

	return out, nil
}
